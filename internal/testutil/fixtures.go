package testutil

import (
	"github.com/google/uuid"
	"go.uber.org/zap"

	"veloria/internal/repo"
)

// NopLogger returns a *zap.Logger that discards all output.
func NopLogger() *zap.Logger {
	return zap.NewNop()
}

// SamplePlugin returns a Plugin with realistic test data.
func SamplePlugin() *repo.Plugin {
	return &repo.Plugin{
		ID:               uuid.New(),
		Name:             "Test Plugin",
		Slug:             "test-plugin",
		Source:           repo.SourceWordPress,
		Version:          "1.0.0",
		ShortDescription: "A test plugin for unit testing",
		ActiveInstalls:   1000,
		Downloaded:       5000,
		DownloadLink:     "https://downloads.wordpress.org/plugin/test-plugin.1.0.0.zip",
	}
}

