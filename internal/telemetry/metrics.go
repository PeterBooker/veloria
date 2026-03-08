package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"veloria/internal/config"
)

// newMeterProvider creates a MeterProvider with the configured exporter.
// Use "none" to disable metric export (instruments work but data is dropped).
func newMeterProvider(ctx context.Context, cfg *config.Config, res *resource.Resource) (*metric.MeterProvider, error) {
	switch cfg.OTelExporterType {
	case "none":
		return metric.NewMeterProvider(metric.WithResource(res)), nil
	case "stdout":
		// stdout mode: no metric export (metrics are only meaningful when pushed).
		return metric.NewMeterProvider(metric.WithResource(res)), nil
	case "otlp":
		// The HTTP exporter reads OTEL_EXPORTER_OTLP_ENDPOINT and
		// OTEL_EXPORTER_OTLP_HEADERS from the environment automatically.
		reader, err := newOTLPMetricReader(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP metric reader: %w", err)
		}
		return metric.NewMeterProvider(
			metric.WithReader(reader),
			metric.WithResource(res),
		), nil
	default:
		return nil, fmt.Errorf("unsupported exporter type: %s", cfg.OTelExporterType)
	}
}

func newOTLPMetricReader(ctx context.Context) (metric.Reader, error) {
	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	return metric.NewPeriodicReader(exporter), nil
}
