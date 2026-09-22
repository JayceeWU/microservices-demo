package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type PrivateObjectStore interface {
	Copy(context.Context, string, string) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type Worker struct {
	Pool    *pgxpool.Pool
	Rabbit  *amqp091.Connection
	Objects PrivateObjectStore
	ClamAV  string
}

type scanRequest struct {
	AttachmentID string `json:"attachment_id"`
	ObjectKey    string `json:"object_key"`
	MimeType     string `json:"mime_type"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
}

func (w Worker) Run(ctx context.Context) error {
	channel, err := w.Rabbit.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	if err = channel.Qos(1, 0, false); err != nil {
		return err
	}
	if err = channel.Confirm(false); err != nil {
		return err
	}
	confirmations := channel.NotifyPublish(make(chan amqp091.Confirmation, 1))
	deliveries, err := channel.Consume("chat.media.scan.q", "chat-media-worker", false, false, false, false, nil)
	if err != nil {
		return err
	}
	cleanupTicker := time.NewTicker(10 * time.Second)
	defer cleanupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-cleanupTicker.C:
			if err := w.cleanupDeletedObject(ctx); err != nil {
				slog.Warn("chat media cleanup retry", "error", err)
			}
		case delivery, open := <-deliveries:
			if !open {
				return fmt.Errorf("chat media queue closed")
			}
			deliveryContext := extractContext(ctx, delivery.Headers)
			if err = w.process(deliveryContext, delivery.Body); err != nil {
				retries := retryCount(delivery.Headers)
				if retries < 3 {
					headers := cloneHeaders(delivery.Headers)
					headers["x-chat-retries"] = retries + 1
					publishErr := channel.PublishWithContext(ctx, "", "chat.media.retry.q", false, false, amqp091.Publishing{ContentType: delivery.ContentType, DeliveryMode: amqp091.Persistent, MessageId: delivery.MessageId, Headers: headers, Body: delivery.Body})
					if publishErr == nil {
						select {
						case confirmation := <-confirmations:
							if !confirmation.Ack {
								publishErr = fmt.Errorf("RabbitMQ rejected chat media retry")
							}
						case <-ctx.Done():
							publishErr = ctx.Err()
						}
					}
					if publishErr == nil {
						_ = delivery.Ack(false)
						continue
					}
				}
				_ = delivery.Nack(false, false)
			} else {
				_ = delivery.Ack(false)
			}
		}
	}
}

func (w Worker) cleanupDeletedObject(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return platform.WithActorTx(ctx, w.Pool, "chat-media-worker", "", "", "service", "chat-media-worker", nil, nil, func(ctx context.Context, tx pgx.Tx) error {
		var objectKey string
		err := tx.QueryRow(ctx, `SELECT job.object_key FROM chat.media_deletion_jobs job
			WHERE job.not_before<=now() AND NOT EXISTS(
				SELECT 1 FROM chat.attachments a WHERE a.status='CLEAN' AND a.clean_object_key=job.object_key)
			ORDER BY job.not_before FOR UPDATE OF job SKIP LOCKED LIMIT 1`).Scan(&objectKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		deleteErr := w.Objects.Delete(ctx, objectKey)
		if deleteErr == nil {
			_, err = tx.Exec(ctx, `DELETE FROM chat.media_deletion_jobs WHERE object_key=$1`, objectKey)
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE chat.media_deletion_jobs SET attempts=attempts+1,last_error=$2,not_before=now()+interval '1 minute' WHERE object_key=$1`, objectKey, deleteErr.Error())
		return err
	})
}

func retryCount(headers amqp091.Table) int32 {
	value, ok := headers["x-chat-retries"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int32:
		return typed
	case int64:
		return int32(typed)
	case int:
		return int32(typed)
	}
	return 0
}

