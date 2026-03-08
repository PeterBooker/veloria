package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/zap"

	"veloria/internal/config"
	velorialog "veloria/internal/log"
)

// LoggingResult contains the logger provider and configured Zap logger.
type LoggingResult struct {
	Provider *sdklog.LoggerProvider
	Logger   *zap.Logger
}

// newLoggerProvider creates a LoggerProvider and Zap logger with OTel bridge.
// Use "none" to disable OTel log export (Zap outputs locally only).
func newLoggerProvider(ctx context.Context, cfg *config.Config, res *resource.Resource) (*LoggingResult, error) {
	isDev := cfg.Env == "development" || cfg.Env == "dev"

	if cfg.OTelExporterType == "none" {
		// No OTel log export — Zap works standalone with local output only.
		zapLogger := velorialog.NewZapLogger(velorialog.Config{
			ServiceName: cfg.Name,
			Development: isDev,
			Debug:       cfg.AppDebug,
		}, nil)

		return &LoggingResult{Logger: zapLogger}, nil
	}

	var exporter sdklog.Exporter
	var err error

	switch cfg.OTelExporterType {
	case "stdout":
		exporter, err = stdoutlog.New()
	case "otlp":
		// The HTTP exporter reads OTEL_EXPORTER_OTLP_ENDPOINT and
		// OTEL_EXPORTER_OTLP_HEADERS from the environment automatically.
		exporter, err = otlploghttp.New(ctx)
	default:
		return nil, fmt.Errorf("unsupported exporter type: %s", cfg.OTelExporterType)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter,
			sdklog.WithExportTimeout(cfg.LogBatchTimeout),
		)),
		sdklog.WithResource(res),
	)

	zapLogger := velorialog.NewZapLogger(velorialog.Config{
		ServiceName: cfg.Name,
		Development: isDev,
		Debug:       cfg.AppDebug,
	}, provider)

	return &LoggingResult{
		Provider: provider,
		Logger:   zapLogger,
	}, nil
}
