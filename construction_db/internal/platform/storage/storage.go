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

var ErrNotFound = errors.New("object not found")

// Store holds evidence objects. Keys are always chosen by the server.
type Store interface {
	// PresignPut returns a URL that accepts exactly the bytes whose SHA-256 is
	// sum, once: a second PUT to the key is refused.
	PresignPut(ctx context.Context, key string, sum []byte, ttl time.Duration) (*PresignedPut, error)
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	Open(ctx context.Context, key, versionID string) (io.ReadCloser, error)
}

// PresignedPut is what a client needs to upload. Headers are signed: the
// client must send every one of them unchanged.
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
