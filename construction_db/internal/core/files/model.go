// Package files is document uploads (M11 D4): certificates, appointment
// letters, H&S file and vault documents. The same seal as evidence without
// the chain: the device declares the hash, size and type, uploads to S3 with
// a URL that signs them, and Complete re-hashes the exact stored version.
// Records attach a verified file of their own purpose; every view of the
// bytes is logged first (access.go). Migration 000024 enforces the rules.
package files

import (
	"errors"
	"time"

	"construction_db/internal/core/evidence"
)

// Purposes: what a file may be attached to.
const (
	PurposeCompetency   = "competency"
	PurposeMedical      = "medical"
	PurposeAppointment  = "appointment"
	PurposeSiteDocument = "site_document"
	PurposeOrgDocument  = "org_document"
)

var Purposes = []string{PurposeCompetency, PurposeMedical, PurposeAppointment, PurposeSiteDocument, PurposeOrgDocument}

// SitePurposes need a site_id; the others belong to the organisation.
func SitePurpose(p string) bool { return p == PurposeAppointment || p == PurposeSiteDocument }

const (
	StatusPending  = "pending"
	StatusVerified = "verified"
	StatusRejected = "rejected"
)

var ContentTypes = []string{"application/pdf", "image/jpeg", "image/png"}

const (
	MaxSizeBytes = 20 << 20
	uploadTTL    = 15 * time.Minute
	viewTTL      = 60 * time.Second

	ReasonSizeMismatch = "size_mismatch"
	ReasonHashMismatch = "hash_mismatch"
	ReasonWrongType    = "wrong_type"
)

var RejectedReasons = []string{ReasonSizeMismatch, ReasonHashMismatch, ReasonWrongType}

// DeclareRequest declares a file under an id the device chose.
type DeclareRequest struct {
	Purpose       string  `json:"purpose"`
	SiteID        *string `json:"site_id"`
	ContentSHA256 string  `json:"content_sha256"`
	SizeBytes     int64   `json:"size_bytes"`
	ContentType   string  `json:"content_type"`
	FileName      *string `json:"file_name"`
	DeviceID      *string `json:"device_id"`
}

type File struct {
	ID             string     `json:"id"`
	Purpose        string     `json:"purpose"`
	SiteID         *string    `json:"site_id"`
	UploadedBy     string     `json:"uploaded_by"`
	ContentSHA256  string     `json:"content_sha256"`
	SizeBytes      int64      `json:"size_bytes"`
	ContentType    string     `json:"content_type"`
	FileName       *string    `json:"file_name"`
	DeviceID       *string    `json:"device_id"`
	Status         string     `json:"status"`
	RejectedReason *string    `json:"rejected_reason"`
	RetainUntil    *time.Time `json:"retain_until"`
	CreatedAt      time.Time  `json:"created_at"`
	// Present while pending: where to PUT the bytes.
	Upload *evidence.Upload `json:"upload,omitempty"`

	objectKey     string
	objectVersion *string
}

// Rejected is a failed Complete: the file is rejected for good.
type Rejected struct{ Reason string }

func (r *Rejected) Error() string { return "stored file failed the checks; declare a new file id" }

var (
	ErrFileNotFound   = errors.New("file not found")
	ErrBadFileID      = errors.New("file id must be a UUID")
	ErrBadPurpose     = errors.New("purpose must be competency, medical, appointment, site_document or org_document")
	ErrSiteRequired   = errors.New("site_id is required for appointment and site_document files, and only for them")
	ErrBadHash        = errors.New("content_sha256 must be 64 hex characters")
	ErrBadSize        = errors.New("size_bytes must be 1-20971520")
	ErrBadType        = errors.New("content_type must be application/pdf, image/jpeg or image/png")
	ErrBadFileName    = errors.New("file_name must be 1-200 characters")
	ErrBadDeviceID    = errors.New("device_id must be 1-200 bytes")
	ErrCantUpload     = errors.New("only an owner or SHE officer can upload this kind of file")
	ErrIDReused       = errors.New("file id already used with different details")
	ErrRejected       = errors.New("file was rejected; declare a new file id")
	ErrNotUploaded    = errors.New("file not uploaded yet")
	ErrNotVerified    = errors.New("file not verified yet")
	ErrWrongPurpose   = errors.New("file was uploaded for another purpose")
	ErrCantViewHealth = errors.New("only an owner or SHE officer can open a medical certificate")
)
