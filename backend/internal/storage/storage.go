// Package storage wraps the S3-compatible object store. Browsers upload and
// download directly with short-lived presigned URLs; the API never proxies file bytes.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint       string // host:port the API uses, e.g. minio:9000
	PublicEndpoint string // host:port browsers use, e.g. files.example.com
	AccessKey      string
	SecretKey      string
	Bucket         string
	Region         string
	UseSSL         bool
	PublicSSL      bool
}

type Storage struct {
	internal *minio.Client
	public   *minio.Client // only signs URLs; never dials
	bucket   string
}

func New(ctx context.Context, cfg Config) (*Storage, error) {
	creds := credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")
	// Region is fixed so presigning never needs a network round-trip.
	internal, err := minio.New(cfg.Endpoint, &minio.Options{Creds: creds, Secure: cfg.UseSSL, Region: cfg.Region})
	if err != nil {
		return nil, err
	}
	public, err := minio.New(cfg.PublicEndpoint, &minio.Options{Creds: creds, Secure: cfg.PublicSSL, Region: cfg.Region})
	if err != nil {
		return nil, err
	}
	s := &Storage{internal: internal, public: public, bucket: cfg.Bucket}
	exists, err := internal.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := internal.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}
	return s, nil
}

// PresignPut returns a URL the browser PUTs the file to. The Content-Type header
// is part of the signature, so the browser must send exactly the same value.
func (s *Storage) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, map[string]string, error) {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	u, err := s.public.PresignHeader(ctx, http.MethodPut, s.bucket, key, ttl, nil, h)
	if err != nil {
		return "", nil, err
	}
	return u.String(), map[string]string{"Content-Type": contentType}, nil
}

// PresignGet returns a download URL that saves the file under its original name.
func (s *Storage) PresignGet(ctx context.Context, key, filename string, ttl time.Duration) (string, error) {
	q := url.Values{}
	q.Set("response-content-disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	u, err := s.public.PresignedGetObject(ctx, s.bucket, key, ttl, q)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

type Object struct {
	Size        int64
	ContentType string
}

var ErrNotFound = fmt.Errorf("object not found")

func (s *Storage) Stat(ctx context.Context, key string) (Object, error) {
	info, err := s.internal.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return Object{}, ErrNotFound
		}
		return Object{}, err
	}
	return Object{Size: info.Size, ContentType: info.ContentType}, nil
}

// SHA256 streams the stored object and hashes it, so the checksum we record is
// what is actually stored, not what a client claimed.
func (s *Storage) SHA256(ctx context.Context, key string) (string, error) {
	obj, err := s.internal.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}
	defer obj.Close()
	h := sha256.New()
	if _, err := io.Copy(h, obj); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Storage) Remove(ctx context.Context, key string) error {
	return s.internal.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *Storage) Ping(ctx context.Context) error {
	_, err := s.internal.BucketExists(ctx, s.bucket)
	return err
}
