package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

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
			a.Logger.Error("Shutdown failure", zap.Error(err))
		}

		close(closed)
	}()

	if err := a.Start(); err != nil {
		a.Logger.Fatal("Server startup failure", zap.Error(err))
	}

	<-closed
}
