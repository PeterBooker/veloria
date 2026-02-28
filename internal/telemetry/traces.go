package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"

	"veloria/internal/config"
)

// newTracerProvider creates a TracerProvider with the configured exporter.
// Use "none" to disable trace export (spans are created but dropped).
func newTracerProvider(ctx context.Context, cfg *config.Config, res *resource.Resource) (*trace.TracerProvider, error) {
	switch cfg.OTelExporterType {
	case "none":
		// No exporter — spans are created (so instrumentation works) but not exported.
		return trace.NewTracerProvider(trace.WithResource(res)), nil
	case "stdout":
		exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout trace exporter: %w", err)
		}
		return newTracerProviderWithExporter(cfg, res, exporter), nil
	case "otlp":
		exporter, err := newOTLPTraceExporter(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
		return newTracerProviderWithExporter(cfg, res, exporter), nil
	default:
		return nil, fmt.Errorf("unsupported exporter type: %s", cfg.OTelExporterType)
	}
}

func newTracerProviderWithExporter(cfg *config.Config, res *resource.Resource, exporter trace.SpanExporter) *trace.TracerProvider {
	return trace.NewTracerProvider(
		trace.WithBatcher(exporter,
			trace.WithBatchTimeout(cfg.TraceBatchTimeout),
		),
		trace.WithResource(res),
	)
}

func newOTLPTraceExporter(ctx context.Context, cfg *config.Config) (trace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
	}
	if cfg.OTLPInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, opts...)
}
