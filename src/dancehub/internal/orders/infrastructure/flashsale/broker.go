package flashsale

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const admissionLua = `
local existing=redis.call('GET',KEYS[3]); if existing then return {1,existing} end
local stock=tonumber(redis.call('GET',KEYS[1]) or '-1'); if stock<=0 then return {0,'sold_out'} end
local bought=tonumber(redis.call('GET',KEYS[2]) or '0'); if bought>=tonumber(ARGV[3]) then return {0,'limit_reached'} end
redis.call('DECR',KEYS[1]); redis.call('INCR',KEYS[2]); redis.call('PEXPIRE',KEYS[2],ARGV[4]); redis.call('SET',KEYS[3],ARGV[1],'PX',ARGV[4]);
redis.call('XADD',KEYS[4],'*','request_id',ARGV[1],'campaign_id',ARGV[2],'user_id',ARGV[5],'idempotency_key',ARGV[6],'traceparent',ARGV[7],'tracestate',ARGV[8],'baggage',ARGV[9]);
return {1,ARGV[1]}`

type Handler func(context.Context, application.FlashSaleMessage) error

type Broker struct {
	redis     *redis.Client
	conn      *amqp091.Connection
	channel   *amqp091.Channel
	cancel    context.CancelFunc
	returns   <-chan amqp091.Return
	publishMu sync.Mutex
}

func New(redisAddress, rabbitURL string) (*Broker, error) {
	rdb := redis.NewClient(&redis.Options{Addr: redisAddress})
	conn, err := amqp091.Dial(rabbitURL)
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		_ = rdb.Close()
		return nil, err
	}
	if err = declareTopology(channel); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		_ = rdb.Close()
		return nil, err
	}
	return &Broker{redis: rdb, conn: conn, channel: channel, returns: channel.NotifyReturn(make(chan amqp091.Return, 16))}, nil
}

func (b *Broker) Start(parent context.Context, handler Handler) {
	ctx, cancel := context.WithCancel(parent)
	b.cancel = cancel
	go b.consume(ctx, handler)
	go b.relayStream(ctx)
}

func (b *Broker) Reserve(ctx context.Context, actor application.ActorContext, campaign application.CampaignSnapshot, audit application.AuditContext) (string, error) {
	stockKey := "flashsale:" + campaign.ID + ":stock"
	ttl := time.Until(campaign.EndsAt) + time.Hour
	if created, _ := b.redis.SetNX(ctx, stockKey, campaign.Inventory, ttl).Result(); created {
		slog.Info("flash sale inventory loaded", "campaign_id", campaign.ID, "inventory", campaign.Inventory)
	}
	requestID := newRequestID()
	keys := []string{stockKey, "flashsale:" + campaign.ID + ":user:" + actor.UserID, "flashsale:" + campaign.ID + ":idem:" + actor.UserID + ":" + audit.IdempotencyKey, "flashsale:admission.stream"}
	traceparent, tracestate, baggage := platform.TraceHeaders(ctx)
	result, err := b.redis.Eval(ctx, admissionLua, keys, requestID, campaign.ID, campaign.PerUserLimit, int64(ttl/time.Millisecond), actor.UserID, audit.IdempotencyKey, traceparent, tracestate, baggage).Slice()
	if err != nil || len(result) != 2 {
		return "", application.Unavailable("unable to reserve flash sale inventory", err)
	}
	accepted, _ := strconv.ParseInt(fmt.Sprint(result[0]), 10, 64)
	value := fmt.Sprint(result[1])
	if accepted != 1 {
		return "", application.Conflict(value)
	}
	return value, nil
}

func (b *Broker) Close() {
	if b.cancel != nil {
		b.cancel()
	}
	_ = b.channel.Close()
	_ = b.conn.Close()
	_ = b.redis.Close()
}

func declareTopology(channel *amqp091.Channel) error {
	if err := channel.ExchangeDeclare("flashsale.exchange", "direct", true, false, false, false, nil); err != nil {
		return err
	}
	queue, err := channel.QueueDeclare("flashsale.purchase-request.q", true, false, false, false, amqp091.Table{"x-queue-type": "quorum"})
	if err != nil {
		return err
	}
	if err = channel.QueueBind(queue.Name, "purchase", "flashsale.exchange", false, nil); err != nil {
		return err
	}
	return channel.Confirm(false)
}

const admissionStream = "flashsale:admission.stream"
const admissionGroup = "rabbitmq-relay"
const acknowledgeLua = `
local acknowledged=redis.call('XACK',KEYS[1],ARGV[1],ARGV[2])
if acknowledged>0 then redis.call('XDEL',KEYS[1],ARGV[2]) end
return acknowledged`

// Publication can be repeated after a crash. Never discard the Redis record
// until the durable RabbitMQ queue has confirmed accepting it.
func publishThenAcknowledge(ctx context.Context, publish, acknowledge func(context.Context) error) error {
	if err := publish(ctx); err != nil {
		return err
	}
	return acknowledge(ctx)
}

func recoverPending(ctx context.Context, client *redis.Client, stream, group, consumer string, minIdle time.Duration, deliver func(context.Context, []redis.XMessage)) error {
	cursor := "0-0"
	for ctx.Err() == nil {
		entries, next, err := client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: stream, Group: group, Consumer: consumer, MinIdle: minIdle, Start: cursor, Count: 100}).Result()
		if err != nil {
			return err
		}
		deliver(ctx, entries)
		cursor = next
		if cursor == "0-0" {
			return nil
		}
	}
	return ctx.Err()
}

