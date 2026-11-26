package devicesync

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrBadCursor       = errors.New("invalid cursor")
	ErrBadLimit        = errors.New("limit must be 1-500")
	ErrBadBatch        = errors.New("invalid batch")
	ErrBatchTooLarge   = errors.New("batch too large; at most 1 MiB")
	ErrBatchSize       = errors.New("records must hold 1-100 records")
	ErrBadMethod       = errors.New("method must be PUT or POST")
	ErrBadRef          = errors.New("ref must be at most 200 bytes")
	ErrRouteNotAllowed = errors.New("route not allowed in a batch")
)

// Sentinels are the errors the sync endpoints send (TestAPIDoc).
var Sentinels = []error{ErrBadCursor, ErrBadLimit, ErrBadBatch, ErrBatchTooLarge, ErrBatchSize, ErrBadMethod, ErrBadRef, ErrRouteNotAllowed}

// Item is one pulled aggregate: Data is exactly the body of its GET for
// the caller's role.
type Item struct {
	Kind string          `json:"kind"`
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

// Pull is one page of a site's (or the organisation's) changes.
type Pull struct {
	ServerTime time.Time `json:"server_time"`
	Reset      bool      `json:"reset"`
	HasMore    bool      `json:"has_more"`
	Cursor     string    `json:"cursor"`
	Items      []Item    `json:"items"`
}

// OrgPull adds what is always sent whole: the caller and every site they
// can see now. A site missing from Sites is one the phone must drop (D5).
type OrgPull struct {
	Pull
	Me    json.RawMessage `json:"me"`
	Sites []SiteEntry     `json:"sites"`
}

type SiteEntry struct {
	Site            json.RawMessage `json:"site"`
	RegimeCode      string          `json:"regime_code"`
	ReferenceSHA256 string          `json:"reference_sha256"`
}

// Reference is a site's regime data, fetched once per sha256.
type Reference struct {
	SiteID     string                     `json:"site_id"`
	RegimeCode string                     `json:"regime_code"`
	SHA256     string                     `json:"sha256"`
	Parts      map[string]json.RawMessage `json:"parts"`
}

type PushRequest struct {
	Records []PushRecord `json:"records"`
}

type PushRecord struct {
	Ref    *string         `json:"ref"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

// PushResult is the single endpoint's own answer for one record.
type PushResult struct {
	Ref        *string         `json:"ref"`
	Status     int             `json:"status"`
	Body       json.RawMessage `json:"body"`
	RetryAfter *int            `json:"retry_after,omitempty"`
}

// PushResponse: records[Processed:] were not attempted; send them again.
type PushResponse struct {
	ServerTime time.Time    `json:"server_time"`
	Processed  int          `json:"processed"`
	Results    []PushResult `json:"results"`
}

// BatchProblem is a 400 for the batch itself; nothing in it ran.
type BatchProblem struct {
	Error string `json:"error"`
	Index *int   `json:"index"`
}

const (
	MaxPushRecords = 100
	MaxPushBytes   = 1 << 20
	MaxPullItems   = 500
	DefaultPull    = 200
)
