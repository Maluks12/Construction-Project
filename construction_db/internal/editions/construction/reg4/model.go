// Package reg4 is a site's planned construction work (REG4): its start date
// and the facts that decide CR 2014 reg 4's notification and reg 3's permit
// application (site_work.json). Amended only by new versions; its deadlines
// go through internal/core/deadlines. Migrations 000045 and 000046 enforce
// the rules; endpoints in docs/api/site-work.md.
package reg4

import (
	"errors"
	"time"

	"construction_db/internal/core/deadlines"
)

// VersionRequest is one whole account of the work. Version 1 creates it.
type VersionRequest struct {
	Description       string    `json:"description"`
	StartsOn          string    `json:"starts_on"`
	EndsOn            *string   `json:"ends_on"`
	Facts             []string  `json:"facts"`
	Status            string    `json:"status"`
	ClientSubmittedAt time.Time `json:"client_submitted_at"`
	DeviceID          *string   `json:"device_id"`
	AmendReason       *string   `json:"amend_reason"`
}

// The values of Version.Status.
var Statuses = []string{"planned", "cancelled"}

// Fact is a fact from the site's regime, with its label and source.
type Fact struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	SourceRef string `json:"source_ref"`
}

type Version struct {
	Version           int       `json:"version"`
	Description       string    `json:"description"`
	StartsOn          string    `json:"starts_on"`
	EndsOn            *string   `json:"ends_on"`
	Facts             []Fact    `json:"facts"`
	Status            string    `json:"status"`
	SubmittedBy       string    `json:"submitted_by"`
	ClientSubmittedAt time.Time `json:"client_submitted_at"`
	SubmittedAt       time.Time `json:"submitted_at"`
	DeviceID          *string   `json:"device_id"`
	AmendReason       *string   `json:"amend_reason"`
	// A deadline this version computed was already past (REG4 D4).
	LateWhenRecorded bool `json:"late_when_recorded"`
}

// Changes says what one version did to the deadlines, by row id.
type Changes struct {
	Added      []string `json:"added"`
	Superseded []string `json:"superseded"`
	Withdrawn  []string `json:"withdrawn"`
	Carried    []string `json:"carried"`
}

// SiteWork is one record as of a version, with every deadline row.
type SiteWork struct {
	ID            string               `json:"id"`
	SiteID        string               `json:"site_id"`
	CreatedBy     string               `json:"created_by"`
	CreatedAt     time.Time            `json:"created_at"`
	LatestVersion int                  `json:"latest_version"`
	Version       Version              `json:"version"`
	Changes       Changes              `json:"changes"`
	Deadlines     []deadlines.Deadline `json:"deadlines"`
}

// Summary is one row of the register, as of the latest version.
type Summary struct {
	ID               string     `json:"id"`
	SiteID           string     `json:"site_id"`
	LatestVersion    int        `json:"latest_version"`
	Description      string     `json:"description"`
	StartsOn         string     `json:"starts_on"`
	EndsOn           *string    `json:"ends_on"`
	Facts            []string   `json:"facts"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	OpenDeadlines    int        `json:"open_deadlines"`
	OverdueDeadlines int        `json:"overdue_deadlines"`
	NextDueAt        *time.Time `json:"next_due_at"`
}

// Rule is a site_work rule as the form explains it.
type Rule struct {
	RuleKey     string     `json:"rule_key"`
	Label       string     `json:"label"`
	Period      int        `json:"period"`
	Unit        string     `json:"unit"`
	DutyHolder  string     `json:"duty_holder"`
	Caveat      *string    `json:"caveat"`
	SourceRef   string     `json:"source_ref"`
	WhenAny     [][]string `json:"when_any"`
	UnlessFacts []string   `json:"unless_facts"`
	UnlessRules []string   `json:"unless_rules"`
}

type Catalogue struct {
	Facts []Fact `json:"facts"`
	Rules []Rule `json:"rules"`
}

var (
	ErrNotFound             = errors.New("site work not found")
	ErrBadID                = errors.New("site work id must be a UUID")
	ErrBadVersion           = errors.New("version must be a whole number, 1 or more")
	ErrVersionNotFound      = errors.New("version not found")
	ErrIDReused             = errors.New("site work id already used with different details")
	ErrVersionDiffers       = errors.New("this version was already submitted with different details")
	ErrNotNextVersion       = errors.New("not the next version; get the site work and amend its latest version")
	ErrSiteArchived         = errors.New("site is archived")
	ErrBadDescription       = errors.New("description must be 1-500 characters")
	ErrBadStartsOn          = errors.New("starts_on must be a date (YYYY-MM-DD)")
	ErrBadEndsOn            = errors.New("ends_on must be a date (YYYY-MM-DD), not before starts_on")
	ErrStartOutsideCalendar = errors.New("starts_on is outside the years the holiday calendar covers")
	ErrRepeatedFacts        = errors.New("facts must not repeat")
	ErrUnknownFacts         = errors.New("facts must be site work facts of the site's regime")
	ErrBadStatus            = errors.New("status must be planned or cancelled")
	ErrFirstCancelled       = errors.New("version 1 is planned; cancel in a later version")
	ErrNoSubmittedAt        = errors.New("client_submitted_at is required")
	ErrSubmittedInFuture    = errors.New("client_submitted_at is in the future; check the device clock")
	ErrBadDeviceID          = errors.New("device_id must be 1-200 bytes")
	ErrReasonRequired       = errors.New("amend_reason is required for an amendment (1-500 characters)")
	ErrReasonOnFirst        = errors.New("version 1 has no amend_reason")
	ErrWeakeningNeedsLead   = errors.New("only an owner or a SHE officer can withdraw a notification deadline or move it later")
	ErrBadLimit             = errors.New("limit must be 1-200")
)

// FactsError names the facts the site's regime doesn't have.
type FactsError struct {
	Facts []string
}

func (e *FactsError) Error() string { return ErrUnknownFacts.Error() }
func (e *FactsError) Unwrap() error { return ErrUnknownFacts }

// NotNextError tells the client which version the record is at.
type NotNextError struct {
	LatestVersion int
}

func (e *NotNextError) Error() string { return ErrNotNextVersion.Error() }
func (e *NotNextError) Unwrap() error { return ErrNotNextVersion }

// Sentinels are the errors the handlers send; TestAPIDoc checks each is quoted.
var Sentinels = []error{ErrNotFound, ErrBadID, ErrBadVersion, ErrVersionNotFound, ErrIDReused, ErrVersionDiffers, ErrNotNextVersion,
	ErrSiteArchived, ErrBadDescription, ErrBadStartsOn, ErrBadEndsOn, ErrStartOutsideCalendar, ErrRepeatedFacts, ErrUnknownFacts,
	ErrBadStatus, ErrFirstCancelled, ErrNoSubmittedAt, ErrSubmittedInFuture, ErrBadDeviceID, ErrReasonRequired, ErrReasonOnFirst,
	ErrWeakeningNeedsLead, ErrBadLimit}