func (b *Broker) relayEntries(ctx context.Context, entries []redis.XMessage) {
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		message := application.FlashSaleMessage{RequestID: streamValue(entry.Values, "request_id"), CampaignID: streamValue(entry.Values, "campaign_id"), UserID: streamValue(entry.Values, "user_id"), IdempotencyKey: streamValue(entry.Values, "idempotency_key"), Traceparent: streamValue(entry.Values, "traceparent"), Tracestate: streamValue(entry.Values, "tracestate"), Baggage: streamValue(entry.Values, "baggage")}
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := publishThenAcknowledge(attempt, func(ctx context.Context) error { return b.publish(ctx, message) }, func(ctx context.Context) error {
			return b.redis.Eval(ctx, acknowledgeLua, []string{admissionStream}, admissionGroup, entry.ID).Err()
		})
		cancel()
		if err != nil {
			slog.Warn("flash sale request remains pending", "request_id", message.RequestID, "error", err)
		}
	}
}

func (b *Broker) relayStream(ctx context.Context) {
	consumer := "orderservice-relay-" + newRequestID()
	for ctx.Err() == nil {
		if err := b.redis.XGroupCreateMkStream(ctx, admissionStream, admissionGroup, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			slog.Warn("flash sale stream group creation failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		if err := recoverPending(ctx, b.redis, admissionStream, admissionGroup, consumer, 60*time.Second, b.relayEntries); err != nil {
			slog.Warn("flash sale pending recovery failed", "error", err)
		}
		batches, err := b.redis.XReadGroup(ctx, &redis.XReadGroupArgs{Group: admissionGroup, Consumer: consumer, Streams: []string{admissionStream, ">"}, Count: 100, Block: 5 * time.Second}).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			slog.Warn("flash sale stream read failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		for _, batch := range batches {
			b.relayEntries(ctx, batch.Messages)
		}
	}
}

func (b *Broker) publish(ctx context.Context, message application.FlashSaleMessage) error {
	b.publishMu.Lock()
	defer b.publishMu.Unlock()
	// Drain returns from previously timed-out attempts before this publication.
	for {
		select {
		case _, open := <-b.returns:
			if !open {
				return fmt.Errorf("RabbitMQ return channel closed")
			}
		default:
			goto drained
		}
	}
drained:
	payload, _ := json.Marshal(message)
	headers := amqp091.Table{}
	for name, value := range map[string]string{"traceparent": message.Traceparent, "tracestate": message.Tracestate, "baggage": message.Baggage} {
		if value != "" {
			headers[name] = value
		}
	}
	confirmation, err := b.channel.PublishWithDeferredConfirmWithContext(ctx, "flashsale.exchange", "purchase", true, false, amqp091.Publishing{DeliveryMode: amqp091.Persistent, ContentType: "application/json", MessageId: message.RequestID, Timestamp: time.Now(), Headers: headers, Body: payload})
	if err != nil {
		return err
	}
	ack, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ack {
		return fmt.Errorf("RabbitMQ rejected flash sale request")
	}
	// AMQP delivers a mandatory return before its publisher confirmation. The
	// buffered notification is therefore available once WaitContext succeeds.
	select {
	case returned, open := <-b.returns:
		if !open {
			return fmt.Errorf("RabbitMQ return channel closed")
		}
		return fmt.Errorf("RabbitMQ returned flash sale request: %s", returned.ReplyText)
	default:
		return nil
	}
}

func (b *Broker) consume(ctx context.Context, handler Handler) {
	deliveries, err := b.channel.ConsumeWithContext(ctx, "flashsale.purchase-request.q", "flashsale-order-worker", false, false, false, false, nil)
	if err != nil {
		slog.Error("flash sale consumer failed", "error", err)
		return
	}
	for delivery := range deliveries {
		var message application.FlashSaleMessage
		if json.Unmarshal(delivery.Body, &message) != nil {
			_ = delivery.Nack(false, false)
			continue
		}
		message.Traceparent = headerValue(delivery.Headers, "traceparent")
		message.Tracestate = headerValue(delivery.Headers, "tracestate")
		message.Baggage = headerValue(delivery.Headers, "baggage")
		messageContext := platform.ContextFromTraceHeaders(ctx, message.Traceparent, message.Tracestate, message.Baggage)
		messageContext, span := otel.Tracer("orderservice").Start(messageContext, "flashsale.purchase consume", trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attribute.String("messaging.system", "rabbitmq"), attribute.String("messaging.destination.name", "flashsale.purchase-request.q")))
		workerErr := handler(messageContext, message)
		if workerErr != nil {
			span.RecordError(workerErr)
			span.SetStatus(otelcodes.Error, "flash sale order failed")
		}
		span.End()
		if workerErr != nil {
			_ = delivery.Nack(false, true)
		} else {
			_ = delivery.Ack(false)
		}
	}
}

func newRequestID() string {
	value := make([]byte, 16)
	_, _ = rand.Read(value)
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
func streamValue(values map[string]any, name string) string {
	if value, ok := values[name]; ok {
		return fmt.Sprint(value)
	}
	return ""
}
func headerValue(values amqp091.Table, name string) string {
	if value, ok := values[name]; ok {
		return fmt.Sprint(value)
	}
	return ""
}
