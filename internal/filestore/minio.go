package filestore

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/gauravfs-14/webhookmind/internal/config"
)

type MinIOStore struct {
	client         *minio.Client
	internalClient *minio.Client // client with internal endpoint for presigned URLs
	bucket         string
	logger         *slog.Logger
}

func NewMinIOStore(cfg config.MinIOConfig, logger *slog.Logger) (*MinIOStore, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}

	// Create a second client for internal (Docker-accessible) presigned URLs.
	// Set region explicitly so PresignedGetObject doesn't need to call getBucketLocation.
	internalEndpoint := cfg.Endpoint
	if cfg.InternalEndpoint != "" {
		internalEndpoint = cfg.InternalEndpoint
	}
	internalClient, err := minio.New(internalEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: "us-east-1",
	})
	if err != nil {
		return nil, fmt.Errorf("minio internal client: %w", err)
	}

	store := &MinIOStore{
		client:         client,
		internalClient: internalClient,
		bucket:         cfg.Bucket,
		logger:         logger,
	}

	// Create bucket if it doesn't exist.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("minio bucket check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("minio make bucket: %w", err)
		}
		logger.Info("created minio bucket", "bucket", cfg.Bucket)
	}

	return store, nil
}

func (s *MinIOStore) Upload(ctx context.Context, objectPath string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, objectPath, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("minio put object: %w", err)
	}
	return nil
}

func (s *MinIOStore) Download(ctx context.Context, objectPath string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio get object: %w", err)
	}
	return obj, nil
}

func (s *MinIOStore) GetPresignedURL(ctx context.Context, objectPath string, expiry time.Duration) (string, error) {
	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucket, objectPath, expiry, url.Values{})
	if err != nil {
		return "", fmt.Errorf("minio presign: %w", err)
	}
	return presignedURL.String(), nil
}

// GetInternalPresignedURL generates a presigned URL accessible from Docker containers.
// PresignedGetObject computes the signature locally (no network call needed),
// so using the internal client works even though host.docker.internal isn't reachable from the host.
func (s *MinIOStore) GetInternalPresignedURL(ctx context.Context, objectPath string, expiry time.Duration) (string, error) {
	presignedURL, err := s.internalClient.PresignedGetObject(ctx, s.bucket, objectPath, expiry, url.Values{})
	if err != nil {
		return "", fmt.Errorf("minio internal presign: %w", err)
	}
	return presignedURL.String(), nil
}
