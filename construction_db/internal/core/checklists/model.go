// Package checklists is the checklist engine (Rev 4 §2.4): templates come
// from the site's regime, never from code.
package checklists

import "errors"

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
