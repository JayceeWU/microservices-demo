package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UnitOfWork struct {
	inner platform.UnitOfWork[application.ActorContext, application.Repositories]
}

func NewUnitOfWork(pool *pgxpool.Pool) UnitOfWork {
	return UnitOfWork{inner: platform.NewUnitOfWork[application.ActorContext](pool, NewRepositories)}
}

func (u UnitOfWork) Do(ctx context.Context, actor application.ActorContext, fn func(context.Context, application.Repositories) error) error {
	return u.inner.Do(ctx, actor, "chatservice", fn)
}

func NewRepositories(tx pgx.Tx) application.Repositories { return repositories{tx: tx} }

type repositories struct{ tx pgx.Tx }

func (r repositories) Conversations() application.ConversationRepository {
	return conversationRepository{tx: r.tx}
}
func (r repositories) Messages() application.MessageRepository { return messageRepository{tx: r.tx} }
func (r repositories) Blocks() application.BlockRepository     { return blockRepository{tx: r.tx} }
func (r repositories) Attachments() application.AttachmentRepository {
	return attachmentRepository{tx: r.tx}
}
func (r repositories) Events() application.EventRepository { return eventRepository{tx: r.tx} }

type scanner interface{ Scan(...any) error }

type blockRepository struct{ tx pgx.Tx }

func (r blockRepository) ExistsEitherDirection(ctx context.Context, a, b string) (bool, error) {
	var exists bool
	err := r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat.user_blocks WHERE (blocker_user_id=$1 AND blocked_user_id=$2) OR (blocker_user_id=$2 AND blocked_user_id=$1))`, a, b).Scan(&exists)
	return exists, err
}
func (r blockRepository) Set(ctx context.Context, blocker, blocked string, active bool) error {
	if active {
		_, err := r.tx.Exec(ctx, `INSERT INTO chat.user_blocks(blocker_user_id,blocked_user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, blocker, blocked)
		return err
	}
	_, err := r.tx.Exec(ctx, `DELETE FROM chat.user_blocks WHERE blocker_user_id=$1 AND blocked_user_id=$2`, blocker, blocked)
	return err
}

type attachmentRepository struct{ tx pgx.Tx }

func (r attachmentRepository) Add(ctx context.Context, a application.Attachment) (application.Attachment, error) {
	_, err := r.tx.Exec(ctx, `INSERT INTO chat.attachments(id,conversation_id,studio_id,uploader_id,kind,object_key,mime_type,size_bytes,sha256,status) SELECT $1,c.id,c.studio_id,$2,$3::chat.message_kind,$4,$5,$6,$7,'PENDING_UPLOAD' FROM chat.conversations c WHERE c.id=$8`, a.ID, a.UploaderID, a.Kind, a.ObjectKey, a.MimeType, a.SizeBytes, a.SHA256, a.ConversationID)
	if err == nil {
		// Wait beyond the PUT URL lifetime: deleting earlier would let a later PUT
		// recreate an orphan after its cleanup job has already completed.
		_, err = r.tx.Exec(ctx, `INSERT INTO chat.media_deletion_jobs(object_key,not_before) VALUES($1,now()+interval '16 minutes')`, a.ObjectKey)
	}
	return a, translate(err, "unable to prepare attachment")
}
func (r attachmentRepository) Get(ctx context.Context, id string, lock bool) (application.Attachment, error) {
	q := `SELECT id::text,conversation_id::text,uploader_id::text,kind::text,mime_type,size_bytes,sha256,object_key,COALESCE(clean_object_key,''),status::text FROM chat.attachments WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	var a application.Attachment
	var kind string
	err := r.tx.QueryRow(ctx, q, id).Scan(&a.ID, &a.ConversationID, &a.UploaderID, &kind, &a.MimeType, &a.SizeBytes, &a.SHA256, &a.ObjectKey, &a.CleanObjectKey, &a.Status)
	a.Kind = domain.MessageKind(kind)
	return a, translate(err, "attachment not found")
}
func (r attachmentRepository) MarkPendingScan(ctx context.Context, id string) (application.Attachment, error) {
	_, err := r.tx.Exec(ctx, `UPDATE chat.attachments SET status='PENDING_SCAN',uploaded_at=now() WHERE id=$1 AND status='PENDING_UPLOAD'`, id)
	if err != nil {
		return application.Attachment{}, err
	}
	return r.Get(ctx, id, false)
}

func (r attachmentRepository) DeletePending(ctx context.Context, id string) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM chat.attachments WHERE id=$1 AND status='PENDING_UPLOAD' AND uploader_id=app_security.user_id()`, id)
	return err
}

type eventRepository struct{ tx pgx.Tx }

func (r eventRepository) AppendRealtime(ctx context.Context, conversationID, eventType string, payload map[string]any) error {
	raw, err := realtimeEnvelope(conversationID, eventType, payload)
	if err != nil {
		return err
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO chat.realtime_outbox(conversation_id,payload) VALUES($1,$2)`, conversationID, raw)
	return err
}

// realtimeEnvelope stamps the routing fields every WebSocket frame needs. The mapper drops
// frames without a conversation ID, so it is set here for every event type rather than
// trusting each producer (message edits and withdrawals only know the message ID).
func realtimeEnvelope(conversationID, eventType string, payload map[string]any) ([]byte, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["event_type"] = eventType
	payload["conversation_id"] = conversationID
	return json.Marshal(payload)
}
func translate(err error, message string) error {
	return platform.TranslateDatabaseError(err, message, appcore.NotFound, appcore.Conflict)
}
func sortedPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}
func decodeOffset(token string) int {
	value, _ := strconv.Atoi(token)
	if value < 0 {
		return 0
	}
	return value
}
func nextOffset(length int, size int32, offset int) string {
	if length > int(size) {
		return strconv.Itoa(offset + int(size))
	}
	return ""
}
func paginateConversations(items []application.ConversationView, size int32, offset int) []application.ConversationView {
	_ = offset
	if len(items) > int(size) {
		return items[:size]
	}
	return items
}

var _ = fmt.Sprintf
