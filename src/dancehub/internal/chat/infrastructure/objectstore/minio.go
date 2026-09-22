package objectstore

import (
	"context"
	"io"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client        *minio.Client
	presignClient *minio.Client
	bucket        string
}

func New(endpoint, publicEndpoint, accessKey, secretKey, bucket string, secure bool) (*Store, error) {
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure})
	if err != nil {
		return nil, err
	}
	if publicEndpoint == "" {
		publicEndpoint = endpoint
	}
	presignClient, err := minio.New(publicEndpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure, Region: "us-east-1"})
	if err != nil {
		return nil, err
	}
	return &Store{client: client, presignClient: presignClient, bucket: bucket}, nil
}

func (s *Store) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}

func (s *Store) PresignUpload(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	_ = contentType
	value, err := s.presignClient.PresignedPutObject(ctx, s.bucket, key, ttl)
	if err != nil {
		return "", err
	}
	return value.String(), nil
}

func (s *Store) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	value, err := s.presignClient.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return "", err
	}
	return value.String(), nil
}

func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

// Copy creates a worker-owned snapshot before scanning. Only quarantine keys
// receive browser upload URLs; every snapshot destination is unique per attempt.
func (s *Store) Copy(ctx context.Context, source, destination string) error {
	_, err := s.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: s.bucket, Object: destination},
		minio.CopySrcOptions{Bucket: s.bucket, Object: source})
	return err
}

func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

var _ application.ObjectStore = (*Store)(nil)
