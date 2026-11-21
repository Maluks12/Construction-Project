package deadlines

import (
	"errors"
	"time"
)

// SatisfactionRequest records that a deadline's duty was met.
type SatisfactionRequest struct {
	SatisfiedAt     time.Time `json:"satisfied_at"`
	Method          string    `json:"method"`
	Reference       *string   `json:"reference"`
	Note            *string   `json:"note"`
	EvidenceID      *string   `json:"evidence_id"`
	DocumentID      *string   `json:"document_id"`
	DocumentVersion *int      `json:"document_version"`
}

// RetractRequest takes a satisfaction back.
type RetractRequest struct {
	Reason string `json:"reason"`
}

// DeadlineDetail is one deadline row with every satisfaction of its duty,
// retracted ones included, oldest first.
type DeadlineDetail struct {
	Deadline
	Satisfactions []Satisfaction `json:"satisfactions"`
}

// Errors with fixed texts; docs/api/deadlines.md quotes each one. The texts
// are M8's, from when these endpoints were in incidents.
var (
	ErrDeadlineNotFound     = errors.New("deadline not found")
	ErrBadSatisfactionID    = errors.New("satisfaction id must be a UUID")
	ErrSatisfactionNotFound = errors.New("satisfaction not found")
	ErrSatisfactionIDReused = errors.New("satisfaction id already used with different details")
	ErrAlreadySatisfied     = errors.New("deadline is already satisfied; retract that satisfaction first")
	ErrDeadlineWithdrawn    = errors.New("deadline is withdrawn; its rule no longer applies")
	ErrNoSatisfiedAt        = errors.New("satisfied_at is required")
	ErrSatisfiedInFuture    = errors.New("satisfied_at is in the future; check the device clock")
	ErrSatisfiedBeforeBasis = errors.New("satisfied_at must not be before the deadline's basis_at")
	ErrBadMethod            = errors.New("method must be phone, email, fax, in_person, online, post or other")
	ErrBadReference         = errors.New("reference must be 1-200 characters")
	ErrBadNote              = errors.New("note must be 1-2000 characters")
	ErrBadEvidenceID        = errors.New("evidence_id must be a UUID")
	ErrCantRetract          = errors.New("only an owner or a SHE officer can retract a satisfaction")
	ErrBadReason            = errors.New("reason must be 1-500 characters")
	ErrRetractionDiffers    = errors.New("satisfaction was already retracted with a different reason")
	ErrBadState             = errors.New("state must be open, due_now, overdue, satisfied or satisfied_late")
	ErrBadLimit             = errors.New("limit must be 1-200")
	ErrBadSubjectType       = errors.New("subject_type must be incident or site_work")
	ErrBeforeCalendar       = errors.New("deadlines can't be computed for this incident: the holiday calendar starts on 2024-01-01")
	ErrEvidenceNotFound     = errors.New("evidence not found on this site")
	ErrEvidenceRejected     = errors.New("evidence was rejected")
	ErrEvidenceNotVerified  = errors.New("evidence not verified yet")

	ErrBadDocumentID     = errors.New("document_id must be a UUID")
	ErrDocumentPair      = errors.New("document_id and document_version are given together")
	ErrDocumentNotFound  = errors.New("document version not found in this site's health and safety file")
	ErrNotProof          = errors.New("that document type is not proof of this deadline")
	ErrDocumentRetracted = errors.New("that document version is retracted")
)

// EvidenceError names the photo a satisfaction can't use.
type EvidenceError struct {
	Err error // ErrEvidenceNotFound, ErrEvidenceRejected or ErrEvidenceNotVerified
	IDs []string
}

func (e *EvidenceError) Error() string { return e.Err.Error() }
func (e *EvidenceError) Unwrap() error { return e.Err }

var sentinels = []error{
	ErrDeadlineNotFound, ErrBadSatisfactionID, ErrSatisfactionNotFound, ErrSatisfactionIDReused, ErrAlreadySatisfied,
	ErrDeadlineWithdrawn, ErrNoSatisfiedAt, ErrSatisfiedInFuture, ErrSatisfiedBeforeBasis, ErrBadMethod, ErrBadReference,
	ErrBadNote, ErrBadEvidenceID, ErrCantRetract, ErrBadReason, ErrRetractionDiffers, ErrBadState, ErrBadLimit,
	ErrBadSubjectType, ErrBeforeCalendar, ErrEvidenceNotFound, ErrEvidenceRejected, ErrEvidenceNotVerified,
	ErrBadDocumentID, ErrDocumentPair, ErrDocumentNotFound, ErrNotProof, ErrDocumentRetracted,
}

func isSentinel(err error) bool {
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}
