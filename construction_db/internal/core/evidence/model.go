package evidence

import (
	"errors"
	"time"
)

const (
	StatusPending  = "pending"
	StatusVerified = "verified"
	StatusRejected = "rejected"

	MaxSizeBytes = 10 << 20
	MaxImageEdge = 2048 // Rev 4 §2.1: compressed to a 2048px long edge
	uploadTTL    = 15 * time.Minute

	// Why a stored file was rejected (evidence.rejected_reason).
	ReasonSizeMismatch       = "size_mismatch"
	ReasonHashMismatch       = "hash_mismatch"
	ReasonNotAnImage         = "not_an_image"
	ReasonWrongType          = "wrong_type"
	ReasonTooLargeDimensions = "too_large_dimensions"
)

var (
	ErrEvidenceNotFound = errors.New("evidence not found")
	ErrIDReused         = errors.New("evidence id already used with different details")
	ErrRejected         = errors.New("evidence was rejected; declare a new evidence id")
	ErrBadEvidenceID    = errors.New("evidence id must be a UUID")
	ErrBadHash          = errors.New("content_sha256 must be 64 hex characters")
	ErrLocationPair     = errors.New("lat and lng must both be set or both be absent")
	ErrBadDeviceID      = errors.New("device_id must be 1-200 bytes")
	ErrBadStatus        = errors.New("status must be pending, verified or rejected")
	ErrBadLimit         = errors.New("limit must be 1-200")
	ErrNotUploaded      = errors.New("file not uploaded yet")
)

// Why a chain check failed (ChainBreak.Reason), in the order they're checked.
const (
	BreakSeqGap            = "seq_gap"
	BreakPrevMismatch      = "prev_mismatch"
	BreakMetadataChanged   = "metadata_changed"
	BreakChainHashMismatch = "chain_hash_mismatch"
	// A chain entry whose evidence row isn't verified any more. The freeze
	// trigger stops that, so it means someone switched the trigger off.
	BreakEntryWithoutVerifiedEvidence = "entry_without_verified_evidence"
	BreakHeadMismatch                 = "head_mismatch"
	BreakVerifiedWithoutEntry         = "verified_without_entry"
)

// ChainVerification is the result of walking a site's chain as stored.
// Intact means no break was found; it never means "tamper-proof". HeadSeq
// and HeadHash are what to record outside the database: only an external
// record of them shows that entries were removed from the end (CHAIN.md).
type ChainVerification struct {
	SiteID     string      `json:"site_id"`
	Intact     bool        `json:"intact"`
	Length     int64       `json:"length"`
	HeadSeq    int64       `json:"head_seq"`
	HeadHash   string      `json:"head_hash"`
	FirstBreak *ChainBreak `json:"first_break"`
	CheckedAt  time.Time   `json:"checked_at"`
}

type ChainBreak struct {
	Seq        *int64 `json:"seq"` // null for verified_without_entry
	EvidenceID string `json:"evidence_id"`
	Reason     string `json:"reason"`
}

// RejectedError is a stored file that failed a check. The evidence is now
// rejected for good; its id can't be used again.
type RejectedError struct {
	Reason  string // one of the Reason constants
	Message string
}

func (e *RejectedError) Error() string { return e.Message + "; declare a new evidence id" }

var rejections = map[string]string{
	ReasonSizeMismatch:       "uploaded file is not size_bytes long",
	ReasonHashMismatch:       "uploaded file does not match content_sha256",
	ReasonNotAnImage:         "uploaded file is not a JPEG or PNG",
	ReasonWrongType:          "uploaded file is not the declared content_type",
	ReasonTooLargeDimensions: "image is larger than 2048px on its long edge",
}

func rejected(reason string) *RejectedError {
	return &RejectedError{Reason: reason, Message: rejections[reason]}
}

type Evidence struct {
	ID               string      `json:"id"`
	SiteID           string      `json:"site_id"`
	Status           string      `json:"status"`
	RejectedReason   *string     `json:"rejected_reason"`
	ContentSHA256    string      `json:"content_sha256"` // lowercase hex
	SizeBytes        int64       `json:"size_bytes"`
	ContentType      string      `json:"content_type"`
	ClientCapturedAt time.Time   `json:"client_captured_at"`
	Lat              *float64    `json:"lat"`
	Lng              *float64    `json:"lng"`
	DeviceID         *string     `json:"device_id"`
	UploadedBy       string      `json:"uploaded_by"`
	CreatedAt        time.Time   `json:"created_at"`
	Chain            *ChainEntry `json:"chain"`            // null until verified
	Upload           *Upload     `json:"upload,omitempty"` // only when the bytes are still needed

	ObjectKey string `json:"-"`
}

// ChainEntry is the evidence's place in its site's chain. Hashes are lowercase hex.
type ChainEntry struct {
	Seq              int64     `json:"seq"`
	ServerReceivedAt time.Time `json:"server_received_at"`
	PrevChainHash    string    `json:"prev_chain_hash"`
	MetadataHash     string    `json:"metadata_hash"`
	ChainHash        string    `json:"chain_hash"`
}

// Upload tells the client where to PUT the bytes. Every header must be sent as
// given, and the body must be exactly size_bytes: the URL signs the length.
type Upload struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type DeclareRequest struct {
	ContentSHA256    string    `json:"content_sha256" binding:"required"`
	SizeBytes        int64     `json:"size_bytes" binding:"required,gt=0,lte=10485760"`
	ContentType      string    `json:"content_type" binding:"required,oneof=image/jpeg image/png"`
	ClientCapturedAt time.Time `json:"client_captured_at" binding:"required"`
	Lat              *float64  `json:"lat" binding:"omitempty,gte=-90,lte=90"`
	Lng              *float64  `json:"lng" binding:"omitempty,gte=-180,lte=180"`
	DeviceID         *string   `json:"device_id"`
}
