package media

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type Queue struct {
	channel       *amqp091.Channel
	confirmations <-chan amqp091.Confirmation
	mu            sync.Mutex
}

func New(connection *amqp091.Connection) (*Queue, error) {
	channel, err := connection.Channel()
	if err != nil {
		return nil, err
	}
	if err = channel.ExchangeDeclare("chat.media.exchange", "direct", true, false, false, false, nil); err != nil {
		return nil, err
	}
	if err = channel.ExchangeDeclare("chat.media.dlx", "direct", true, false, false, false, nil); err != nil {
		return nil, err
	}
	dead, err := channel.QueueDeclare("chat.media.dead-letter.q", true, false, false, false, nil)
	if err != nil {
		return nil, err
	}
	if err = channel.QueueBind(dead.Name, "dead", "chat.media.dlx", false, nil); err != nil {
		return nil, err
	}
	_, err = channel.QueueDeclare("chat.media.retry.q", true, false, false, false, amqp091.Table{"x-message-ttl": int32(10000), "x-dead-letter-exchange": "chat.media.exchange", "x-dead-letter-routing-key": "scan"})
	if err != nil {
		return nil, err
	}
	queue, err := channel.QueueDeclare("chat.media.scan.q", true, false, false, false, amqp091.Table{"x-dead-letter-exchange": "chat.media.dlx", "x-dead-letter-routing-key": "dead"})
	if err != nil {
		return nil, err
	}
	if err = channel.QueueBind(queue.Name, "scan", "chat.media.exchange", false, nil); err != nil {
		return nil, err
	}
	if err = channel.Confirm(false); err != nil {
		return nil, err
	}
	return &Queue{channel: channel, confirmations: channel.NotifyPublish(make(chan amqp091.Confirmation, 1))}, nil
}
func (q *Queue) Close() error { return q.channel.Close() }
func (q *Queue) RequestScan(ctx context.Context, attachment application.Attachment) error {
	payload, err := json.Marshal(map[string]any{"attachment_id": attachment.ID, "object_key": attachment.ObjectKey, "mime_type": attachment.MimeType, "size_bytes": attachment.SizeBytes, "sha256": attachment.SHA256})
	if err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	headers := amqp091.Table{}
	for key, value := range carrier {
		headers[key] = value
	}
	if err = q.channel.PublishWithContext(ctx, "chat.media.exchange", "scan", false, false, amqp091.Publishing{ContentType: "application/json", DeliveryMode: amqp091.Persistent, MessageId: attachment.ID, Headers: headers, Body: payload}); err != nil {
		return err
	}
	select {
	case confirmation, open := <-q.confirmations:
		if !open || !confirmation.Ack {
			return fmt.Errorf("RabbitMQ did not confirm chat media scan request")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ application.MediaQueue = (*Queue)(nil)
