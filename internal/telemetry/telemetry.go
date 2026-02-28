package telemetry

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"

	"veloria/internal/config"
	velorialog "veloria/internal/log"
)

// Telemetry holds the initialized telemetry components.
type Telemetry struct {
	// Logger is the configured Zap logger with OTel integration.
	Logger *zap.Logger
	// PrometheusHandler is the HTTP handler for /metrics endpoint (nil if not enabled).
	PrometheusHandler http.Handler

	shutdownFuncs []func(context.Context) error
}

// Shutdown gracefully shuts down all telemetry providers.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs error
	for i := len(t.shutdownFuncs) - 1; i >= 0; i-- {
		if err := t.shutdownFuncs[i](ctx); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// Setup initializes the OpenTelemetry SDK with the provided configuration.
func Setup(ctx context.Context, cfg *config.Config) (*Telemetry, error) {
	t := &Telemetry{}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Set up propagator for distributed tracing.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Set up tracer provider.
	tp, err := newTracerProvider(ctx, cfg, res)
	if err != nil {
		return t, errors.Join(err, t.Shutdown(ctx))
	}
	otel.SetTracerProvider(tp)
	t.shutdownFuncs = append(t.shutdownFuncs, tp.Shutdown)

	// Set up meter provider (always Prometheus-scraped, never pushed).
	metricsResult, err := newMeterProvider(cfg, res)
	if err != nil {
		return t, errors.Join(err, t.Shutdown(ctx))
	}
	otel.SetMeterProvider(metricsResult.Provider)
	t.shutdownFuncs = append(t.shutdownFuncs, metricsResult.Provider.Shutdown)
	t.PrometheusHandler = metricsResult.PrometheusHandler

	// Set up runtime metrics if enabled.
	if cfg.EnableRuntimeMetrics {
		if err := runtime.Start(); err != nil {
			return t, errors.Join(err, t.Shutdown(ctx))
		}
	}

	// Set up logger provider.
	loggingResult, err := newLoggerProvider(ctx, cfg, res)
	if err != nil {
		return t, errors.Join(err, t.Shutdown(ctx))
	}
	if loggingResult.Provider != nil {
		global.SetLoggerProvider(loggingResult.Provider)
		t.shutdownFuncs = append(t.shutdownFuncs, loggingResult.Provider.Shutdown)
	}
	t.Logger = loggingResult.Logger

	// Replace global loggers.
	velorialog.SetGlobal(t.Logger)

	return t, nil
}
