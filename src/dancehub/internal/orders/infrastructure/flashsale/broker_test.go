package flashsale

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"os"
	"testing"
)

func TestRelayNeverAcknowledgesAnUnconfirmedPublication(t *testing.T) {
	for _, failure := range []error{errors.New("publisher timeout"), errors.New("mandatory return"), context.Canceled} {
		acknowledged := false
		err := publishThenAcknowledge(context.Background(), func(context.Context) error { return failure }, func(context.Context) error { acknowledged = true; return nil })
		if !errors.Is(err, failure) || acknowledged {
			t.Fatalf("unconfirmed publication acknowledged: %v", err)
		}
	}
}

// An optional real-Redis check; its random stream never touches application data.
func TestRedisPendingIsRecoveredByANewConsumer(t *testing.T) {
	address := os.Getenv("FLASHSALE_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set FLASHSALE_TEST_REDIS_ADDR for the Redis integration check")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	ctx := context.Background()
	stream, group := "test:flashsale:"+newRequestID(), "recovery-test"
	defer client.Del(ctx, stream)
	id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"request_id": "stable-request"}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err = client.XGroupCreate(ctx, stream, group, "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err = client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: group, Consumer: "crashed-process", Streams: []string{stream, ">"}, Count: 1}).Err(); err != nil {
		t.Fatal(err)
	}
	publications := 0
	deliver := func(ctx context.Context, entries []redis.XMessage) {
		for _, entry := range entries {
			if entry.ID != id || streamValue(entry.Values, "request_id") != "stable-request" {
				t.Fatalf("redelivery changed identity: %+v", entry)
			}
			publications++
			// First process confirms publication but dies before Redis acknowledgement.
			if publications == 1 {
				continue
			}
			if err := client.Eval(ctx, acknowledgeLua, []string{stream}, group, entry.ID).Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = recoverPending(ctx, client, stream, group, "new-process-1", 0, deliver); err != nil {
		t.Fatal(err)
	}
	if pending, _ := client.XPending(ctx, stream, group).Result(); pending.Count != 1 {
		t.Fatal("unacknowledged request was lost")
	}
	if err = recoverPending(ctx, client, stream, group, "new-process-2", 0, deliver); err != nil {
		t.Fatal(err)
	}
	if pending, _ := client.XPending(ctx, stream, group).Result(); pending.Count != 0 {
		t.Fatal("acknowledged request is still pending")
	}
	if length, _ := client.XLen(ctx, stream).Result(); length != 0 || publications != 2 {
		t.Fatalf("unexpected retained stream or publication count: %d/%d", length, publications)
	}
}

func TestRelayReplaysAfterCrashBetweenPublishAndAcknowledge(t *testing.T) {
	publications, acknowledgements := 0, 0
	publish := func(context.Context) error { publications++; return nil }
	crash := errors.New("process lost Redis connection after publish")
	if err := publishThenAcknowledge(context.Background(), publish, func(context.Context) error { return crash }); !errors.Is(err, crash) {
		t.Fatal(err)
	}
	if err := publishThenAcknowledge(context.Background(), publish, func(context.Context) error { acknowledgements++; return nil }); err != nil {
		t.Fatal(err)
	}
	if publications != 2 || acknowledgements != 1 {
		t.Fatalf("expected at-least-once publish and one acknowledgement, got %d/%d", publications, acknowledgements)
	}
}
