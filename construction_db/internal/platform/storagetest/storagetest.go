package storagetest

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	"construction_db/internal/platform/storage"
)

// MinIO no longer publishes images; Chainguard's free build has only a
// "latest" tag, so it is pinned by digest. Keep in step with docker-compose.yml.
const Image = "cgr.dev/chainguard/minio@sha256:0a01100085e30fb08d2f39e39c9f5c9d83a95707f220b007d813b690cd41622d"

const (
	Bucket   = "evidence-test"
	user     = "minioadmin"
	password = "minioadmin"
)

type Env struct {
	Store  *storage.S3
	Admin  *s3.Client       // for fixtures: writes that bypass presigned URLs
	Config storage.S3Config // Bucket is the locked test bucket
}

// Start runs MinIO with Bucket created like production: Object Lock enabled
// and a default retention (GOVERNANCE, 1 day).
func Start(ctx context.Context) (*Env, func(), error) {
	ctr, err := minio.Run(ctx, Image, minio.WithUsername(user), minio.WithPassword(password))
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, nil, fmt.Errorf("start minio: %w", err)
	}
	cleanup := func() { _ = testcontainers.TerminateContainer(ctr) }

	host, err := ctr.ConnectionString(ctx)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("minio endpoint: %w", err)
	}
	cfg := storage.S3Config{
		Bucket:          Bucket,
		Region:          "af-south-1",
		Endpoint:        "http://" + host,
		AccessKeyID:     user,
		SecretAccessKey: password,
	}

	admin := AdminClient(cfg)
	if err := CreateBucket(ctx, admin, Bucket, true); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := SetDefaultRetention(ctx, admin, Bucket, types.ObjectLockRetentionModeGovernance); err != nil {
		cleanup()
		return nil, nil, err
	}

	store, err := storage.NewS3(ctx, cfg)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return &Env{Store: store, Admin: admin, Config: cfg}, cleanup, nil
}

func AdminClient(cfg storage.S3Config) *s3.Client {
	return s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
	})
}

func CreateBucket(ctx context.Context, admin *s3.Client, bucket string, lock bool) error {
	_, err := admin.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket:                     aws.String(bucket),
		ObjectLockEnabledForBucket: aws.Bool(lock),
	})
	if err != nil {
		return fmt.Errorf("create bucket %s: %w", bucket, err)
	}
	return nil
}

func SetDefaultRetention(ctx context.Context, admin *s3.Client, bucket string, mode types.ObjectLockRetentionMode) error {
	_, err := admin.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
		ObjectLockConfiguration: &types.ObjectLockConfiguration{
			ObjectLockEnabled: types.ObjectLockEnabledEnabled,
			Rule: &types.ObjectLockRule{
				DefaultRetention: &types.DefaultRetention{Mode: mode, Days: aws.Int32(1)},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("set default retention on %s: %w", bucket, err)
	}
	return nil
}
