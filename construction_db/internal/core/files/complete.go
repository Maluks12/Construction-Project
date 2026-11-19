package files

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoders image.DecodeConfig uses
	_ "image/png"
	"io"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/storage"
)

// Complete checks the bytes S3 stored for a pending file and seals it
// (verified) if they are what was declared, or rejects it for good. Only the
// uploader completes. created is false when it was already verified.
func (s *Service) Complete(ctx context.Context, orgID, userID, role, fileID string) (*File, bool, error) {
	var f *File
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var err error
		f, err = GetFile(ctx, tx, orgID, fileID)
		if isNotFound(err) || (err == nil && f.UploadedBy != userID) {
			return ErrFileNotFound
		}
		if err == nil && f.Status == StatusRejected {
			return ErrRejected
		}
		return err
	})
	if isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("complete file: %w", err)
	}
	if f.Status == StatusVerified {
		return f, false, nil
	}

	info, err := s.store.Stat(ctx, f.objectKey)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false, ErrNotUploaded
	}
	if err != nil {
		return nil, false, fmt.Errorf("complete file: %w", err)
	}
	if info.LockMode == "" || info.RetainUntil == nil {
		return nil, false, fmt.Errorf("complete file: object %s has no Object Lock retention; check the bucket's default retention", f.objectKey)
	}
	// Exactly the version Stat saw, so what is checked is what is sealed.
	r, err := s.store.ReadVersion(ctx, f.objectKey, info.VersionID)
	if err != nil {
		return nil, false, fmt.Errorf("complete file: %w", err)
	}
	reason, err := inspect(r, f)
	r.Close()
	if err != nil {
		return nil, false, fmt.Errorf("complete file: %w", err)
	}

	var out *File
	created := false
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var changed bool
		var err error
		if reason != "" {
			changed, err = markRejected(ctx, tx, orgID, f.ID, reason)
		} else {
			changed, err = markVerified(ctx, tx, orgID, f.ID, info.VersionID, *info.RetainUntil)
		}
		if err != nil {
			return err
		}
		created = changed && reason == ""
		out, err = GetFile(ctx, tx, orgID, f.ID)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("complete file: %w", err)
	}
	// Another complete may have finished it first; its outcome is the answer.
	switch out.Status {
	case StatusVerified:
		return out, created, nil
	case StatusRejected:
		return nil, false, &Rejected{Reason: *out.RejectedReason}
	}
	return nil, false, fmt.Errorf("complete file: %s is still %s", f.ID, out.Status)
}

var pdfMagic = []byte("%PDF-")

// inspect reads the stored file once, in constant memory: every byte passes
// through the hash and the counter exactly once. It returns why the file is
// rejected, or "" if it passes. Checks run size, hash, then type.
func inspect(r io.Reader, f *File) (string, error) {
	sum := sha256.New()
	var size countWriter
	src := &readErrReader{r: io.LimitReader(r, MaxSizeBytes+1)}
	br := bufio.NewReader(io.TeeReader(src, io.MultiWriter(sum, &size)))

	typeOK := false
	switch f.ContentType {
	case "application/pdf":
		head, _ := br.Peek(len(pdfMagic))
		typeOK = bytes.Equal(head, pdfMagic)
	default:
		_, format, err := image.DecodeConfig(br)
		typeOK = err == nil && "image/"+format == f.ContentType
	}
	if _, err := io.Copy(io.Discard, br); err != nil {
		return "", fmt.Errorf("read stored file: %w", err)
	}
	if src.err != nil {
		return "", fmt.Errorf("read stored file: %w", src.err)
	}
	switch {
	case int64(size) != f.SizeBytes:
		return ReasonSizeMismatch, nil
	case hex.EncodeToString(sum.Sum(nil)) != f.ContentSHA256:
		return ReasonHashMismatch, nil
	case !typeOK:
		return ReasonWrongType, nil
	}
	return "", nil
}

type countWriter int64

func (c *countWriter) Write(p []byte) (int, error) {
	*c += countWriter(len(p))
	return len(p), nil
}

// readErrReader remembers the first read error other than EOF, which a
// decoder reading through it might otherwise swallow.
type readErrReader struct {
	r   io.Reader
	err error
}

func (e *readErrReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}
