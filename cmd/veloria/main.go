package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"veloria/internal/app"
)

func main() {
	a, err := app.New(context.Background())
	if err != nil {
		log.Fatalf("failed to initialize application: %v", err)
	}

	closed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.Config.HTTPShutdownTimeout)
		defer cancel()

		if err := a.Shutdown(shutdownCtx); err != nil {
			a.Logger.Error().Err(err).Msg("Shutdown failure")
		}

		close(closed)
	}()

	if err := a.Start(); err != nil {
		a.Logger.Fatal().Err(err).Msg("Server startup failure")
	}

	<-closed
}
