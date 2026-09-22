package platform

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

func InitTelemetry(serviceName string) func(context.Context) error {
	endpoint := strings.TrimRight(MustEnv("OTEL_EXPORTER_OTLP_ENDPOINT"), "/")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, _ := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName(serviceName), semconv.DeploymentEnvironmentName(MustEnv("ENVIRONMENT"))))
	traceExporter, traceErr := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"))
	metricExporter, metricErr := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint+"/v1/metrics"))
	if traceErr != nil || metricErr != nil {
		slog.Warn("telemetry exporter unavailable; business traffic will continue", "trace_error", traceErr, "metric_error", metricErr)
		return func(context.Context) error { return nil }
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res))
	mp := metric.NewMeterProvider(metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(10*time.Second))), metric.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return func(ctx context.Context) error { _ = mp.Shutdown(ctx); return tp.Shutdown(ctx) }
}

func ConfigureJSONLogging(serviceName string) {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", serviceName, "environment", MustEnv("ENVIRONMENT")))
}
