package projection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Consumer struct {
	Brokers []string
	Store   Store
	mu      sync.RWMutex
	ready   bool
}

func (c *Consumer) Health(context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.ready {
		return errors.New("payroll projection is not ready")
	}
	return nil
}

func (c *Consumer) setReady(ready bool) {
	c.mu.Lock()
	c.ready = ready
	c.mu.Unlock()
}

// Run recreates the consumer after any failed application or offset commit.
// The next session starts at the committed offset, so later records can never
// cause an earlier failed record to be acknowledged. The inbox absorbs replay
// after a database commit followed by a failed Kafka commit.
func (c *Consumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		err := c.consume(ctx)
		c.setReady(false)
		if ctx.Err() != nil {
			return
		}
		slog.Error("payroll projection will retry from its committed offset", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	client, err := kgo.NewClient(kgo.SeedBrokers(c.Brokers...), kgo.ConsumerGroup(Group),
		kgo.ConsumeTopics(Topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll())
	if err != nil {
		return err
	}
	defer client.CloseAllowingRebalance()
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = client.Ping(pingCtx)
	cancel()
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		pollCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		fetches := client.PollRecords(pollCtx, 1)
		stop()
		for _, failure := range fetches.Errors() {
			if !errors.Is(failure.Err, context.DeadlineExceeded) {
				return fmt.Errorf("fetch %s/%d: %w", failure.Topic, failure.Partition, failure.Err)
			}
		}
		for _, record := range fetches.Records() {
			workCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			err = processRecord(workCtx, c.Store, record, func(ctx context.Context, record *kgo.Record) error {
				return client.CommitRecords(ctx, record)
			})
			stop()
			if err != nil {
				return fmt.Errorf("process %s/%d/%d: %w", record.Topic, record.Partition, record.Offset, err)
			}
		}
		client.AllowRebalance()
		c.setReady(true)
	}
	return ctx.Err()
}

func processRecord(ctx context.Context, store Store, record *kgo.Record, commit func(context.Context, *kgo.Record) error) error {
	event, relevant, err := Decode(record.Value)
	if err != nil {
		return err
	}
	if relevant {
		carrier := propagation.MapCarrier{"traceparent": event.Traceparent}
		for _, header := range record.Headers {
			carrier[header.Key] = string(header.Value)
		}
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
		ctx, span := otel.Tracer("payroll-projector").Start(ctx, "payroll.project", trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(attribute.String("messaging.destination.name", Topic), attribute.String("event.id", event.ID),
				attribute.Int64("payroll.fact.version", event.Fact.Version)))
		err = store.Apply(ctx, event)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "payroll projection failed")
		}
		span.End()
		if err != nil {
			return err
		}
	}
	return commit(ctx, record)
}
