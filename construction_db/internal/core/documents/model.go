// Package documents is versioned documents (M11 D19, D20): the
// organisation's vault (core, internal/core/vault mounts it) and each site's
// H&S file (the Construction edition's safetyfile mounts it). Types come
// from the regime: the record's own for the vault, the site's for the H&S
// file. Every version is a sealed file (internal/core/files); nothing is
// overwritten. Migration 000027 enforces the rules.
package documents

import (
	"errors"
	"time"
)

const (
	ScopeOrg  = "org"
	ScopeSite = "site"
)

// Version and readiness states.
const (
	StateValid    = "valid"
	StateExpiring = "expiring" // within the first reminder lead time
	StateExpired  = "expired"

	StatusPresent = "present"
	StatusMissing = "missing"
	StatusExpired = "expired"
)

var (
	VersionStates   = []string{StateValid, StateExpiring, StateExpired}
	ReadinessStates = []string{StatusPresent, StatusMissing, StatusExpired}
)

// CreateRequest creates a document with its first version, under an id the
// device chose. regime_code names the vault document's regime (D1); a
// site's H&S file takes the site's and leaves it out.
type CreateRequest struct {
	RegimeCode *string        `json:"regime_code"`
	TypeKey    string         `json:"type_key"`
	Title      string         `json:"title"`
	Version    VersionRequest `json:"version"`
}

// VersionRequest is one version: its file and what it says.
type VersionRequest struct {
	FileID           string    `json:"file_id"`
	Issuer           *string   `json:"issuer"`
	Reference        *string   `json:"reference"`
	IssuedOn         *string   `json:"issued_on"`
	ExpiresOn        *string   `json:"expires_on"`
	ClientUploadedAt time.Time `json:"client_uploaded_at"`
	DeviceID         *string   `json:"device_id"`
}

type DocumentSummary struct {
	ID            string    `json:"id"`
	Scope         string    `json:"scope"`
	SiteID        *string   `json:"site_id"`
	RegimeCode    string    `json:"regime_code"`
	TypeKey       string    `json:"type_key"`
	Title         string    `json:"title"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	LatestVersion int       `json:"latest_version"`
	Current       Version   `json:"current"`
}

type Document struct {
	DocumentSummary
	Versions []Version `json:"versions"`
}

type Version struct {
	Version          int       `json:"version"`
	FileID           string    `json:"file_id"`
	Issuer           *string   `json:"issuer"`
	Reference        *string   `json:"reference"`
	IssuedOn         *string   `json:"issued_on"`
	ExpiresOn        *string   `json:"expires_on"`
	UploadedBy       string    `json:"uploaded_by"`
	ClientUploadedAt time.Time `json:"client_uploaded_at"`
	ReceivedAt       time.Time `json:"received_at"`
	DeviceID         *string   `json:"device_id"`
	// The current version's state; earlier versions are "superseded".
	State string `json:"state"`
}

// StateSuperseded marks a version that a later one replaced.
const StateSuperseded = "superseded"

// Readiness is one type's place in the register: present (a current,
// unexpired version), expired, or missing (R4 "inspection readiness").
type Readiness struct {
	TypeKey     string   `json:"type_key"`
	Label       string   `json:"label"`
	Required    bool     `json:"required"`
	Status      string   `json:"status"`
	DocumentIDs []string `json:"document_ids"`
}

type Register struct {
	RegimeCode string            `json:"regime_code"`
	Documents  []DocumentSummary `json:"documents"`
	Readiness  []Readiness       `json:"readiness"`
}

var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrVersionNotFound  = errors.New("version not found")
	ErrRegimeNotFound   = errors.New("regime not found")
	ErrBadDocumentID    = errors.New("document id must be a UUID")
	ErrBadVersion       = errors.New("version must be a whole number from 2")
	ErrBadRegime        = errors.New("regime_code is required for a vault document and must be a loaded regime")
	ErrRegimeForSite    = errors.New("a safety file document takes its site's regime; leave regime_code out")
	ErrBadType          = errors.New("type_key is not a document type of this regime and scope")
	ErrBadTitle         = errors.New("title must be 1-200 characters")
	ErrBadIssuer        = errors.New("issuer must be 1-200 characters, and one of the type's issuers when it lists them")
	ErrBadReference     = errors.New("reference must be 1-200 characters")
	ErrBadDate          = errors.New("issued_on and expires_on must be YYYY-MM-DD, expiry not before issue")
	ErrExpiryNeeded     = errors.New("expires_on is required for this type")
	ErrExpiryNone       = errors.New("this type has no expiry; leave expires_on out")
	ErrNoUploadedAt     = errors.New("client_uploaded_at is required")
	ErrUploadedInFuture = errors.New("client_uploaded_at is in the future; check the device clock")
	ErrBadDeviceID      = errors.New("device_id must be 1-200 bytes")
	ErrBadFileID        = errors.New("file_id must be a UUID")
	ErrCantWrite        = errors.New("only an owner or SHE officer can add documents")
	ErrIDReused         = errors.New("document id already used with different details")
	ErrVersionTaken     = errors.New("that version already exists with different details")
	ErrNotNextVersion   = errors.New("version is not the next version of this document")
	ErrFileUsed         = errors.New("file already attached to another document version")
)
