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

	ResultPass = "pass"
	ResultFail = "fail"
	ResultNA   = "na"
)

var (
	ErrInspectionNotFound = errors.New("inspection not found")
	ErrBadInspectionID    = errors.New("inspection id must be a UUID")
	ErrInspectionIDReused = errors.New("inspection id already used with different details")
	ErrSiteArchived       = errors.New("site is archived")
	ErrBadCorrectsID      = errors.New("corrects_id must be a completed inspection on this site")
	ErrBadDeviceID        = errors.New("device_id must be 1-200 bytes")
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

// Inspection carries its own copy of the template version's wording, so it
// reads the same whatever the regime data says later.
type Inspection struct {
	ID                string           `json:"id"`
	SiteID            string           `json:"site_id"`
	Status            string           `json:"status"`
	TemplateKey       string           `json:"template_key"`
	TemplateVersion   int              `json:"template_version"`
	TemplateSHA256    string           `json:"template_sha256"` // lowercase hex
	TemplateTitle     string           `json:"template_title"`
	TemplateSourceRef string           `json:"template_source_ref"`
	ItemCount         int              `json:"item_count"`
	StartedBy         string           `json:"started_by"`
	ClientStartedAt   time.Time        `json:"client_started_at"`
	StartedAt         time.Time        `json:"started_at"`
	DeviceID          *string          `json:"device_id"`
	CorrectsID        *string          `json:"corrects_id"`
	ClientCompletedAt *time.Time       `json:"client_completed_at"`
	CompletedAt       *time.Time       `json:"completed_at"`
	Items             []InspectionItem `json:"items"`
}

// InspectionItem is one template item with its answer; the answer fields are
// null until the item is answered.
type InspectionItem struct {
	Key              string     `json:"key"`
	Position         int        `json:"position"`
	Prompt           string     `json:"prompt"`
	SourceRef        *string    `json:"source_ref"`
	ResponseType     string     `json:"response_type"`
	Result           *string    `json:"result"`
	Severity         *string    `json:"severity"`
	Comment          *string    `json:"comment"`
	ClientAnsweredAt *time.Time `json:"client_answered_at"`
	AnsweredAt       *time.Time `json:"answered_at"`
}
