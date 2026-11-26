// Package workers is the organisation's worker register with competencies
// and medical fitness (M11 D5–D10), the links from toolbox signatures to
// workers (D7), and the regime type lists those records use (D1). Endpoints
// are in docs/api/workers.md; migration 000025 enforces the rules.
package workers

import (
	"errors"
	"time"

	"construction_db/internal/regimes"
)

// Record states, from the dates and the record's place in its renewals.
const (
	StateValid      = "valid"
	StateExpiring   = "expiring" // within the regime's first reminder lead time
	StateExpired    = "expired"
	StateSuperseded = "superseded"
	StateRetracted  = "retracted"
)

var States = []string{StateValid, StateExpiring, StateExpired, StateSuperseded, StateRetracted}

const (
	OutcomeFit                 = "fit"
	OutcomeFitWithRestrictions = "fit_with_restrictions"
	OutcomeUnfit               = "unfit"
)

var Outcomes = []string{OutcomeFit, OutcomeFitWithRestrictions, OutcomeUnfit}

// Warnings a sign-in carries (D18); a sign-in is never refused for them.
const (
	WarningNoValidMedical    = "no_valid_medical"
	WarningMedicalUnfit      = "medical_unfit"
	WarningCompetencyExpired = "competency_expired"
)

var Warnings = []string{WarningNoValidMedical, WarningMedicalUnfit, WarningCompetencyExpired}

// WorkerRequest creates a worker (owner, SHE officer, supervisor) or edits
// one (owner, SHE officer). The same validation runs for ONBOARD's CSV rows.
type WorkerRequest struct {
	Name        string  `json:"name"`
	Firm        *string `json:"firm"`
	ClockNumber *string `json:"clock_number"`
	Trade       *string `json:"trade"`
	Active      *bool   `json:"active"`
	// The worker's updated_at as the editor last read it (created_at if it
	// was never edited). An edit of a worker changed since is 409 with the
	// current worker (SYNC conflict rule). Optional: without it, the last
	// edit wins.
	BaseUpdatedAt *time.Time `json:"base_updated_at"`
}

// Changed is the 409 for an edit based on an older copy.
type Changed struct {
	Current *Worker
}

func (e *Changed) Error() string { return ErrChanged.Error() }
func (e *Changed) Unwrap() error { return ErrChanged }

type Worker struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Firm        *string    `json:"firm"`
	ClockNumber *string    `json:"clock_number"`
	Trade       *string    `json:"trade"`
	Active      bool       `json:"active"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedBy   *string    `json:"updated_by"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

type WorkerDetail struct {
	Worker
	Competencies []Competency `json:"competencies"`
	Medicals     []Medical    `json:"medicals"`
}

type CompetencyRequest struct {
	RegimeCode       string    `json:"regime_code"`
	TypeKey          string    `json:"type_key"`
	Number           *string   `json:"number"`
	Issuer           *string   `json:"issuer"`
	IssuedOn         string    `json:"issued_on"`
	ExpiresOn        *string   `json:"expires_on"`
	FileID           string    `json:"file_id"`
	SupersedesID     *string   `json:"supersedes_id"`
	ClientRecordedAt time.Time `json:"client_recorded_at"`
	DeviceID         *string   `json:"device_id"`
}

type MedicalRequest struct {
	RegimeCode       string    `json:"regime_code"`
	Outcome          string    `json:"outcome"`
	ExaminedOn       string    `json:"examined_on"`
	ExpiresOn        string    `json:"expires_on"`
	PracticeNumber   *string   `json:"practice_number"`
	FileID           string    `json:"file_id"`
	SupersedesID     *string   `json:"supersedes_id"`
	ClientRecordedAt time.Time `json:"client_recorded_at"`
	DeviceID         *string   `json:"device_id"`
}

type RetractRequest struct {
	Reason string `json:"reason"`
}

type LinkRequest struct {
	WorkerID string `json:"worker_id"`
}

type UnlinkRequest struct {
	Reason string `json:"reason"`
}

// RecordHeader is what a competency and a medical share.
type RecordHeader struct {
	ID               string      `json:"id"`
	WorkerID         string      `json:"worker_id"`
	RegimeCode       string      `json:"regime_code"`
	FileID           string      `json:"file_id"`
	SupersedesID     *string     `json:"supersedes_id"`
	SupersededBy     *string     `json:"superseded_by"`
	RecordedBy       string      `json:"recorded_by"`
	ClientRecordedAt time.Time   `json:"client_recorded_at"`
	RecordedAt       time.Time   `json:"recorded_at"`
	DeviceID         *string     `json:"device_id"`
	Retraction       *Retraction `json:"retraction"`
	State            string      `json:"state"`
}

type Competency struct {
	RecordHeader
	TypeKey   string  `json:"type_key"`
	Number    *string `json:"number"`
	Issuer    *string `json:"issuer"`
	IssuedOn  string  `json:"issued_on"`
	ExpiresOn *string `json:"expires_on"`
}

// Medical holds the outcome and dates only (D6). Its certificate opens for an
// owner or SHE officer, logged.
type Medical struct {
	RecordHeader
	Outcome        string  `json:"outcome"`
	ExaminedOn     string  `json:"examined_on"`
	ExpiresOn      string  `json:"expires_on"`
	PracticeNumber *string `json:"practice_number"`
}

