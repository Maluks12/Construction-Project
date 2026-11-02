package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type S3Config struct {
	Bucket string
	Region string
	// Endpoint is empty for AWS. PublicEndpoint is the host clients upload to;
	// it is part of the signature, so it must be the one they call.
	Endpoint       string
	PublicEndpoint string
	// Both empty: the default AWS credential chain.
	AccessKeyID     string
	SecretAccessKey string
}

type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

var _ Store = (*S3)(nil)

func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	opts := []func(*config.LoadOptions) error{config.WithRegion(cfg.Region)}
	if cfg.AccessKeyID != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("new s3: %w", err)
	}

	public := cfg.PublicEndpoint
	if public == "" {
		public = cfg.Endpoint
	}
	return &S3{
		client:  s3.NewFromConfig(awsCfg, withEndpoint(cfg.Endpoint)),
		presign: s3.NewPresignClient(s3.NewFromConfig(awsCfg, withEndpoint(public))),
		bucket:  cfg.Bucket,
	}, nil
}

// withEndpoint points the client at MinIO; path-style because MinIO has no
// per-bucket DNS names.
func withEndpoint(endpoint string) func(*s3.Options) {
	return func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	}
}

func (s *S3) PresignPut(ctx context.Context, key string, sum []byte, ttl time.Duration) (*PresignedPut, error) {
	expires := time.Now().Add(ttl)
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(s.bucket),
		Key:            aws.String(key),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum)),
		IfNoneMatch:    aws.String("*"),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return nil, fmt.Errorf("presign put: %w", err)
	}

	headers := map[string]string{}
	for name := range req.SignedHeader {
		if http.CanonicalHeaderKey(name) != "Host" {
			headers[http.CanonicalHeaderKey(name)] = req.SignedHeader.Get(name)
		}
	}
	return &PresignedPut{Method: req.Method, URL: req.URL, Headers: headers, ExpiresAt: expires}, nil
}

func (s *S3) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if isNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", key, err)
	}
	return &ObjectInfo{
		Size:        aws.ToInt64(out.ContentLength),
		VersionID:   aws.ToString(out.VersionId),
		LockMode:    string(out.ObjectLockMode),
		RetainUntil: out.ObjectLockRetainUntilDate,
	}, nil
}

func (s *S3) Open(ctx context.Context, key, versionID string) (io.ReadCloser, error) {
	in := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	// S3 rejects an empty versionId; omitting it reads the latest version.
	if versionID != "" {
		in.VersionId = aws.String(versionID)
	}
	out, err := s.client.GetObject(ctx, in)
	if isNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", key, err)
	}
	return out.Body, nil
}

// CheckObjectLock fails unless every new object in the bucket gets retention
// in mode. Retention comes from the bucket default, so no upload can skip it.
func (s *S3) CheckObjectLock(ctx context.Context, mode string) error {
	out, err := s.client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("check object lock on %s: %w", s.bucket, err)
	}
	cfg := out.ObjectLockConfiguration
	if cfg == nil || cfg.ObjectLockEnabled != types.ObjectLockEnabledEnabled {
		return fmt.Errorf("check object lock on %s: object lock is not enabled", s.bucket)
	}
	if cfg.Rule == nil || cfg.Rule.DefaultRetention == nil {
		return fmt.Errorf("check object lock on %s: no default retention", s.bucket)
	}
	if got := string(cfg.Rule.DefaultRetention.Mode); got != mode {
		return fmt.Errorf("check object lock on %s: default retention is %s, want %s", s.bucket, got, mode)
	}
	return nil
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "NotFound", "NoSuchKey", "NoSuchVersion":
		return true
	}
	return false
}
