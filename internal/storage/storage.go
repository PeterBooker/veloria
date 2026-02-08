package storage

import (
	"context"

	"google.golang.org/protobuf/proto"
)

// ResultStorage defines the interface for storing search results.
// This allows for different implementations (S3, local filesystem, etc.).
type ResultStorage interface {
	// UploadResult stores search results as zstd-compressed protobuf.
	// The searchID is used to generate the object key.
	// Returns the compressed size in bytes.
	UploadResult(ctx context.Context, searchID string, result proto.Message) (size int64, err error)

	// DownloadResult retrieves and decompresses search results.
	// The searchID is used to generate the object key.
	DownloadResult(ctx context.Context, searchID string, result proto.Message) error

	// DeleteResult removes search results from storage.
	DeleteResult(ctx context.Context, searchID string) error

	// EnsureBucket verifies the bucket exists and is accessible.
	EnsureBucket(ctx context.Context) error

	// GetObjectKey returns the S3 object key for a given search ID.
	GetObjectKey(searchID string) string
}
