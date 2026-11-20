// Package exports is the audit pack (M12): one site over a date range, as of
// the moment the export was queued, built by cmd/worker into a
// deterministic ZIP whose SHA-256 the export records.
package exports

import (
	"errors"
	"time"
)

// PackFormat is the pack's layout version. A change to what the pack holds
// or how it is written gets the next number: a rebuild gives the recorded
// SHA-256 only in the same format (D10).
const PackFormat = 1

const (
	// MaxDays is the longest range: 366 local dates.
	MaxDays = 366
	// Settle: a build starts at least this long after as_of, so every
	// transaction stamped before as_of has committed (D10). The database
	// enforces it (audit_export_settle, 000039).
	Settle = 2 * time.Minute
	// Lease: how long a claim lasts unless the builder extends it.
	Lease = 2 * time.Minute
	// MaxAttempts: after this many failed or lapsed attempts the export is
	// failed for good.
	MaxAttempts = 3
	// Keep: how long a finished pack can be downloaded (D7); the exports
	// bucket's lifecycle rule deletes it after that.
	Keep = 7 * 24 * time.Hour
	// DownloadTTL: how long a download link works.
	DownloadTTL = 5 * time.Minute
	// ContentType of a pack.
	ContentType = "application/zip"
)

// Export statuses.
const (
	StatusQueued   = "queued"
	StatusBuilding = "building"
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusExpired  = "expired"
)

var Statuses = []string{StatusQueued, StatusBuilding, StatusReady, StatusFailed, StatusExpired}

// Request queues an export (PUT, under an id the device chose).
type Request struct {
	From              string    `json:"from"`
	To                string    `json:"to"`
	ClientRequestedAt time.Time `json:"client_requested_at"`
	DeviceID          *string   `json:"device_id"`
}

// Export is the append-only record of one export and where it stands.
type Export struct {
	ID                string    `json:"id"`
	SiteID            string    `json:"site_id"`
	From              string    `json:"from"`
	To                string    `json:"to"`
	TimeZone          string    `json:"time_zone"`
	AsOf              time.Time `json:"as_of"`
	RequestedBy       string    `json:"requested_by"`
	RequesterRole     string    `json:"requester_role"`
	IncludesHealth    bool      `json:"includes_health"`
	PackFormat        int       `json:"pack_format"`
	RegimeCode        string    `json:"regime_code"`
	RegimeVersion     string    `json:"regime_version"`
	RegimeDataSHA256  string    `json:"regime_data_sha256"`
	ClientRequestedAt time.Time `json:"client_requested_at"`
	DeviceID          *string   `json:"device_id"`

	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	FailedAttempts int    `json:"failed_attempts"`
	// A stored original no longer matched its seal: failed for good, and
	// owners and SHE officers were alerted.
	IntegrityAlarm bool    `json:"integrity_alarm"`
	LastError      *string `json:"last_error"`
	// Set once the pack is built.
	SHA256        *string    `json:"sha256"`
	SizeBytes     *int64     `json:"size_bytes"`
	EvidenceCount *int       `json:"evidence_count"`
	FileCount     *int       `json:"file_count"`
	ChainLength   *int64     `json:"chain_length"`
	ChainHead     *string    `json:"chain_head"`
	ChainIntact   *bool      `json:"chain_intact"`
	FinishedAt    *time.Time `json:"finished_at"`
	ExpiresAt     *time.Time `json:"expires_at"`

	objectKey, objectVersion *string
}

// Download is a short-lived link to a ready pack.
type Download struct {
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
}

var (
	ErrSiteNotFound   = errors.New("site not found")
	ErrNotFound       = errors.New("export not found")
	ErrBadID          = errors.New("export id must be a UUID")
	ErrBadRange       = errors.New("from and to must be YYYY-MM-DD, to not before from, at most 366 days")
	ErrBadRequestedAt = errors.New("client_requested_at is required, and not in the future")
	ErrBadDeviceID    = errors.New("device_id must be 1-200 bytes")
	ErrIDReused       = errors.New("export id already used with different details")
	ErrNotReady       = errors.New("export not ready yet")
	ErrFailed         = errors.New("export failed; request a new export")
	ErrExpired        = errors.New("export expired; request a new export")

	errBody = errors.New("invalid request body")
)

// Sentinels are the errors the handlers send; TestAPIDoc checks each is quoted.
var Sentinels = []error{ErrSiteNotFound, ErrNotFound, ErrBadID, ErrBadRange, ErrBadRequestedAt, ErrBadDeviceID, ErrIDReused,
	ErrNotReady, ErrFailed, ErrExpired}
