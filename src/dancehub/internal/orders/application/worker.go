package application

import (
	"context"
	"log/slog"
	"time"
)

type FulfillmentWorker struct {
	Service  *Service
	Interval time.Duration
}

func (w FulfillmentWorker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := w.Service.ProcessRefunds(ctx); err != nil {
			slog.Warn("refund worker failed", "error", err)
		}
		if err := w.Service.ClaimAndFulfillOne(ctx); err != nil {
			slog.Warn("order fulfillment attempt failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
