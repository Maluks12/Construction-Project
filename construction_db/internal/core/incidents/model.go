// Package incidents is the incident register (Rev 4 §2.6, A.6): incidents
// classified from the site's regime taxonomy, amended only by new versions,
// with witness statements, photos, and the deadlines each version starts
// (internal/core/deadlines). Endpoints are in docs/api/incidents.md.
package incidents

import (
	"errors"
	"time"

	"construction_db/internal/core/deadlines"
)

// SubmitVersionRequest is one whole account of the incident. Version 1
// creates the incident; each later version restates everything, and the
// previous ones stay readable as submitted.
type SubmitVersionRequest struct {
	TaxonomyCodes     []string      `json:"taxonomy_codes"`
	OccurredAt        time.Time     `json:"occurred_at"`
	EmployerAwareAt   *time.Time    `json:"employer_aware_at"`
	Location          *string       `json:"location"`
	Description       *string       `json:"description"`
	People            []PersonInput `json:"people"`
	EvidenceIDs       []string      `json:"evidence_ids"`
	ClientSubmittedAt time.Time     `json:"client_submitted_at"`
	DeviceID          *string       `json:"device_id"`
	AmendReason       *string       `json:"amend_reason"`
}

// PersonInput is someone in the incident. A death is an injury, so died_at
// needs injured. Injury details are only given for someone injured, and
// employer_name only for someone not employed by the organisation.
type PersonInput struct {
	Name              *string    `json:"name"`
	Occupation        *string    `json:"occupation"`
	EmployeeOfOrg     bool       `json:"employee_of_org"`
	EmployerName      *string    `json:"employer_name"`
	Injured           bool       `json:"injured"`
	InjuryDescription *string    `json:"injury_description"`
	BodyPart          *string    `json:"body_part"`
	Treatment         *string    `json:"treatment"`
	DiedAt            *time.Time `json:"died_at"`
}

// The values of PersonInput.Treatment.
var Treatments = []string{"none", "first_aid", "medical_treatment", "hospitalised"}

// Person is a stored PersonInput, with its position in the version.
type Person struct {
	Position int `json:"position"`
	PersonInput
}

// IncidentType is one of a version's types, with its label and whether the
// regime makes it reportable.
type IncidentType struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Reportable bool   `json:"reportable"`
}

// AttachedEvidence is a photo in a version or statement: its id and capture
// time only, which is not a view of the evidence (see CLAUDE.md, "Evidence
// reads are logged"). The photo itself goes through the logged view.
type AttachedEvidence struct {
	EvidenceID       string    `json:"evidence_id"`
	ClientCapturedAt time.Time `json:"client_captured_at"`
}

// Version is one submitted account.
type Version struct {
	Version           int                `json:"version"`
	Types             []IncidentType     `json:"types"`
	OccurredAt        time.Time          `json:"occurred_at"`
	EmployerAwareAt   *time.Time         `json:"employer_aware_at"`
	Location          *string            `json:"location"`
	Description       *string            `json:"description"`
	People            []Person           `json:"people"`
	Evidence          []AttachedEvidence `json:"evidence"`
	SubmittedBy       string             `json:"submitted_by"`
	ClientSubmittedAt time.Time          `json:"client_submitted_at"`
	SubmittedAt       time.Time          `json:"submitted_at"`
	DeviceID          *string            `json:"device_id"`
	AmendReason       *string            `json:"amend_reason"`
}

// WitnessStatement is a witness's own account. It never changes; a
// correction is a further statement with supersedes_id, and the one it
// corrects shows superseded_by.
type WitnessStatement struct {
	ID             string            `json:"id"`
	WitnessName    string            `json:"witness_name"`
	WitnessContact *string           `json:"witness_contact"`
	Statement      string            `json:"statement"`
	TakenBy        string            `json:"taken_by"`
	TakenAt        time.Time         `json:"taken_at"`
	RecordedAt     time.Time         `json:"recorded_at"`
	DeviceID       *string           `json:"device_id"`
	Evidence       *AttachedEvidence `json:"evidence"`
	SupersedesID   *string           `json:"supersedes_id"`
	SupersededBy   *string           `json:"superseded_by"`
}