type Retraction struct {
	Reason      string    `json:"reason"`
	RetractedBy string    `json:"retracted_by"`
	RetractedAt time.Time `json:"retracted_at"`
}

type Link struct {
	ID           string     `json:"id"`
	AttendanceID string     `json:"attendance_id"`
	WorkerID     string     `json:"worker_id"`
	Method       string     `json:"method"`
	LinkedBy     string     `json:"linked_by"`
	LinkedAt     time.Time  `json:"linked_at"`
	UnlinkedBy   *string    `json:"unlinked_by"`
	UnlinkedAt   *time.Time `json:"unlinked_at"`
	UnlinkReason *string    `json:"unlink_reason"`
}

// RegimeSummary is one loaded regime, for picking types (D1).
type RegimeSummary struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	Edition      string `json:"edition"`
	Jurisdiction string `json:"jurisdiction"`
}

// RegimeTypes is what a regime offers records typed against it.
type RegimeTypes struct {
	RegimeCode       string                    `json:"regime_code"`
	CompetencyTypes  []regimes.CompetencyType  `json:"competency_types"`
	AppointmentTypes []regimes.AppointmentType `json:"appointment_types"`
	Medical          regimes.MedicalRule       `json:"medical"`
	DocumentTypes    []regimes.DocumentType    `json:"document_types"`
	Reminders        regimes.Reminders         `json:"reminders"`
}

// FieldProblem is one invalid field, for the API and ONBOARD's CSV rows.
type FieldProblem struct {
	Field string `json:"field"`
	Error string `json:"error"`
}

// Problem is a 400 that names its field.
type Problem struct{ FieldProblem }

func (p *Problem) Error() string { return p.FieldProblem.Error }

// Field validation texts (FieldProblem.Error).
const (
	MsgName       = "name must be 1-200 characters"
	MsgFirm       = "firm must be 1-200 characters"
	MsgClock      = "clock_number must be 1-50 characters"
	MsgTrade      = "trade must be 1-100 characters"
	MsgRegime     = "regime_code is not a loaded regime"
	MsgType       = "type_key is not a competency type of that regime"
	MsgNumber     = "number must be 1-100 characters"
	MsgIssuer     = "issuer must be 1-200 characters"
	MsgIssuedOn   = "issued_on must be YYYY-MM-DD"
	MsgExpiresOn  = "expires_on must be YYYY-MM-DD, not before the issue or examination date"
	MsgExpiryNeed = "expires_on is required for this type"
	MsgExpiryNone = "this type has no expiry; leave expires_on out"
	MsgOutcome    = "outcome must be fit, fit_with_restrictions or unfit"
	MsgExaminedOn = "examined_on must be YYYY-MM-DD"
	MsgPractice   = "practice_number must be 1-30 letters, digits, spaces, dots, slashes or dashes"
	MsgRecordedAt = "client_recorded_at is required"
	MsgFuture     = "client_recorded_at is in the future; check the device clock"
	MsgDevice     = "device_id must be 1-200 bytes"
	MsgFileID     = "file_id must be a UUID"
	MsgSupersedes = "supersedes_id must be a UUID"
)

var Messages = []string{MsgName, MsgFirm, MsgClock, MsgTrade, MsgRegime, MsgType, MsgNumber, MsgIssuer, MsgIssuedOn,
	MsgExpiresOn, MsgExpiryNeed, MsgExpiryNone, MsgOutcome, MsgExaminedOn, MsgPractice, MsgRecordedAt, MsgFuture, MsgDevice,
	MsgFileID, MsgSupersedes}

// IdentityClash is the 409 for a worker whose name, firm and clock number
// are already on the register.
type IdentityClash struct {
	Error    string `json:"error"`
	WorkerID string `json:"worker_id"`
}

var (
	ErrWorkerNotFound     = errors.New("worker not found")
	ErrRecordNotFound     = errors.New("record not found")
	ErrRegimeNotFound     = errors.New("regime not found")
	ErrAttendanceNotFound = errors.New("attendance not found")
	ErrBadWorkerID        = errors.New("worker id must be a UUID")
	ErrBadRecordID        = errors.New("record id must be a UUID")
	ErrBadActive          = errors.New("active filter must be true or false")
	ErrBadLimit           = errors.New("limit must be 1-500")
	ErrBadReason          = errors.New("reason must be 1-500 characters")
	ErrCantEdit           = errors.New("only an owner or SHE officer can edit a worker")
	ErrChanged            = errors.New("worker changed since base_updated_at; get it and edit again")
	ErrCantRetract        = errors.New("only an owner or SHE officer can retract a record")
	ErrCantLink           = errors.New("only an owner or SHE officer can link or unlink a signature")
	ErrIdentityTaken      = errors.New("a worker with this name, firm and clock number is already on the register")
	ErrIDReused           = errors.New("id already used with different details")
	ErrSupersedeTarget    = errors.New("supersedes_id must be a live record of the same worker (and type)")
	ErrAlreadyRenewed     = errors.New("that record already has a renewal")
	ErrAlreadyRetracted   = errors.New("record already retracted with another reason")
	ErrAlreadyLinked      = errors.New("signature already linked to a worker; unlink it first")
	ErrNotLinked          = errors.New("signature is not linked to a worker")
)
