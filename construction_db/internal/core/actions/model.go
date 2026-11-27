// Package actions is the corrective action register (Rev 4 §2.5): actions
// raised from inspection findings or incident versions, with an owner, a due
// date, close-outs with photos, verification, and waivers of low and medium
// findings. Decisions D1–D8 are in construction_db/CLAUDE.md; endpoints in
// docs/api/actions.md. Migrations 000018 and 000019 enforce every rule.
package actions

import (
	"errors"
	"time"
)

// An action's stored status.
const (
	StatusOpen      = "open"
	StatusAwaiting  = "awaiting_verification"
	StatusClosed    = "closed"
	StatusCancelled = "cancelled"
)

// The states the API shows (D8): the status, with overdue and closed_late
// worked out from the due time.
const (
	StateOpen       = "open"
	StateOverdue    = "overdue"
	StateAwaiting   = "awaiting_verification"
	StateClosed     = "closed"
	StateClosedLate = "closed_late"
	StateCancelled  = "cancelled"
)

var States = []string{StateOpen, StateOverdue, StateAwaiting, StateClosed, StateClosedLate, StateCancelled}

// DefaultDueDays: days from the local date an action is raised to its
// default due date, by severity (D2). It mirrors action_default_due_days
// (000018); a test compares them.
var DefaultDueDays = map[string]int{"low": 14, "medium": 7, "high": 1, "critical": 0}

// Source types.
const (
	SourceFinding  = "finding"
	SourceIncident = "incident"
)

var Sources = []string{SourceFinding, SourceIncident}

// Event kinds as stored.
const (
	KindRaised            = "raised"
	KindReassigned        = "reassigned"
	KindDueChanged        = "due_changed"
	KindCancelled         = "cancelled"
	KindCloseoutSubmitted = "closeout_submitted"
	KindVerified          = "verified"
	KindRejected          = "rejected"
	KindReopened          = "reopened"
)

var EventKinds = []string{KindRaised, KindReassigned, KindDueChanged, KindCancelled, KindCloseoutSubmitted, KindVerified, KindRejected, KindReopened}

// What an EventRequest asks for.
const (
	DoReassign = "reassign"
	DoMoveDue  = "move_due"
	DoVerify   = "verify"
	DoReject   = "reject"
	DoReopen   = "reopen"
	DoCancel   = "cancel"
)

var Requests = []string{DoReassign, DoMoveDue, DoVerify, DoReject, DoReopen, DoCancel}

// What a request records.
var kindOf = map[string]string{
	DoReassign: KindReassigned, DoMoveDue: KindDueChanged, DoVerify: KindVerified,
	DoReject: KindRejected, DoReopen: KindReopened, DoCancel: KindCancelled,
}

// Review flags on an action whose finding was corrected (D7).
const (
	ReviewSeverityRaised = "severity_raised"
	ReviewItemNotFailed  = "item_not_failed"
)

var Reviews = []string{ReviewSeverityRaised, ReviewItemNotFailed}

// FindingRef names a finding: an inspection's failed item.
type FindingRef struct {
	InspectionID string `json:"inspection_id"`
	ItemKey      string `json:"item_key"`
}

// IncidentRef names an incident version.
type IncidentRef struct {
	IncidentID string `json:"incident_id"`
	Version    int    `json:"version"`
}

// RaiseRequest raises an action under an id the device chose, from exactly
// one of a finding or an incident version.
type RaiseRequest struct {
	Finding  *FindingRef  `json:"finding"`
	Incident *IncidentRef `json:"incident"`
	// Required for an incident. A finding's action takes the finding's
	// severity; if sent, it must match.
	Severity         *string `json:"severity"`
	Title            string  `json:"title"`
	Description      *string `json:"description"`
	OwnerID          string  `json:"owner_id"`
	ResponsibleParty *string `json:"responsible_party"`
	// YYYY-MM-DD in the site regime's time zone; the severity's default when
	// absent. Later than the default needs an owner or SHE officer and
	// due_reason.
	DueOn          *string   `json:"due_on"`
	DueReason      *string   `json:"due_reason"`
	ClientRaisedAt time.Time `json:"client_raised_at"`
	DeviceID       *string   `json:"device_id"`
}

