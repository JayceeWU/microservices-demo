package platform

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// TraceHeaders serializes the active trace context for durable outbox rows and
// message headers. Empty values are valid when telemetry is disabled.
func TraceHeaders(ctx context.Context) (traceparent, tracestate, baggage string) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent"), carrier.Get("tracestate"), carrier.Get("baggage")
}

// ContextFromTraceHeaders restores a message producer context for a worker.
func ContextFromTraceHeaders(ctx context.Context, traceparent, tracestate, baggage string) context.Context {
	carrier := propagation.MapCarrier{}
	carrier.Set("traceparent", traceparent)
	carrier.Set("tracestate", tracestate)
	carrier.Set("baggage", baggage)
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
