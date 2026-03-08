package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

	"veloria/internal/config"
)

// newResource creates an OpenTelemetry resource identifying this service.
// Standard OTel env vars (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES) take
// precedence over the code defaults via WithFromEnv().
func newResource(ctx context.Context, cfg *config.Config) (*resource.Resource, error) {
	hostname, _ := os.Hostname()

	instanceID := cfg.OTelServiceInstanceID
	if instanceID == "" {
		instanceID = hostname
	}

	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.Name),
			semconv.ServiceNamespace(cfg.OTelServiceNamespace),
			semconv.ServiceInstanceID(instanceID),
			semconv.ServiceVersion(config.Version),
			semconv.DeploymentEnvironmentName(cfg.Env),
			semconv.HostName(hostname),
		),
		resource.WithTelemetrySDK(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithFromEnv(),
	)
}