// EventRequest records one change to an action under an id the device chose.
type EventRequest struct {
	Kind string `json:"kind"`
	// reassign: the new owner, and the responsible party (null for none).
	OwnerID          *string `json:"owner_id"`
	ResponsibleParty *string `json:"responsible_party"`
	// move_due: the new due date.
	DueOn *string `json:"due_on"`
	// verify and reject: the close-out awaiting verification.
	CloseoutID *string   `json:"closeout_id"`
	Reason     *string   `json:"reason"`
	ClientAt   time.Time `json:"client_at"`
	DeviceID   *string   `json:"device_id"`
}

// CloseoutRequest claims the work is done, with photos.
type CloseoutRequest struct {
	Note              *string   `json:"note"`
	EvidenceIDs       []string  `json:"evidence_ids"`
	ClientSubmittedAt time.Time `json:"client_submitted_at"`
	DeviceID          *string   `json:"device_id"`
}

// ActionHeader is an action as the register lists it.
type ActionHeader struct {
	ID     string `json:"id"`
	SiteID string `json:"site_id"`
	Source string `json:"source"`
	// A finding's action.
	InspectionID *string `json:"inspection_id"`
	ItemKey      *string `json:"item_key"`
	// The completed correction of the finding's inspection, which supersedes
	// the finding (D7); null otherwise.
	SourceCorrectedBy *string `json:"source_corrected_by"`
	// An incident's action: the version it was raised against, and the
	// incident's latest version now.
	IncidentID            *string `json:"incident_id"`
	IncidentVersion       *int    `json:"incident_version"`
	IncidentLatestVersion *int    `json:"incident_latest_version"`
	// Whether a lead should look again because the finding was corrected.
	Review         *string   `json:"review"`
	Severity       string    `json:"severity"`
	Title          string    `json:"title"`
	Description    *string   `json:"description"`
	RaisedBy       string    `json:"raised_by"`
	ClientRaisedAt time.Time `json:"client_raised_at"`
	RaisedAt       time.Time `json:"raised_at"`
	DeviceID       *string   `json:"device_id"`
	TimeZone       string    `json:"time_zone"`
	SourceAt       time.Time `json:"source_at"`
	DefaultDueOn   string    `json:"default_due_on"`
	Status         string    `json:"status"`
	State          string    `json:"state"`
	OwnerID        string    `json:"owner_id"`
	// Whether the owner can still act on it: an active member whom
	// action_site_member() admits on its site, the rule raise and reassign
	// use. False once they're suspended, removed, in another role or off the
	// site: it needs a new owner (ONBOARD review R1).
	OwnerEligible     bool       `json:"owner_eligible"`
	ResponsibleParty  *string    `json:"responsible_party"`
	DueOn             string     `json:"due_on"`
	DueAt             time.Time  `json:"due_at"`
	PendingCloseoutID *string    `json:"pending_closeout_id"`
	AwaitingSince     *time.Time `json:"awaiting_since"`
	ClosedAt          *time.Time `json:"closed_at"`
}

// Action is one action with its whole history.
type Action struct {
	ActionHeader
	Events    []Event    `json:"events"`
	Closeouts []Closeout `json:"closeouts"`
}

// Event is one change to an action, with the action's state after it.
type Event struct {
	ID               string    `json:"id"`
	Seq              int       `json:"seq"`
	Kind             string    `json:"kind"`
	ActorID          string    `json:"actor_id"`
	ClientAt         time.Time `json:"client_at"`
	RecordedAt       time.Time `json:"recorded_at"`
	DeviceID         *string   `json:"device_id"`
	Reason           *string   `json:"reason"`
	Status           string    `json:"status"`
	OwnerID          string    `json:"owner_id"`
	ResponsibleParty *string   `json:"responsible_party"`
	DueOn            string    `json:"due_on"`
	CloseoutID       *string   `json:"closeout_id"`
	SelfVerified     *bool     `json:"self_verified"`
}

