package testutil

import (
	"github.com/google/uuid"
	"go.uber.org/zap"

	"veloria/internal/config"
	"veloria/internal/repo"
)

// NopLogger returns a *zap.Logger that discards all output.
func NopLogger() *zap.Logger {
	return zap.NewNop()
}

// SampleConfig returns a valid Config with test-friendly defaults.
func SampleConfig() *config.Config {
	return &config.Config{
		Name:               "Veloria Test",
		Port:               9071,
		Env:                "development",
		WorkingDir:         "/tmp",
		DataDir:            "/tmp/veloria-test-data",
		HTTPHandlerTimeout: 0,
		DBHost:             "localhost",
		DBPort:             5432,
		DBName:             "veloria_test",
		DBUser:             "postgres",
		DBPass:             "postgres",
		DBSSLMode:          "disable",
		DBTimeZone:         "UTC",
		DBConnectTimeout:   5,
		IndexerConcurrency: 1,
		SearchConcurrency:  6,
		S3Endpoint:         "localhost:9000",
		S3Bucket:           "veloria-test",
		S3AccessKey:        "minioadmin",
		S3SecretKey:        "minioadmin",
		S3EnsureBucket:     true,
		AspireCloudAPIKey:  "test-key",
	}
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

// SampleTheme returns a Theme with realistic test data.
func SampleTheme() *repo.Theme {
	return &repo.Theme{
		ID:               uuid.New(),
		Name:             "Test Theme",
		Slug:             "test-theme",
		Source:           repo.SourceWordPress,
		Version:          "2.0.0",
		ShortDescription: "A test theme for unit testing",
		ActiveInstalls:   500,
		Downloaded:       2000,
		DownloadLink:     "https://downloads.wordpress.org/theme/test-theme.2.0.0.zip",
	}
}

// SampleCore returns a Core with realistic test data.
func SampleCore() *repo.Core {
	return &repo.Core{
		ID:      uuid.New(),
		Name:    "WordPress 6.8",
		Version: "6.8",
	}
}
