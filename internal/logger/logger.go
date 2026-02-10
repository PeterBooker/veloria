package logger

import (
	"os"

	"github.com/rs/zerolog"
)

func New(isDebug bool) *zerolog.Logger {
	logLevel := zerolog.WarnLevel
	if isDebug {
		logLevel = zerolog.TraceLevel
	}

	zerolog.SetGlobalLevel(logLevel)
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	return &logger
}
