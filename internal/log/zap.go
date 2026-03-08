package log

import (
	stdlog "log"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config holds logging configuration.
type Config struct {
	ServiceName string
	Development bool // Controls output format (console vs JSON)
	Debug       bool // Enables debug/info level output (errors/warnings always logged)
}

// NewZapLogger creates a Zap logger that bridges to OpenTelemetry.
// Development controls the output format: console with color (dev) or JSON (prod).
// Debug controls the minimum log level: Debug (on) or Warn (off).
// Errors and warnings are always logged regardless of Debug setting.
func NewZapLogger(cfg Config, otelProvider *sdklog.LoggerProvider) *zap.Logger {
	level := zapcore.WarnLevel
	if cfg.Development || cfg.Debug {
		level = zapcore.DebugLevel
	}

	// Create the local output core based on environment.
	var localCore zapcore.Core
	if cfg.Development {
		encoderConfig := zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		localCore = zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderConfig),
			zapcore.AddSync(os.Stdout),
			level,
		)
	} else {
		encoderConfig := zap.NewProductionEncoderConfig()
		encoderConfig.TimeKey = "timestamp"
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		localCore = zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig),
			zapcore.AddSync(os.Stdout),
			level,
		)
	}

	// If an OTel provider is available, tee both cores together.
	core := localCore
	if otelProvider != nil {
		otelCore := otelzap.NewCore(
			cfg.ServiceName,
			otelzap.WithLoggerProvider(otelProvider),
		)
		core = zapcore.NewTee(localCore, otelCore)
	}

	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

// SetGlobal replaces the global Zap logger and redirects stdlib log.
func SetGlobal(logger *zap.Logger) {
	zap.ReplaceGlobals(logger)
	_ = zap.RedirectStdLog(logger)
	stdlog.SetFlags(0)
}
