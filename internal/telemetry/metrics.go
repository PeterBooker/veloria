package telemetry

import (
	"fmt"
	"net/http"

	promhttp "github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"veloria/internal/config"
)

// MetricsResult contains the meter provider and optional Prometheus handler.
type MetricsResult struct {
	Provider          *metric.MeterProvider
	PrometheusHandler http.Handler
}

// newMeterProvider creates a MeterProvider backed by a Prometheus scrape endpoint.
// Metrics are never pushed — they are always scraped via the /metrics HTTP handler.
func newMeterProvider(_ *config.Config, res *resource.Resource) (*MetricsResult, error) {
	reader, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	return &MetricsResult{
		Provider: metric.NewMeterProvider(
			metric.WithReader(reader),
			metric.WithResource(res),
		),
		PrometheusHandler: promhttp.Handler(),
	}, nil
}
