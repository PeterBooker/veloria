package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"veloria/internal/config"
)

// S3Client handles object storage operations for search results.
type S3Client struct {
	client       *minio.Client
	bucket       string
	l            *zap.Logger
	ensureBucket bool
	zstdEncPool  sync.Pool
	zstdDecPool  sync.Pool
}

// NewS3Client creates a new S3 client from configuration.
func NewS3Client(c *config.Config, l *zap.Logger) (*S3Client, error) {
	client, err := minio.New(c.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.S3AccessKey, c.S3SecretKey, ""),
		Secure: c.S3UseSSL,
		Region: c.S3Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Verify that zstd encoder/decoder creation works before returning.
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	_ = enc.Close()

	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	dec.Close()

	s3c := &S3Client{
		client:       client,
		bucket:       c.S3Bucket,
		l:            l,
		ensureBucket: c.S3EnsureBucket,
	}
	s3c.zstdEncPool = sync.Pool{
		New: func() any {
			w, _ := zstd.NewWriter(nil)
			return w
		},
	}
	s3c.zstdDecPool = sync.Pool{
		New: func() any {
			r, _ := zstd.NewReader(nil)
			return r
		},
	}
	return s3c, nil
}

// EnsureBucket verifies the bucket exists and is accessible.
func (s *S3Client) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket: %w", err)
	}
	if !exists {
		if !s.ensureBucket {
			return fmt.Errorf("bucket %q does not exist", s.bucket)
		}
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		s.l.Info("Created S3 bucket", zap.String("bucket", s.bucket))
	}
	return nil
}

// GetObjectKey returns the S3 object key for a given search ID.
// Format: searches/{searchID}.pb.zst
func (s *S3Client) GetObjectKey(searchID string) string {
	return fmt.Sprintf("searches/%s.pb.zst", searchID)
}

func (s *S3Client) getLegacyProtoGzipKey(searchID string) string {
	return fmt.Sprintf("searches/%s.pb.gz", searchID)
}

func (s *S3Client) getLegacyObjectKey(searchID string) string {
	return fmt.Sprintf("searches/%s.json.gz", searchID)
}

// UploadResult stores search results as zstd-compressed protobuf.
// Returns the compressed size in bytes.
func (s *S3Client) UploadResult(ctx context.Context, searchID string, result proto.Message) (int64, error) {
	if result == nil {
		return 0, errors.New("result is nil")
	}

	key := s.GetObjectKey(searchID)

	protoData, err := proto.Marshal(result)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal result: %w", err)
	}

	// Compress with zstd
	enc := s.zstdEncPool.Get().(*zstd.Encoder)
	compressedData := enc.EncodeAll(protoData, nil)
	s.zstdEncPool.Put(enc)
	compressedSize := int64(len(compressedData))

	// Upload to S3
	_, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(compressedData), compressedSize, minio.PutObjectOptions{
		ContentType:     "application/x-protobuf",
		ContentEncoding: "zstd",
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload to S3: %w", err)
	}

	s.l.Debug("Uploaded search result to S3", zap.String("key", key), zap.Int64("size", compressedSize))
	return compressedSize, nil
}

// DownloadResult retrieves and decompresses search results from S3.
func (s *S3Client) DownloadResult(ctx context.Context, searchID string, result proto.Message) error {
	if result == nil {
		return errors.New("result is nil")
	}

	key := s.GetObjectKey(searchID)
	data, err := s.readZstdObject(ctx, key)
	if err != nil {
		if isNoSuchKey(err) {
			if err := s.downloadLegacyProtoGzipResult(ctx, searchID, result); err != nil {
				if isNoSuchKey(err) {
					return s.downloadLegacyResult(ctx, searchID, result)
				}
				return err
			}
			return nil
		}
		return fmt.Errorf("failed to read result: %w", err)
	}

	if err := proto.Unmarshal(data, result); err != nil {
		return fmt.Errorf("failed to unmarshal result: %w", err)
	}
	return nil
}

// DeleteResult removes a search result from S3.
func (s *S3Client) DeleteResult(ctx context.Context, searchID string) error {
	key := s.GetObjectKey(searchID)
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !isNoSuchKey(err) {
		return fmt.Errorf("failed to delete object from S3: %w", err)
	}

	legacyProtoKey := s.getLegacyProtoGzipKey(searchID)
	legacyProtoErr := s.client.RemoveObject(ctx, s.bucket, legacyProtoKey, minio.RemoveObjectOptions{})
	if legacyProtoErr != nil && !isNoSuchKey(legacyProtoErr) {
		return fmt.Errorf("failed to delete legacy proto object from S3: %w", legacyProtoErr)
	}

	legacyKey := s.getLegacyObjectKey(searchID)
	legacyErr := s.client.RemoveObject(ctx, s.bucket, legacyKey, minio.RemoveObjectOptions{})
	if legacyErr != nil && !isNoSuchKey(legacyErr) {
		return fmt.Errorf("failed to delete legacy object from S3: %w", legacyErr)
	}
	return nil
}

// DeleteAllResults removes all search result objects from the S3 bucket.
// Returns the number of objects deleted.
func (s *S3Client) DeleteAllResults(ctx context.Context) (int, error) {
	objectsCh := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    "searches/",
		Recursive: true,
	})

	deleted := 0
	for obj := range objectsCh {
		if obj.Err != nil {
			return deleted, fmt.Errorf("failed to list objects: %w", obj.Err)
		}
		if err := s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			return deleted, fmt.Errorf("failed to delete %q: %w", obj.Key, err)
		}
		deleted++
	}
	return deleted, nil
}

func (s *S3Client) readZstdObject(ctx context.Context, key string) (data []byte, err error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := obj.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	if _, err := obj.Stat(); err != nil {
		return nil, err
	}

	compressedData, err := io.ReadAll(obj)
	if err != nil {
		return nil, err
	}

	dec := s.zstdDecPool.Get().(*zstd.Decoder)
	data, err = dec.DecodeAll(compressedData, nil)
	s.zstdDecPool.Put(dec)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (s *S3Client) readGzipObject(ctx context.Context, key string) (data []byte, err error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := obj.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	if _, err := obj.Stat(); err != nil {
		return nil, err
	}

	gzReader, err := gzip.NewReader(obj)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := gzReader.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	data, err = io.ReadAll(gzReader)
	return data, err
}

func (s *S3Client) downloadLegacyProtoGzipResult(ctx context.Context, searchID string, result proto.Message) error {
	legacyKey := s.getLegacyProtoGzipKey(searchID)
	data, err := s.readGzipObject(ctx, legacyKey)
	if err != nil {
		if isNoSuchKey(err) {
			return err
		}
		return fmt.Errorf("failed to read legacy proto result: %w", err)
	}
	if err := proto.Unmarshal(data, result); err != nil {
		return fmt.Errorf("failed to unmarshal legacy result: %w", err)
	}
	return nil
}

func (s *S3Client) downloadLegacyResult(ctx context.Context, searchID string, result proto.Message) error {
	legacyKey := s.getLegacyObjectKey(searchID)
	data, err := s.readGzipObject(ctx, legacyKey)
	if err != nil {
		return fmt.Errorf("failed to read legacy result: %w", err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, result); err != nil {
		return fmt.Errorf("failed to unmarshal legacy result: %w", err)
	}
	return nil
}

func isNoSuchKey(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject"
}
