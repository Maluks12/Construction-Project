// Package reports is the compliance summary (M12): per site and as an
// organisation rollup. An edition adds its own categories through
// Contributor; core never imports an edition.
package reports

import (
	"context"
	"errors"
	"time"

	"construction_db/internal/core/sites"
	"construction_db/internal/regimes"
)

// Colours (D1). A site's status is its worst category's colour.
const (
	Red   = "red"
	Amber = "amber"
	Green = "green"
)

// Category keys, in the order the summary lists them.
const (
	CatHSFile            = "hs_file"
	CatVault             = "vault"
	CatAppointments      = "appointments"
	CatFindings          = "findings"
	CatActions           = "actions"
	CatVerifications     = "verifications"
	CatSignIns           = "sign_ins"
	CatIncidentDeadlines = "incident_deadlines"
	CatSiteWorkDeadlines = "site_work_deadlines" // the edition's (REG4)
	CatWorkerRecords     = "worker_records"
	CatAbsentWorkers     = "absent_workers"
	CatPendingUploads    = "pending_uploads"
)

var CategoryKeys = []string{CatHSFile, CatVault, CatAppointments, CatFindings, CatActions, CatVerifications,
	CatSignIns, CatIncidentDeadlines, CatSiteWorkDeadlines, CatWorkerRecords, CatAbsentWorkers, CatPendingUploads}

// What an item is about: subject_type, with subject_id its record's id
// (null for a required type with nothing recorded).
const (
	SubjectDocument        = "document"
	SubjectDocumentType    = "document_type"
	SubjectAppointment     = "appointment"
	SubjectAppointmentType = "appointment_type"
	SubjectFinding         = "finding"
	SubjectAction          = "action"
	SubjectCloseout        = "closeout"
	SubjectSignIn          = "sign_in"
	SubjectDeadline        = "deadline"
	SubjectCompetency      = "competency"
	SubjectMedical         = "medical"
	SubjectRosterEntry     = "roster_entry"
	SubjectEvidence        = "evidence"
	SubjectFile            = "file"
)

var SubjectTypes = []string{SubjectDocument, SubjectDocumentType, SubjectAppointment, SubjectAppointmentType, SubjectFinding,
	SubjectAction, SubjectCloseout, SubjectSignIn, SubjectDeadline, SubjectCompetency, SubjectMedical, SubjectRosterEntry,
	SubjectEvidence, SubjectFile}

// Item states: why the item is listed.
const (
	ItemMissing     = "missing"
	ItemExpired     = "expired"
	ItemExpiring    = "expiring"
	ItemUnactioned  = "unactioned"
	ItemOverdue     = "overdue"
	ItemDueNow      = "due_now"
	ItemDueSoon     = "due_soon"
	ItemStalled     = "stalled"
	ItemWarnings    = "warnings"
	ItemAbsent      = "absent"
	ItemPending     = "pending"
	ItemNoAppointee = "no_appointee"
)

var ItemStates = []string{ItemMissing, ItemExpired, ItemExpiring, ItemUnactioned, ItemOverdue, ItemDueNow, ItemDueSoon,
	ItemStalled, ItemWarnings, ItemAbsent, ItemPending, ItemNoAppointee}

// Item is one thing the summary lists, linked to its record.
type Item struct {
	SubjectType string  `json:"subject_type"`
	SubjectID   *string `json:"subject_id"`
	State       string  `json:"state"`
	// red or amber; null in a category that isn't counted.
	Colour *string `json:"colour"`
	Label  string  `json:"label"`
	// The ids the dashboard needs to open the record, where it has a parent:
	// a finding's inspection and item, a competency's worker, a deadline's
	// incident, a stalled close-out's action.
	InspectionID *string `json:"inspection_id"`
	ItemKey      *string `json:"item_key"`
	WorkerID     *string `json:"worker_id"`
	IncidentID   *string `json:"incident_id"`
	SiteWorkID   *string `json:"site_work_id"`
	ActionID     *string `json:"action_id"`
	TypeKey      *string `json:"type_key"`
	Severity     *string `json:"severity"`
	// Overdue actions: 1-3, as the reminders escalate (M11 D23).
	EscalationStep *int `json:"escalation_step"`
	// The date the item turns on (an expiry, a due date), or the time it
	// started waiting (a stalled verification, a pending upload).
	DueOn    *string    `json:"due_on"`
	Since    *time.Time `json:"since"`
	Warnings []string   `json:"warnings"`
	// Why the colour may not apply (a required appointment, D4).
	Note *string `json:"note"`
}

// Category: counted categories have a colour; shown ones (absent workers,
// pending uploads) have none.
type Category struct {
	Key     string  `json:"key"`
	Counted bool    `json:"counted"`
	Status  *string `json:"status"`
	Count   int     `json:"count"`
	Items   []Item  `json:"items"`
}

type SiteSummary struct {
	SiteID     string     `json:"site_id"`
	SiteName   string     `json:"site_name"`
	SiteStatus string     `json:"site_status"`
	RegimeCode string     `json:"regime_code"`
	TimeZone   string     `json:"time_zone"`
	Date       string     `json:"date"`
	ComputedAt time.Time  `json:"computed_at"`
	Status     string     `json:"status"`
	RedItems   int        `json:"red_items"`
	AmberItems int        `json:"amber_items"`
	Categories []Category `json:"categories"`
}

// SiteRank is a site's line in the rollup: its counts without the items.
type SiteRank struct {
	SiteID     string            `json:"site_id"`
	SiteName   string            `json:"site_name"`
	SiteStatus string            `json:"site_status"`
	Status     string            `json:"status"`
	RedItems   int               `json:"red_items"`
	AmberItems int               `json:"amber_items"`
	Counts     map[string]int    `json:"counts"`
	Statuses   map[string]string `json:"statuses"`
}

// Rollup: the organisation's sites, worst first (D3), and the items that
// belong to no site, once (expired vault documents; for owners and SHE
// officers, document files pending with no site).
type Rollup struct {
	ComputedAt time.Time  `json:"computed_at"`
	OrgItems   []Category `json:"org_items"`
	Sites      []SiteRank `json:"sites"`
}

// SiteContext is what a Contributor gets for one site.
type SiteContext struct {
	OrgID, UserID, Role string
	Site                *sites.Site
	Regime              *regimes.DataSet
	Now                 time.Time
	Today               string
}

// Contributor is implemented by an edition with categories of its own
// (construction: the H&S file and appointments). Each category's items
// carry their colours; the summary counts them.
type Contributor interface {
	ComplianceCategories(ctx context.Context, sc SiteContext) ([]Category, error)
}

var ErrSiteNotFound = errors.New("site not found")
