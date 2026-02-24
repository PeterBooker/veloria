package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"

	"veloria/internal/config"
	veloriamc "veloria/internal/mcp"
)

const defaultURL = "http://localhost:9071"

func main() {
	baseURL := os.Getenv("VELORIA_URL")
	if baseURL == "" {
		baseURL = defaultURL
	}

	svc, err := veloriamc.NewAPIService(baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "veloria-mcp: %v\n", err)
		os.Exit(1)
	}
	mcpServer := veloriamc.NewMCPServer("veloria", config.Version, svc)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := server.ServeStdio(mcpServer, server.WithStdioContextFunc(func(_ context.Context) context.Context {
		return ctx
	})); err != nil {
		fmt.Fprintf(os.Stderr, "veloria-mcp: %v\n", err)
		os.Exit(1)
	}
}
