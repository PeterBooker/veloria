package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

	"veloria/internal/config"
)

// newResource creates an OpenTelemetry resource identifying this service.
func newResource(ctx context.Context, cfg *config.Config) (*resource.Resource, error) {
	hostname, _ := os.Hostname()

	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.Name),
			semconv.ServiceVersion(config.Version),
			semconv.DeploymentEnvironmentName(cfg.Env),
			semconv.HostName(hostname),
		),
		resource.WithTelemetrySDK(),
		resource.WithOS(),
		resource.WithProcess(),
	)
}
