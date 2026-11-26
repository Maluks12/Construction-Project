// Package checklists is the checklist engine (Rev 4 §2.4): templates come
// from the site's regime, never from code.
package checklists

import (
	"errors"
	"time"
)

// Finding severity is a fixed product scale, not regime data, so findings
// compare across sites and regimes (decided 9 Nov 2026). Each level has a
// definition the inspector sees when choosing; without one, two foremen use
// "high" and "critical" differently and the comparison means nothing.
// Changing a definition changes the meaning of past findings: fix wording
// only, and add a new level for a new meaning.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

type SeverityLevel struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Definition string `json:"definition"`
}

// Severities, least severe first. docs/api/inspections.md and
// construction_db/CLAUDE.md quote these word for word (TestSeverityDefinitions).
var Severities = []SeverityLevel{
	{SeverityLow, "Low", "A minor shortfall unlikely to cause injury (e.g. housekeeping or paperwork); fix at the next routine opportunity."},
	{SeverityMedium, "Medium", "Could cause an injury needing treatment, or breaks a regulation with no immediate danger; fix within days, not weeks."},
	{SeverityHigh, "High", "Could cause serious injury or a reportable incident, but not imminently; fix before the affected work continues on the next shift."},
	{SeverityCritical, "Critical", "Imminent danger of death or serious injury; stop the affected work now and keep it stopped until it is made safe."},
}

// Response types the engine can record. Rev 4 lists more (scale, numeric,
// count, text); none ships yet, and a test fails if a shipped template uses
// one before it is supported here.
const ResponseTypePassFailNA = "pass_fail_na"

var SupportedResponseTypes = []string{ResponseTypePassFailNA}

// ChecklistCatalog is what a site's regime offers: its checklist templates
// and the severity scale for findings.
type ChecklistCatalog struct {
	Regime     RegimeRef           `json:"regime"`
	Checklists []ChecklistTemplate `json:"checklists"`
	Severities []SeverityLevel     `json:"severities"`
}

type RegimeRef struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ChecklistTemplate is the API's copy of a regime template, so a change to the
// data files' shape can't silently change the API. SHA256 is the version's
// content hash (regimes.ChecklistTemplate.ContentHash): the phone keeps it with
// the wording it shows and sends it back when it starts an inspection.
type ChecklistTemplate struct {
	Key       string          `json:"key"`
	Version   int             `json:"version"`
	SHA256    string          `json:"sha256"`
	Title     string          `json:"title"`
	Cadence   string          `json:"cadence"`
	SourceRef string          `json:"source_ref"`
	Items     []ChecklistItem `json:"items"`
}

// Starting an inspection names the template version the phone showed and its
// hash; resolveTemplate checks both against the site's regime.
var (
	ErrUnknownTemplateVersion = errors.New("unknown checklist template version")
	ErrTemplateMismatch       = errors.New("checklist template does not match")
)

type ChecklistItem struct {
	Key          string `json:"key"`
	Prompt       string `json:"prompt"`
	ResponseType string `json:"response_type"`
	SourceRef    string `json:"source_ref"`
}

// An inspection is one filling-in of one checklist template version.
const (
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusAbandoned  = "abandoned"

	ResultPass = "pass"
	ResultFail = "fail"
	ResultNA   = "na"
)

var (
	ErrInspectionNotFound  = errors.New("inspection not found")
	ErrBadInspectionID     = errors.New("inspection id must be a UUID")
	ErrInspectionIDReused  = errors.New("inspection id already used with different details")
	ErrSiteArchived        = errors.New("site is archived")
	ErrBadCorrectsID       = errors.New("corrects_id must be a completed inspection on this site")
	ErrBadDeviceID         = errors.New("device_id must be 1-200 bytes")
	ErrAlreadyCorrected    = errors.New("inspection already has a correction")
	ErrCorrectionChecklist = errors.New("a correction must use the same checklist as the inspection it corrects")
	ErrBadStatus           = errors.New("status must be in_progress, completed or abandoned")
	ErrBadLimit            = errors.New("limit must be 1-200")
)

// StartInspectionRequest names the template version the phone showed, with
// the hash the catalogue gave for it.
type StartInspectionRequest struct {
	TemplateKey     string    `json:"template_key" binding:"required"`
	TemplateVersion int       `json:"template_version" binding:"required,gte=1"`
	TemplateSHA256  string    `json:"template_sha256" binding:"required"`
	ClientStartedAt time.Time `json:"client_started_at" binding:"required"`
	DeviceID        *string   `json:"device_id"`
	CorrectsID      *string   `json:"corrects_id"`
}