// Closeout is a claim that the work was done, and what became of it.
type Closeout struct {
	ID                string             `json:"id"`
	Note              *string            `json:"note"`
	SubmittedBy       string             `json:"submitted_by"`
	ClientSubmittedAt time.Time          `json:"client_submitted_at"`
	SubmittedAt       time.Time          `json:"submitted_at"`
	DeviceID          *string            `json:"device_id"`
	Evidence          []CloseoutEvidence `json:"evidence"`
	// verified, rejected, or null while awaiting (or if the action was
	// cancelled first).
	Decision *string `json:"decision"`
	// Submitted at or after the due time, by the device's clock.
	Late bool `json:"late"`
	// In time by the device's clock but received after the due time (an
	// offline phone), as for M8 satisfactions.
	RecordedAfterDue bool `json:"recorded_after_due"`
}

// CloseoutEvidence is a close-out photo: its id and capture time, which is
// not a view of the evidence (CLAUDE.md, "Evidence reads are logged").
type CloseoutEvidence struct {
	EvidenceID       string    `json:"evidence_id"`
	ClientCapturedAt time.Time `json:"client_captured_at"`
	// Taken before the action was raised: fixed on the spot, recorded after (D5).
	CapturedBeforeRaised bool `json:"captured_before_raised"`
}

