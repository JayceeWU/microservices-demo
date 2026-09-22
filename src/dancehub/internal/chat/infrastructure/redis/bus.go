package redis

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	redis "github.com/redis/go-redis/v9"
)

const realtimeChannel = "chat:events:v1"

var acquireUploadScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local expires = now + tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
if redis.call('ZCARD', key) >= tonumber(ARGV[3]) then
  return 0
end
redis.call('ZADD', key, expires, ARGV[4])
redis.call('PEXPIRE', key, ARGV[2])
return 1
`)

type Bus struct{ client *redis.Client }

func New(address string) *Bus                 { return &Bus{client: redis.NewClient(&redis.Options{Addr: address})} }
func (b *Bus) Close() error                   { return b.client.Close() }
func (b *Bus) Ping(ctx context.Context) error { return b.client.Ping(ctx).Err() }
func (b *Bus) Publish(ctx context.Context, payload []byte) error {
	return b.client.Publish(ctx, realtimeChannel, payload).Err()
}

func (b *Bus) Subscribe(ctx context.Context) (<-chan []byte, func() error, error) {
	pubsub := b.client.Subscribe(ctx, realtimeChannel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, nil, err
	}
	out := make(chan []byte, 64)
	go func() {
		defer close(out)
		for message := range pubsub.Channel() {
			select {
			case out <- []byte(message.Payload):
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, pubsub.Close, nil
}

func (b *Bus) Create(ctx context.Context, actor application.ActorContext, ttl time.Duration) (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	ticket := base64.RawURLEncoding.EncodeToString(random)
	value, err := json.Marshal(actor)
	if err != nil {
		return "", err
	}
	return ticket, b.client.Set(ctx, "chat:ticket:"+ticket, value, ttl).Err()
}

func (b *Bus) Consume(ctx context.Context, ticket string) (application.ActorContext, error) {
	var actor application.ActorContext
	value, err := b.client.GetDel(ctx, "chat:ticket:"+ticket).Bytes()
	if err == redis.Nil {
		return actor, appcore.Denied("invalid or expired WebSocket ticket")
	}
	if err != nil {
		return actor, err
	}
	if err = json.Unmarshal(value, &actor); err != nil {
		return actor, err
	}
	return actor, nil
}

func (b *Bus) Allow(ctx context.Context, userID, category string, limit int, window time.Duration) (bool, time.Duration, error) {
	bucket := time.Now().UTC().Unix() / int64(window.Seconds())
	key := fmt.Sprintf("chat:rate:%s:%s:%d", category, userID, bucket)
	count, err := b.client.Incr(ctx, key).Result()
	if err != nil {
		return false, 0, err
	}
	if count == 1 {
		_ = b.client.Expire(ctx, key, window).Err()
	}
	ttl, _ := b.client.TTL(ctx, key).Result()
	return count <= int64(limit), ttl, nil
}

func (b *Bus) SetPresence(ctx context.Context, userID string, ttl time.Duration) error {
	return b.client.Set(ctx, "chat:presence:"+userID, "1", ttl).Err()
}

func (b *Bus) AcquireUpload(ctx context.Context, userID, uploadID string, limit int, ttl time.Duration) (bool, error) {
	result, err := acquireUploadScript.Run(ctx, b.client, []string{"chat:uploads:" + userID}, time.Now().UTC().UnixMilli(), ttl.Milliseconds(), limit, uploadID).Int()
	return result == 1, err
}

func (b *Bus) ReleaseUpload(ctx context.Context, userID, uploadID string) error {
	return b.client.ZRem(ctx, "chat:uploads:"+userID, uploadID).Err()
}

var _ application.RealtimeBus = (*Bus)(nil)
var _ application.TicketStore = (*Bus)(nil)