// InspectionHeader is what an inspection says about itself: the fields a
// full inspection and a list summary share. Embedded untagged, encoding/json
// flattens it into both.
type InspectionHeader struct {
	ID                string     `json:"id"`
	SiteID            string     `json:"site_id"`
	Status            string     `json:"status"`
	TemplateKey       string     `json:"template_key"`
	TemplateVersion   int        `json:"template_version"`
	TemplateSHA256    string     `json:"template_sha256"` // lowercase hex
	TemplateTitle     string     `json:"template_title"`
	TemplateSourceRef string     `json:"template_source_ref"`
	ItemCount         int        `json:"item_count"`
	StartedBy         string     `json:"started_by"`
	ClientStartedAt   time.Time  `json:"client_started_at"`
	StartedAt         time.Time  `json:"started_at"`
	DeviceID          *string    `json:"device_id"`
	CorrectsID        *string    `json:"corrects_id"`
	ClientCompletedAt *time.Time `json:"client_completed_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	AbandonedAt       *time.Time `json:"abandoned_at"`
	AbandonedBy       *string    `json:"abandoned_by"`
	AbandonReason     *string    `json:"abandon_reason"`
}

// Inspection carries its own copy of the template version's wording, so it
// reads the same whatever the regime data says later.
type Inspection struct {
	InspectionHeader
	Items []InspectionItem `json:"items"`
	// One per failed item, raised by the database when the inspection is
	// completed; [] until then.
	Findings []InspectionFinding `json:"findings"`
}

// InspectionSummary is one inspection in a list: no items, but how far it
// got and how many items failed.
type InspectionSummary struct {
	InspectionHeader
	AnsweredCount int `json:"answered_count"`
	FailedCount   int `json:"failed_count"`
}

// InspectionItem is one template item with its answer; the answer fields are
// null until the item is answered.
type InspectionItem struct {
	Key              string             `json:"key"`
	Position         int                `json:"position"`
	Prompt           string             `json:"prompt"`
	SourceRef        *string            `json:"source_ref"`
	ResponseType     string             `json:"response_type"`
	Result           *string            `json:"result"`
	Severity         *string            `json:"severity"`
	Comment          *string            `json:"comment"`
	ClientAnsweredAt *time.Time         `json:"client_answered_at"`
	AnsweredAt       *time.Time         `json:"answered_at"`
	Evidence         []AttachedEvidence `json:"evidence"`
}

// AttachedEvidence is a photo on an answer. It shows the photo's id and
// capture time, not the photo: seeing it goes through the logged
// POST …/evidence/:evidenceId/view. CapturedBeforeStart compares two device
// times (photo captured, inspection started), so a reviewer can spot an old
// photo reused.
type AttachedEvidence struct {
	EvidenceID          string    `json:"evidence_id"`
	ClientCapturedAt    time.Time `json:"client_captured_at"`
	CapturedBeforeStart bool      `json:"captured_before_start"`
	AttachedAt          time.Time `json:"attached_at"`
}

// AnswerRequest is an item's whole answer: sending it again replaces the
// result, severity, comment and photos together.
type AnswerRequest struct {
	Result           string    `json:"result" binding:"required,oneof=pass fail na"`
	Severity         *string   `json:"severity"`
	Comment          *string   `json:"comment"`
	EvidenceIDs      []string  `json:"evidence_ids"`
	ClientAnsweredAt time.Time `json:"client_answered_at" binding:"required"`
}

const (
	maxCommentChars  = 2000 // the column's CHECK
	maxPhotosPerItem = 20
	// How far ahead of the server a device clock may be. The newest answer
	// wins by client_answered_at, so a phone with a fast clock would
	// otherwise block every later correction of that item.
	maxClockSkew = 5 * time.Minute
)

var (
	ErrNotInspector        = errors.New("only the inspector who started this inspection can change it")
	ErrInspectionCompleted = errors.New("inspection is completed; start a new inspection to correct it")
	ErrItemNotFound        = errors.New("item not found")
	ErrSeverityRequired    = errors.New("severity is required for a fail")
	ErrSeverityNotAllowed  = errors.New("severity is only given for a fail")
	ErrBadSeverity         = errors.New("severity must be low, medium, high or critical")
	ErrBadCoverage         = errors.New("coverage must be unactioned, actioned, waived or superseded")
	ErrCommentTooLong      = errors.New("comment must be at most 2000 characters")
	ErrBadEvidenceIDs      = errors.New("evidence_ids must be UUIDs")
	ErrRepeatedEvidence    = errors.New("evidence_ids must not repeat")
	ErrTooManyPhotos       = errors.New("at most 20 photos per item")
	ErrAnswerInFuture      = errors.New("client_answered_at is in the future; check the device clock")
	ErrStartedInFuture     = errors.New("client_started_at is in the future; check the device clock")
	ErrCompletedInFuture   = errors.New("client_completed_at is in the future; check the device clock")

	// Sent with the offending ids (EvidenceError).
	ErrEvidenceNotFound    = errors.New("evidence not found on this site")
	ErrEvidenceRejected    = errors.New("evidence was rejected")
	ErrEvidenceNotVerified = errors.New("evidence not verified yet")

	// Sent with the stored answer (StaleAnswerError).
	ErrNewerAnswer     = errors.New("a newer answer is already stored")
	ErrDifferentAnswer = errors.New("a different answer is already stored for this time")
)

// EvidenceError is a photo that can't be attached, with the ids concerned.
type EvidenceError struct {
	Err error // ErrEvidenceNotFound, ErrEvidenceRejected or ErrEvidenceNotVerified
	IDs []string
}

func (e *EvidenceError) Error() string { return e.Err.Error() }
func (e *EvidenceError) Unwrap() error { return e.Err }

// StaleAnswerError refuses an answer that would overwrite a newer or a
// different one sent at the same time; Stored is what the server keeps.
type StaleAnswerError struct {
	Err    error // ErrNewerAnswer or ErrDifferentAnswer
	Stored InspectionItem
}

func (e *StaleAnswerError) Error() string { return e.Err.Error() }
func (e *StaleAnswerError) Unwrap() error { return e.Err }

// CompleteRequest closes an inspection; nothing about it changes afterwards.
type CompleteRequest struct {
	ClientCompletedAt time.Time `json:"client_completed_at" binding:"required"`
}

// InspectionFinding is a finding as part of its inspection.
type InspectionFinding struct {
	ItemKey  string    `json:"item_key"`
	Severity string    `json:"severity"`
	RaisedAt time.Time `json:"raised_at"`
}

// Finding is a finding in a site's list, with enough of its inspection and
// item to show without fetching them.
type Finding struct {
	InspectionID    string    `json:"inspection_id"`
	SiteID          string    `json:"site_id"`
	ItemKey         string    `json:"item_key"`
	Severity        string    `json:"severity"`
	RaisedAt        time.Time `json:"raised_at"`
	TemplateKey     string    `json:"template_key"`
	TemplateVersion int       `json:"template_version"`
	TemplateTitle   string    `json:"template_title"`
	Prompt          string    `json:"prompt"`
	// The completed correction of its inspection, which supersedes it; null
	// while there is none, or it is in progress or abandoned.
	CorrectedBy *string `json:"corrected_by"`
	// How the finding is addressed (D1, D7): superseded, actioned, waived or
	// unactioned, from the database's finding_coverage (000019).
	Coverage string `json:"coverage"`
	// The actions that cover it (its own, or carried from the same item
	// earlier in its correction chain), and the waiver that does.
	CoveredBy []string `json:"covered_by"`
	WaiverID  *string  `json:"waiver_id"`
}

// The values of Finding.Coverage.
const (
	CoverageUnactioned = "unactioned"
	CoverageActioned   = "actioned"
	CoverageWaived     = "waived"
	CoverageSuperseded = "superseded"
)

var Coverages = []string{CoverageUnactioned, CoverageActioned, CoverageWaived, CoverageSuperseded}

var (
	// Sent with the item keys concerned (IncompleteError).
	ErrItemsNotAnswered = errors.New("items not answered")
	ErrFailNeedsPhoto   = errors.New("a failed item needs a photo")
)

// IncompleteError refuses a completion, naming the items to fix, in
// template order.
type IncompleteError struct {
	Err      error // ErrItemsNotAnswered or ErrFailNeedsPhoto
	ItemKeys []string
}

func (e *IncompleteError) Error() string { return e.Err.Error() }
func (e *IncompleteError) Unwrap() error { return e.Err }

// AbandonRequest gives up an inspection still in progress. The reason is
// kept, with who abandoned it and when.
type AbandonRequest struct {
	Reason string `json:"reason" binding:"required"`
}

const maxReasonChars = 500 // the column's CHECK

var (
	ErrInspectionAbandoned = errors.New("inspection is abandoned")
	ErrCantAbandon         = errors.New("only the inspector who started this inspection, an owner or a SHE officer can abandon it")
	ErrAbandonNeedsLead    = errors.New("an inspection with a failed item can only be abandoned by an owner or a SHE officer")
	ErrBadReason           = errors.New("reason must be 1-500 characters")
)
