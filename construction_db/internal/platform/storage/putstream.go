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

// Multipart limits: memory stays at one part. S3 takes at most 10,000
// parts of at most 5 GiB, in an object of at most 5 TiB.
const (
	PartSize      = 8 << 20
	MaxParts      = 10_000
	MaxPartSize   = 5 << 30
	MaxObjectSize = 5 << 40
)

// ErrTooLarge: no part size fits the object in S3's limits.
var ErrTooLarge = errors.New("too large for one object")

// PartSizeFor is the smallest part size, in whole MiB and at least
// PartSize, that fits size bytes in MaxParts parts.
func PartSizeFor(size int64) (int64, error) {
	if size > MaxObjectSize {
		return 0, fmt.Errorf("%d bytes: %w (the most is %d)", size, ErrTooLarge, int64(MaxObjectSize))
	}
	part := (size + MaxParts - 1) / MaxParts
	part = (part + 1<<20 - 1) / (1 << 20) * (1 << 20)
	return min(max(part, PartSize), MaxPartSize), nil
}

// PutStream returns the stored version, so a download can pin it; a failure
// aborts the upload, leaving no object. size is an upper bound on the
// stream's length: it sets the part size.
func (s *S3) PutStream(ctx context.Context, key string, r io.Reader, contentType string, size int64) (string, error) {
	if contentType == "" {
		return "", fmt.Errorf("put stream %s: %w", key, ErrNoContentType)
	}
	partSize, err := PartSizeFor(size)
	if err != nil {
		return "", fmt.Errorf("put stream %s: %w", key, err)
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

	buf := make([]byte, partSize)
	var parts []types.CompletedPart
	for n := int32(1); ; n++ {
		if n > MaxParts {
			return abort(fmt.Errorf("more than %d parts: the stream outgrew its size, %d bytes", MaxParts, size))
		}
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
