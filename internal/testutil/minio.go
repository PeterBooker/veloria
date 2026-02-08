//go:build integration

package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	"veloria/internal/config"
	"veloria/internal/storage"
)

// NewTestS3 spins up a real MinIO container and returns a connected ResultStorage.
// The container is automatically cleaned up when the test finishes.
func NewTestS3(t *testing.T) storage.ResultStorage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := minio.Run(ctx, "minio/minio:latest",
		minio.WithUsername("minioadmin"),
		minio.WithPassword("minioadmin"),
	)
	if err != nil {
		t.Fatalf("failed to start minio container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate minio container: %v", err)
		}
	})

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get minio connection string: %v", err)
	}

	logger := zerolog.Nop()
	cfg := &config.Config{
		S3Endpoint:     endpoint,
		S3AccessKey:    "minioadmin",
		S3SecretKey:    "minioadmin",
		S3Bucket:       "veloria-test",
		S3UseSSL:       false,
		S3Region:       "us-east-1",
		S3EnsureBucket: true,
	}

	s3Client, err := storage.NewS3Client(cfg, &logger)
	if err != nil {
		t.Fatalf("failed to create S3 client: %v", err)
	}

	if err := s3Client.EnsureBucket(ctx); err != nil {
		t.Fatalf("failed to ensure bucket: %v", err)
	}

	return s3Client
}
