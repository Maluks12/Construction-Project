package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// PartSize: memory stays at one part (S3's minimum is 5 MiB).
const PartSize = 8 << 20

// PutStream returns the stored version, so a download can pin it; a failure
// aborts the upload, leaving no object.
func (s *S3) PutStream(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	if contentType == "" {
		return "", fmt.Errorf("put stream %s: %w", key, ErrNoContentType)
	}
	created, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("put stream %s: %w", key, err)
	}
	abort := func(cause error) (string, error) {
		_, _ = s.client.AbortMultipartUpload(context.WithoutCancel(ctx), &s3.AbortMultipartUploadInput{
			Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId,
		})
		return "", fmt.Errorf("put stream %s: %w", key, cause)
	}

	buf := make([]byte, PartSize)
	var parts []types.CompletedPart
	for n := int32(1); ; n++ {
		k, rerr := io.ReadFull(r, buf)
		if rerr != nil && !errors.Is(rerr, io.EOF) && !errors.Is(rerr, io.ErrUnexpectedEOF) {
			return abort(rerr)
		}
		if k > 0 || n == 1 {
			out, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
				Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId,
				PartNumber: aws.Int32(n), Body: bytes.NewReader(buf[:k]), ContentLength: aws.Int64(int64(k)),
			})
			if err != nil {
				return abort(err)
			}
			parts = append(parts, types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(n)})
		}
		if rerr != nil {
			break
		}
	}
	done, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return abort(err)
	}
	if done.VersionId == nil || *done.VersionId == "" {
		return "", fmt.Errorf("put stream %s: %w (is versioning on in %s?)", key, ErrNoVersionID, s.bucket)
	}
	return *done.VersionId, nil
}
