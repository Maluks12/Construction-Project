package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	LockGovernance = "GOVERNANCE"
	LockCompliance = "COMPLIANCE"
)

var (
	ErrNotFound      = errors.New("object not found")
	ErrNoVersionID   = errors.New("a version id is required")
	ErrNoContentType = errors.New("a content type is required")
)

// Store holds evidence objects. Keys are always chosen by the server.
//
// Only internal/core/evidence may reach the bytes: ReadVersion in complete.go
// (hashing; serves nothing) and PresignGet in access.go (after logging the
// view). internal/archtest enforces it; a new method here fails that test
// until it is added there deliberately.
type Store interface {
	// PresignPut returns a URL that accepts exactly size bytes whose SHA-256 is
	// sum, sent as contentType, once: a second PUT to the key is refused.
	PresignPut(ctx context.Context, key string, sum []byte, size int64, contentType string, ttl time.Duration) (*PresignedPut, error)
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	// ReadVersion streams exactly one version of an object, for the server's
	// own checks.
	ReadVersion(ctx context.Context, key, versionID string) (io.ReadCloser, error)
	// PresignGet returns a URL that serves exactly one version of an object,
	// with contentType as the response's Content-Type whatever the object was
	// stored with.
	PresignGet(ctx context.Context, key, versionID, contentType string, ttl time.Duration) (*PresignedGet, error)
}

// PresignedGet is a URL anyone holding it can fetch until ExpiresAt; no
// headers are needed.
type PresignedGet struct {
	Method    string
	URL       string
	ExpiresAt time.Time
}

// PresignedPut is what a client needs to upload. Headers are signed: the
// client must send every one of them unchanged. Content-Length is signed too
// but not listed, because HTTP clients set it from the body.
type PresignedPut struct {
	Method    string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectInfo struct {
	Size        int64
	VersionID   string
	LockMode    string // "" if the object has no retention
	RetainUntil *time.Time
}

func ValidLockMode(mode string) bool {
	return mode == LockGovernance || mode == LockCompliance
}
