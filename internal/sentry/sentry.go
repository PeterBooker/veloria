package sentry

import (
	"log"
	"time"

	"github.com/getsentry/sentry-go"

	"veloria/internal/config"
)

func Setup(c *config.Config) {
	sentrySyncTransport := sentry.NewHTTPSyncTransport()
	sentrySyncTransport.Timeout = time.Second * 3

	if c.SentryDSN == "" {
		log.Println("Sentry DSN is not set, skipping Sentry initialization")
		return
	}

	err := sentry.Init(sentry.ClientOptions{
		Transport:        sentrySyncTransport,
		Dsn:              c.SentryDSN,
		Environment:      c.Env,
		Release:          config.Version,
		SampleRate:       c.SentrySampleRate,
		EnableTracing:    true,
		TracesSampleRate: c.SentryTracesSampleRate,
		AttachStacktrace: true,
	})

	if err != nil {
		log.Printf("Sentry initialization failed: %v", err)
	}
}

func Flush() bool {
	return sentry.Flush(2 * time.Second)
}