// Errors with fixed texts; docs/api/actions.md quotes each one.
var (
	ErrActionNotFound    = errors.New("action not found")
	ErrBadActionID       = errors.New("action id must be a UUID")
	ErrActionIDReused    = errors.New("action id already used with different details")
	ErrSiteArchived      = errors.New("site is archived")
	ErrOneSource         = errors.New("give exactly one of finding or incident")
	ErrBadFinding        = errors.New("finding needs inspection_id (a UUID) and item_key")
	ErrFindingNotFound   = errors.New("finding not found on this site")
	ErrFindingSuperseded = errors.New("finding is superseded by a correction; act on the correction's finding")
	ErrBadIncident       = errors.New("incident needs incident_id (a UUID) and version (1 or more)")
	ErrIncidentNotFound  = errors.New("incident version not found on this site")
	ErrNotLatestVersion  = errors.New("raise it against the incident's latest version")
	ErrBadSeverity       = errors.New("severity must be low, medium, high or critical")
	ErrSeverityRequired  = errors.New("severity is required for an incident's action")
	ErrSeverityMismatch  = errors.New("a finding's action has the finding's severity; leave severity out")
	ErrBadTitle          = errors.New("title must be 1-200 characters")
	ErrBadDescription    = errors.New("description must be 1-2000 characters")
	ErrBadOwnerID        = errors.New("owner_id must be a UUID")
	ErrOwnerNotEligible  = errors.New("owner must be an owner, SHE officer or supervisor on this site")
	ErrBadParty          = errors.New("responsible_party must be 1-200 characters")
	ErrBadDueOn          = errors.New("due_on must be a date, YYYY-MM-DD")
	ErrDueBeforeRaised   = errors.New("due_on must not be before the day the action is raised")
	ErrDueInPast         = errors.New("due_on must not be in the past")
	ErrLaterNeedsLead    = errors.New("only an owner or a SHE officer can set a due date later than the default or move it later")
	ErrDueReasonRequired = errors.New("due_reason is required for a due date later than the default (1-500 characters)")
	ErrNoRaisedAt        = errors.New("client_raised_at is required")
	ErrRaisedInFuture    = errors.New("client_raised_at is in the future; check the device clock")
	ErrBadDeviceID       = errors.New("device_id must be 1-200 bytes")

	ErrBadEventID       = errors.New("event id must be a UUID")
	ErrEventIDReused    = errors.New("event id already used with different details")
	ErrBadKind          = errors.New("kind must be reassign, move_due, verify, reject, reopen or cancel")
	ErrNoClientAt       = errors.New("client_at is required")
	ErrClientAtInFuture = errors.New("client_at is in the future; check the device clock")
	ErrBadReason        = errors.New("reason must be 1-500 characters")
	ErrReasonRequired   = errors.New("reason is required to reject, reopen or cancel (1-500 characters)")
	ErrMoveLaterReason  = errors.New("reason is required to move a due date later (1-500 characters)")
	ErrUnexpectedField  = errors.New("a field was given that this kind doesn't take")
	ErrNothingChanges   = errors.New("nothing would change")
	ErrBadCloseoutRef   = errors.New("closeout_id must be a UUID")
	ErrActionClosed     = errors.New("action is closed; reopen it first")
	ErrActionCancelled  = errors.New("action is cancelled")
	ErrNotAwaiting      = errors.New("action has no close-out awaiting verification")
	ErrNotPending       = errors.New("closeout_id is not the close-out awaiting verification")
	ErrNotClosed        = errors.New("only a closed action can be reopened")
	ErrCantReassign     = errors.New("only the action's raiser, an owner or a SHE officer can reassign it")
	ErrCantMoveEarlier  = errors.New("only the action's raiser, its owner, an owner or a SHE officer can move its due date earlier")
	ErrNeedsLead        = errors.New("only an owner or a SHE officer can verify, reject, reopen or cancel an action")

	ErrBadCloseoutID     = errors.New("close-out id must be a UUID")
	ErrCloseoutIDReused  = errors.New("close-out id already used with different details")
	ErrAlreadyAwaiting   = errors.New("a close-out is already awaiting verification")
	ErrCantCloseOut      = errors.New("only the action's owner, an owner or a SHE officer can close it out")
	ErrBadNote           = errors.New("note must be 1-2000 characters")
	ErrNoPhotos          = errors.New("a close-out needs at least one photo")
	ErrTooManyPhotos     = errors.New("at most 20 photos")
	ErrBadEvidenceIDs    = errors.New("evidence_ids must be UUIDs")
	ErrRepeatedEvidence  = errors.New("evidence_ids must not repeat")
	ErrNoSubmittedAt     = errors.New("client_submitted_at is required")
	ErrSubmittedInFuture = errors.New("client_submitted_at is in the future; check the device clock")

	// Sent with the photos concerned (EvidenceError).
	ErrEvidenceNotFound    = errors.New("evidence not found on this site")
	ErrEvidenceRejected    = errors.New("evidence was rejected")
	ErrEvidenceNotVerified = errors.New("evidence not verified yet")
	ErrEvidenceTooOld      = errors.New("evidence was taken before the action's source, so it can't show the fix")
	ErrEvidenceIsSource    = errors.New("evidence is one of the source's own photos; take a photo of the fix")

	ErrBadState         = errors.New("state must be open, overdue, awaiting_verification, closed, closed_late or cancelled")
	ErrBadSource        = errors.New("source must be finding or incident")
	ErrBadOwner         = errors.New("owner must be me or a user id")
	ErrBadLimit         = errors.New("limit must be 1-200")
	ErrBadOwnerEligible = errors.New("owner_eligible must be true or false")
)

// EvidenceError names the photos a close-out can't use.
type EvidenceError struct {
	Err error
	IDs []string
}

func (e *EvidenceError) Error() string { return e.Err.Error() }
func (e *EvidenceError) Unwrap() error { return e.Err }

// NotLatestError tells the client which version the incident is at.
type NotLatestError struct {
	LatestVersion int
}

func (e *NotLatestError) Error() string { return ErrNotLatestVersion.Error() }
func (e *NotLatestError) Unwrap() error { return ErrNotLatestVersion }