func cloneHeaders(source amqp091.Table) amqp091.Table {
	result := make(amqp091.Table, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func extractContext(ctx context.Context, headers amqp091.Table) context.Context {
	carrier := propagation.MapCarrier{}
	for key, value := range headers {
		if text, ok := value.(string); ok {
			carrier[key] = text
		}
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func (w Worker) process(ctx context.Context, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ctx, span := otel.Tracer("dancehub/chat/media").Start(ctx, "chat.attachment.scan")
	defer span.End()
	var request scanRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	var currentStatus string
	err := platform.WithActorTx(ctx, w.Pool, "chat-media-worker", "", "", "service", "chat-media-worker", nil, nil, func(ctx context.Context, tx pgx.Tx) error {
		// The queue carries a reference, not authoritative attachment metadata.
		return tx.QueryRow(ctx, `SELECT status::text,object_key,mime_type,sha256,size_bytes FROM chat.attachments WHERE id=$1`, request.AttachmentID).Scan(&currentStatus, &request.ObjectKey, &request.MimeType, &request.SHA256, &request.SizeBytes)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // The group may have been deleted while this job was queued.
	}
	if err != nil {
		return err
	}
	if currentStatus == "CLEAN" || currentStatus == "REJECTED" {
		return nil
	}
	if currentStatus != "PENDING_SCAN" {
		return fmt.Errorf("attachment is not ready for scanning")
	}
	candidate := "clean/" + request.AttachmentID + "/" + uuid.NewString()
	// Register cleanup before creating bytes, so a crash cannot leak the snapshot.
	// Cleanup starts after this attempt's deadline and never removes published keys.
	if err = platform.WithActorTx(ctx, w.Pool, "chat-media-worker", "", "", "service", "chat-media-worker", nil, nil, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat.media_deletion_jobs(object_key,not_before) VALUES($1,now()+interval '15 minutes')`, candidate)
		return err
	}); err != nil {
		return err
	}
	clean, reason, err := w.scanCandidate(ctx, request, candidate)
	if err != nil {
		return err
	}
	status := "CLEAN"
	cleanKey := candidate
	if !clean {
		status = "REJECTED"
		cleanKey = ""
	}
	return platform.WithActorTx(ctx, w.Pool, "chat-media-worker", "", "", "service", "chat-media-worker", nil, nil, func(ctx context.Context, tx pgx.Tx) error {
		var conversationID string
		err := tx.QueryRow(ctx, `UPDATE chat.attachments SET status=$2::chat.attachment_status,rejection_reason=NULLIF($3,''),clean_object_key=NULLIF($4,''),scanned_at=now()
			WHERE id=$1 AND status='PENDING_SCAN' AND clean_object_key IS NULL RETURNING conversation_id::text`, request.AttachmentID, status, reason, cleanKey).Scan(&conversationID)
		if errors.Is(err, pgx.ErrNoRows) {
			// A competing scan or group deletion won; clean up only our candidate.
			_, err = tx.Exec(ctx, `UPDATE chat.media_deletion_jobs SET not_before=now() WHERE object_key=$1`, candidate)
			return err
		}
		if err != nil {
			return err
		}
		if clean {
			_, err = tx.Exec(ctx, `DELETE FROM chat.media_deletion_jobs WHERE object_key=$1`, candidate)
		} else {
			_, err = tx.Exec(ctx, `UPDATE chat.media_deletion_jobs SET not_before=now() WHERE object_key=$1`, candidate)
		}
		if err != nil {
			return err
		}
		event, _ := json.Marshal(map[string]any{"event_type": "attachment_scanned", "conversation_id": conversationID, "attachment_id": request.AttachmentID, "status": status})
		_, err = tx.Exec(ctx, `INSERT INTO chat.realtime_outbox(conversation_id,payload) VALUES($1,$2)`, conversationID, event)
		return err
	})
}

func (w Worker) scanCandidate(ctx context.Context, request scanRequest, candidate string) (bool, string, error) {
	if err := w.Objects.Copy(ctx, request.ObjectKey, candidate); err != nil {
		return false, "", err
	}
	// Scan the independent snapshot, never the browser-writable upload key.
	object, err := w.Objects.Open(ctx, candidate)
	if err != nil {
		return false, "", err
	}
	defer object.Close()
	clean, reason, actualHash, actualSize, err := scanStream(ctx, w.ClamAV, request.MimeType, io.LimitReader(object, request.SizeBytes+1))
	if err != nil {
		return false, "", err
	}
	if !strings.EqualFold(actualHash, request.SHA256) {
		return false, "sha256 mismatch", nil
	}
	if actualSize != request.SizeBytes {
		return false, "file size mismatch", nil
	}
	return clean, reason, nil
}

func scanStream(ctx context.Context, clamAddress, mime string, source io.Reader) (bool, string, string, int64, error) {
	connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", clamAddress)
	if err != nil {
		return false, "", "", 0, err
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	deadline := time.Now().Add(10 * time.Minute)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	_ = connection.SetDeadline(deadline)
	if _, err = connection.Write([]byte("zINSTREAM\x00")); err != nil {
		return false, "", "", 0, err
	}
	hash := sha256.New()
	header := make([]byte, 0, 16)
	codecTail := make([]byte, 0, 3)
	browserVideoCodec := false
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		count, readErr := source.Read(buffer)
		if count > 0 {
			total += int64(count)
			if len(header) < 16 {
				needed := 16 - len(header)
				if needed > count {
					needed = count
				}
				header = append(header, buffer[:needed]...)
			}
			if mime == "video/mp4" && !browserVideoCodec {
				window := make([]byte, 0, len(codecTail)+count)
				window = append(window, codecTail...)
				window = append(window, buffer[:count]...)
				browserVideoCodec = containsBrowserVideoCodec(window)
				if len(window) > 3 {
					codecTail = append(codecTail[:0], window[len(window)-3:]...)
				} else {
					codecTail = append(codecTail[:0], window...)
				}
			}
			_, _ = hash.Write(buffer[:count])
			var length [4]byte
			binary.BigEndian.PutUint32(length[:], uint32(count))
			if _, err = connection.Write(length[:]); err != nil {
				return false, "", "", total, err
			}
			if _, err = connection.Write(buffer[:count]); err != nil {
				return false, "", "", total, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return false, "", "", total, readErr
		}
	}
	if _, err = connection.Write([]byte{0, 0, 0, 0}); err != nil {
		return false, "", "", total, err
	}
	response, err := bufio.NewReader(connection).ReadString(0)
	if err != nil {
		return false, "", "", total, err
	}
	if !validMagic(mime, header) {
		return false, "invalid file signature", hex.EncodeToString(hash.Sum(nil)), total, nil
	}
	if mime == "video/mp4" && !browserVideoCodec {
		return false, "MP4 must use a browser-compatible video codec", hex.EncodeToString(hash.Sum(nil)), total, nil
	}
	if !strings.Contains(response, "OK") {
		return false, strings.TrimSpace(strings.TrimSuffix(response, "\x00")), hex.EncodeToString(hash.Sum(nil)), total, nil
	}
	return true, "", hex.EncodeToString(hash.Sum(nil)), total, nil
}

func containsBrowserVideoCodec(payload []byte) bool {
	// ISO BMFF sample entries use these four-byte codec identifiers. They cover
	// the video codecs supported by current evergreen browsers without retaining
	// an entire (potentially 500 MB) upload in worker memory.
	for _, codec := range []string{"avc1", "avc3", "hvc1", "hev1", "vp09", "av01"} {
		if bytes.Contains(payload, []byte(codec)) {
			return true
		}
	}
	return false
}

func validMagic(mime string, header []byte) bool {
	switch mime {
	case "image/jpeg":
		return len(header) >= 3 && bytes.Equal(header[:3], []byte{0xff, 0xd8, 0xff})
	case "image/png":
		return len(header) >= 8 && bytes.Equal(header[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	case "image/webp":
		return len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP"
	case "image/gif":
		return len(header) >= 6 && (string(header[:6]) == "GIF87a" || string(header[:6]) == "GIF89a")
	case "video/mp4":
		return len(header) >= 12 && string(header[4:8]) == "ftyp"
	default:
		return false
	}
}
