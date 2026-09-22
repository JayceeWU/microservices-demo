package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Relay struct {
	Pool *pgxpool.Pool
	Bus  application.RealtimeBus
}

type pendingEvent struct {
	ID      string
	Payload []byte
}

func (r Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.publishOne(ctx); err != nil {
				slog.Warn("chat realtime relay retry", "error", err)
			}
		}
	}
}

func (r Relay) publishOne(ctx context.Context) error {
	var event pendingEvent
	err := platform.WithActorTx(ctx, r.Pool, "chat-relay", "", "", "service", "chat-relay", nil, nil, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT event_id::text,payload FROM chat.realtime_outbox WHERE published_at IS NULL ORDER BY occurred_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&event.ID, &event.Payload)
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	var payload map[string]any
	if err = json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	payload["event_id"] = event.ID
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err = r.Bus.Publish(ctx, encoded); err != nil {
		return err
	}
	return platform.WithActorTx(ctx, r.Pool, "chat-relay", "", "", "service", "chat-relay", nil, nil, func(ctx context.Context, tx pgx.Tx) error {
		_, updateErr := tx.Exec(ctx, `UPDATE chat.realtime_outbox SET published_at=now(),attempts=attempts+1 WHERE event_id=$1 AND published_at IS NULL`, event.ID)
		return updateErr
	})
}