// Incident is one incident as of the version asked for (the latest by
// default), with every deadline row and every statement.
type Incident struct {
	ID            string    `json:"id"`
	SiteID        string    `json:"site_id"`
	ReportedBy    string    `json:"reported_by"`
	CreatedAt     time.Time `json:"created_at"`
	LatestVersion int       `json:"latest_version"`
	// True when any of the version's types is reportable in the regime.
	Reportable bool    `json:"reportable"`
	Version    Version `json:"version"`
	// What this version did to the deadlines, by deadline row id.
	Changes           DeadlineChanges      `json:"changes"`
	Deadlines         []deadlines.Deadline `json:"deadlines"`
	WitnessStatements []WitnessStatement   `json:"witness_statements"`
}

// DeadlineChanges says what one version did: rows it added (new duties, or
// duties that apply again), rows it superseded and their replacements, rows
// it withdrew, and live rows it carried unchanged.
type DeadlineChanges struct {
	Added      []string `json:"added"`
	Superseded []string `json:"superseded"`
	Withdrawn  []string `json:"withdrawn"`
	Carried    []string `json:"carried"`
}

// IncidentSummary is one row of the register: the latest version, in brief.
type IncidentSummary struct {
	ID            string         `json:"id"`
	SiteID        string         `json:"site_id"`
	LatestVersion int            `json:"latest_version"`
	Types         []IncidentType `json:"types"`
	Reportable    bool           `json:"reportable"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Location      *string        `json:"location"`
	ReportedBy    string         `json:"reported_by"`
	CreatedAt     time.Time      `json:"created_at"`
	PeopleCount   int            `json:"people_count"`
	InjuredCount  int            `json:"injured_count"`
	DeathCount    int            `json:"death_count"`
	// Live deadlines not yet met, and how many of those are past due.
	OpenDeadlines    int        `json:"open_deadlines"`
	OverdueDeadlines int        `json:"overdue_deadlines"`
	NextDueAt        *time.Time `json:"next_due_at"`
}

// StatementRequest records a witness statement under an id the device chose.
type StatementRequest struct {
	WitnessName    string    `json:"witness_name"`
	WitnessContact *string   `json:"witness_contact"`
	Statement      string    `json:"statement"`
	TakenAt        time.Time `json:"taken_at"`
	DeviceID       *string   `json:"device_id"`
	EvidenceID     *string   `json:"evidence_id"`
	SupersedesID   *string   `json:"supersedes_id"`
}

// SatisfactionRequest records that a deadline's duty was met.
type SatisfactionRequest struct {
	SatisfiedAt time.Time `json:"satisfied_at"`
	Method      string    `json:"method"`
	Reference   *string   `json:"reference"`
	Note        *string   `json:"note"`
	EvidenceID  *string   `json:"evidence_id"`
}

// RetractRequest takes a satisfaction back.
type RetractRequest struct {
	Reason string `json:"reason"`
}

// DeadlineDetail is one deadline row with every satisfaction of its duty,
// retracted ones included, oldest first.
type DeadlineDetail struct {
	deadlines.Deadline
	Satisfactions []deadlines.Satisfaction `json:"satisfactions"`
}

// Errors with fixed texts; docs/api/incidents.md quotes each one.
var (
	ErrIncidentNotFound    = errors.New("incident not found")
	ErrBadIncidentID       = errors.New("incident id must be a UUID")
	ErrBadVersion          = errors.New("version must be a whole number, 1 or more")
	ErrVersionNotFound     = errors.New("version not found")
	ErrSiteArchived        = errors.New("site is archived")
	ErrIDReused            = errors.New("incident id already used with different details")
	ErrVersionDiffers      = errors.New("this version was already submitted with different details")
	ErrNotNextVersion      = errors.New("not the next version; get the incident and amend its latest version")
	ErrBadDeviceID         = errors.New("device_id must be 1-200 bytes")
	ErrNoTypes             = errors.New("taxonomy_codes must list at least one incident type")
	ErrTooManyTypes        = errors.New("at most 10 incident types")
	ErrRepeatedTypes       = errors.New("taxonomy_codes must not repeat")
	ErrUnknownTypes        = errors.New("taxonomy_codes must be incident types of the site's regime")
	ErrNoOccurredAt        = errors.New("occurred_at is required")
	ErrOccurredInFuture    = errors.New("occurred_at is in the future; check the device clock")
	ErrAwareBeforeIncident = errors.New("employer_aware_at must not be before occurred_at")
	ErrAwareInFuture       = errors.New("employer_aware_at is in the future; check the device clock")
	ErrAwareRequired       = deadlines.ErrEmployerAwareRequired
	ErrNoSubmittedAt       = errors.New("client_submitted_at is required")
	ErrSubmittedInFuture   = errors.New("client_submitted_at is in the future; check the device clock")
	ErrBadLocation         = errors.New("location must be 1-500 characters")
	ErrBadDescription      = errors.New("description must be 1-5000 characters")
	ErrTooManyPeople       = errors.New("at most 50 people")
	ErrBadPerson           = errors.New("people: name, occupation, employer_name and body_part must be 1-200 characters, injury_description 1-2000")
	ErrBadTreatment        = errors.New("treatment must be none, first_aid, medical_treatment or hospitalised")
	ErrInjuryNotInjured    = errors.New("injury details are only given for someone injured")
	ErrDeathNotInjured     = errors.New("someone who died must be marked injured")
	ErrEmployerOfEmployee  = errors.New("employer_name is only given for someone not employed by the organisation")
	ErrDeathBeforeIncident = errors.New("died_at must not be before occurred_at")
	ErrDeathInFuture       = errors.New("died_at is in the future; check the device clock")
	ErrDeathNotReportable  = errors.New("a death must be classified with at least one reportable incident type")
	ErrBadEvidenceIDs      = errors.New("evidence_ids must be UUIDs")
	ErrRepeatedEvidence    = errors.New("evidence_ids must not repeat")
	ErrTooManyPhotos       = errors.New("at most 20 photos")
	ErrReasonRequired      = errors.New("amend_reason is required for an amendment (1-500 characters)")
	ErrReasonOnFirst       = errors.New("version 1 has no amend_reason")
	ErrWeakeningNeedsLead  = errors.New("only an owner or a SHE officer can withdraw a report deadline or move it later")
	ErrBeforeCalendar      = errors.New("deadlines can't be computed for this incident: the holiday calendar starts on 2024-01-01")
	ErrEvidenceNotFound    = errors.New("evidence not found on this site")
	ErrEvidenceRejected    = errors.New("evidence was rejected")
	ErrEvidenceNotVerified = errors.New("evidence not verified yet")
	ErrBadType             = errors.New("type must be an incident type of the site's regime")
	ErrBadReportable       = errors.New("reportable must be true or false")
	ErrBadOpen             = errors.New("open_deadlines must be true or false")
	ErrBadLimit            = errors.New("limit must be 1-200")

	ErrBadStatementID     = errors.New("statement id must be a UUID")
	ErrStatementIDReused  = errors.New("statement id already used with different details")
	ErrBadWitnessName     = errors.New("witness_name must be 1-200 characters")
	ErrBadWitnessContact  = errors.New("witness_contact must be 1-200 characters")
	ErrBadStatement       = errors.New("statement must be 1-20000 characters")
	ErrNoTakenAt          = errors.New("taken_at is required")
	ErrTakenInFuture      = errors.New("taken_at is in the future; check the device clock")
	ErrBadSupersedes      = errors.New("supersedes_id must be a statement on this incident")
	ErrStatementCorrected = errors.New("that statement already has a correction")

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
)

// EvidenceError names the photos a request can't use.
type EvidenceError struct {
	Err error // ErrEvidenceNotFound, ErrEvidenceRejected or ErrEvidenceNotVerified
	IDs []string
}

func (e *EvidenceError) Error() string { return e.Err.Error() }
func (e *EvidenceError) Unwrap() error { return e.Err }

// TypesError names the taxonomy codes the site's regime doesn't have.
type TypesError struct {
	Codes []string
}

func (e *TypesError) Error() string { return ErrUnknownTypes.Error() }
func (e *TypesError) Unwrap() error { return ErrUnknownTypes }

// NotNextError tells the client which version the incident is at.
type NotNextError struct {
	LatestVersion int
}

func (e *NotNextError) Error() string { return ErrNotNextVersion.Error() }
func (e *NotNextError) Unwrap() error { return ErrNotNextVersion }
