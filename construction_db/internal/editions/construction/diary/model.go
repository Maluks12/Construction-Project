// Package diary is the Construction edition's daily site diary (Rev 4 §3.9,
// §5.2): one per site per local day, closed at midnight, with dated addenda
// after that. Decisions D4–D10 are in construction_db/CLAUDE.md; endpoints
// in docs/api/diary.md. Migrations 000021 and 000022 enforce every rule.
package diary

import (
	"errors"
	"time"
)

// GraceAfterClose: a revision saved on the device before midnight still
// counts as the original if it reaches the server within this long of
// midnight (D6). It mirrors site_diary_grace() (000021); a test compares them.
const GraceAfterClose = 48 * time.Hour

// Weather conditions, as 000021's CHECK lists them.
var Conditions = []string{"fine", "cloudy", "rain", "heavy_rain", "thunderstorm", "wind", "hot", "cold", "mist"}

// Plant states.
var PlantStates = []string{"working", "idle", "broken_down"}

// How an instruction was given.
var InstructionForms = []string{"verbal", "written"}

// Delivery statuses.
var DeliveryStatuses = []string{"received", "expected_not_received"}

// Weather is required: at least one condition.
type Weather struct {
	Conditions []string `json:"conditions"`
	Note       *string  `json:"note"`
}

// Sections are the diary's optional parts; any can be left out. Each row has
// an id the device chose, kept across revisions, so later records (JBCC
// delay events and contract instructions, the principal-contractor tier's
// firms) can point at it.
type Sections struct {
	WeatherTimes *WeatherTimes `json:"weather_times,omitempty"`
	Labour       []LabourRow   `json:"labour,omitempty"`
	Plant        []PlantRow    `json:"plant,omitempty"`
	Instructions []Instruction `json:"instructions,omitempty"`
	Delays       []Delay       `json:"delays,omitempty"`
	Visitors     []Visitor     `json:"visitors,omitempty"`
	Deliveries   []Delivery    `json:"deliveries,omitempty"`
}

// WeatherTimes records weather by its effect (Rev 4 §3.9).
type WeatherTimes struct {
	RainStartedAt      *time.Time `json:"rain_started_at"`
	RainStoppedAt      *time.Time `json:"rain_stopped_at"`
	WorkStoppedAt      *time.Time `json:"work_stopped_at"`
	WorkRestartedAt    *time.Time `json:"work_restarted_at"`
	RainGaugeMM        *float64   `json:"rain_gauge_mm"`
	HoursLost          *float64   `json:"hours_lost"`
	ActivitiesAffected *string    `json:"activities_affected"`
	CriticalPath       *bool      `json:"critical_path"`
	DelayedEffect      *bool      `json:"delayed_effect"`
}

// LabourRow is a firm's headcount; own_team marks the organisation's own.
type LabourRow struct {
	ID        string  `json:"id"`
	Firm      string  `json:"firm"`
	OwnTeam   bool    `json:"own_team"`
	Trade     *string `json:"trade"`
	Headcount int     `json:"headcount"`
	// The register workers counted in this row (M11 D8), optional: each
	// once, at most headcount. Old revisions have none.
	WorkerIDs []string `json:"worker_ids,omitempty"`
}

type PlantRow struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Ref           *string  `json:"ref"`
	State         string   `json:"state"`
	HoursWorking  *float64 `json:"hours_working"`
	HoursStanding *float64 `json:"hours_standing"`
}

// Instruction is one received on site. It is a record of what was said or
// written, never a contract instruction by itself (D8).
type Instruction struct {
	ID          string    `json:"id"`
	FromName    string    `json:"from_name"`
	FromRole    *string   `json:"from_role"`
	ReceivedAt  time.Time `json:"received_at"`
	Form        string    `json:"form"`
	Description string    `json:"description"`
	Reference   *string   `json:"reference"`
}

type Delay struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	StartedAt   *time.Time `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	HoursLost   *float64   `json:"hours_lost"`
	Cause       *string    `json:"cause"`
}

type Visitor struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Organisation *string    `json:"organisation"`
	Purpose      *string    `json:"purpose"`
	ArrivedAt    *time.Time `json:"arrived_at"`
	LeftAt       *time.Time `json:"left_at"`
}

type Delivery struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Supplier    *string    `json:"supplier"`
	Quantity    *string    `json:"quantity"`
	Status      string     `json:"status"`
	ReceivedAt  *time.Time `json:"received_at"`
}

// RevisionRequest restates the whole diary. weather, headcount and
// work_done are required; nothing else is.
type RevisionRequest struct {
	// The revision this phone last saw; null for a diary written from scratch.
	BaseRevisionID *string  `json:"base_revision_id"`
	Weather        *Weather `json:"weather"`
	// The headcount the supervisor entered (required; 0 is a headcount).
	Headcount   *int      `json:"headcount"`
	WorkDone    string    `json:"work_done"`
	Sections    Sections  `json:"sections"`
	EvidenceIDs []string  `json:"evidence_ids"`
	ClientSaved time.Time `json:"client_saved_at"`
	// Required exactly when client_saved_at is after the day closed (a late
	// diary, for a day that has no revision yet).
	LateReason *string `json:"late_reason"`
	DeviceID   *string `json:"device_id"`
}

// AddendumRequest adds to a closed diary: a reason, and at least one of a
// note, sections or photos.
type AddendumRequest struct {
	Reason      string    `json:"reason"`
	Note        *string   `json:"note"`
	Sections    Sections  `json:"sections"`
	EvidenceIDs []string  `json:"evidence_ids"`
	ClientAdded time.Time `json:"client_added_at"`
	DeviceID    *string   `json:"device_id"`
}

type Revision struct {
	ID                 string    `json:"id"`
	SiteID             string    `json:"site_id"`
	DiaryDate          string    `json:"diary_date"`
	Seq                int       `json:"seq"`
	BaseRevisionID     *string   `json:"base_revision_id"`
	Weather            Weather   `json:"weather"`
	Headcount          int       `json:"headcount"`
	HeadcountSuggested int       `json:"headcount_suggested"`
	WorkDone           string    `json:"work_done"`
	Sections           Sections  `json:"sections"`
	EvidenceIDs        []string  `json:"evidence_ids"`
	SavedBy            string    `json:"saved_by"`
	ClientSavedAt      time.Time `json:"client_saved_at"`
	ServerReceivedAt   time.Time `json:"server_received_at"`
	SavedAfterClose    bool      `json:"saved_after_close"`
	ReceivedAfterClose bool      `json:"received_after_close"`
	LateReason         *string   `json:"late_reason"`
	DeviceID           *string   `json:"device_id"`
}

type Addendum struct {
	ID               string    `json:"id"`
	SiteID           string    `json:"site_id"`
	DiaryDate        string    `json:"diary_date"`
	Reason           string    `json:"reason"`
	Note             *string   `json:"note"`
	Sections         Sections  `json:"sections"`
	EvidenceIDs      []string  `json:"evidence_ids"`
	AddedBy          string    `json:"added_by"`
	ClientAddedAt    time.Time `json:"client_added_at"`
	ServerReceivedAt time.Time `json:"server_received_at"`
	DeviceID         *string   `json:"device_id"`
}

// DiaryHeader is what the register and a full diary share.
type DiaryHeader struct {
	SiteID    string `json:"site_id"`
	DiaryDate string `json:"diary_date"`
	TimeZone  string `json:"time_zone"`
	// The local day; saves made on the device before closes_at count until
	// grace_ends_at.
	OpensAt     time.Time `json:"opens_at"`
	ClosesAt    time.Time `json:"closes_at"`
	GraceEndsAt time.Time `json:"grace_ends_at"`
	// closes_at has passed (server time).
	Closed bool `json:"closed"`
	// Written after its day (a late diary).
	Late          bool      `json:"late"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	RevisionCount int       `json:"revision_count"`
	// Revisions saved without seeing each other; empty when there's none
	// (D5). GET …/revisions shows them side by side.
	ConflictRevisionIDs []string `json:"conflict_revision_ids"`
	AddendumCount       int      `json:"addendum_count"`
}

type DiarySummary struct {
	DiaryHeader
	HeadRevisionID     string  `json:"head_revision_id"`
	Weather            Weather `json:"weather"`
	Headcount          int     `json:"headcount"`
	HeadcountSuggested int     `json:"headcount_suggested"`
}

type Diary struct {
	DiaryHeader
	// The current revision: the newest by device time.
	Current Revision `json:"current"`
	// site_headcount for the day now (sign-ins and talk signatures, M11);
	// current.headcount_suggested is what
	// it was when that revision arrived.
	HeadcountSuggestedNow int        `json:"headcount_suggested_now"`
	Addenda               []Addendum `json:"addenda"`
	// That day's toolbox talks and incidents, linked by date, not copied.
	ToolboxTalkIDs []string `json:"toolbox_talk_ids"`
	IncidentIDs    []string `json:"incident_ids"`
}

var (
	ErrDiaryNotFound    = errors.New("site diary not found")
	ErrBadDate          = errors.New("date must be YYYY-MM-DD")
	ErrDayNotBegun      = errors.New("the diary's day hasn't begun")
	ErrDayAfterArchive  = errors.New("the site was archived before this day")
	ErrBadRevisionID    = errors.New("revision id must be a UUID")
	ErrRevisionIDReused = errors.New("revision id already used with different details")
	ErrBadBase          = errors.New("base_revision_id must be a UUID")
	ErrUnknownBase      = errors.New("base_revision_id is not a revision of this diary")
	ErrNoWeather        = errors.New("weather is required, with at least one condition")
	ErrBadCondition     = errors.New("weather conditions must be fine, cloudy, rain, heavy_rain, thunderstorm, wind, hot, cold or mist, each once")
	ErrBadWeatherNote   = errors.New("weather note must be 1-500 characters")
	ErrNoHeadcount      = errors.New("headcount is required (0-10000)")
	ErrBadWorkDone      = errors.New("work_done must be 1-4000 characters")
	ErrNoSavedAt        = errors.New("client_saved_at is required")
	ErrSavedInFuture    = errors.New("client_saved_at is in the future; check the device clock")
	ErrSavedBeforeDay   = errors.New("client_saved_at is before the diary's day")
	ErrLateReason       = errors.New("a diary saved after its day closed is a late diary and needs late_reason (1-500 characters)")
	ErrNotLate          = errors.New("late_reason is only for a diary saved after its day closed")
	ErrDiaryClosed      = errors.New("site diary closed at midnight; add an addendum")
	ErrPastGrace        = errors.New("saved before midnight but received more than 48 hours after; add an addendum")
	ErrBadDeviceID      = errors.New("device_id must be 1-200 bytes")

	ErrBadAddendumID    = errors.New("addendum id must be a UUID")
	ErrAddendumIDReused = errors.New("addendum id already used with different details")
	ErrBadReason        = errors.New("reason must be 1-500 characters")
	ErrBadNote          = errors.New("note must be 1-4000 characters")
	ErrAddsNothing      = errors.New("an addendum needs a note, a section or a photo")
	ErrNoAddedAt        = errors.New("client_added_at is required")
	ErrAddedInFuture    = errors.New("client_added_at is in the future; check the device clock")
	ErrDiaryOpen        = errors.New("site diary is still open; save a revision instead")

	ErrBadEvidenceIDs      = errors.New("evidence_ids must be UUIDs, each once, at most 50")
	ErrEvidenceNotFound    = errors.New("evidence not found on this site")
	ErrEvidenceRejected    = errors.New("evidence was rejected")
	ErrEvidenceNotVerified = errors.New("evidence not verified yet")

	// Section errors carry the section and row (SectionError).
	ErrBadRowID       = errors.New("row id must be a UUID, used once in its section")
	ErrBadText        = errors.New("text is required and must be 1-500 characters (descriptions 1-2000)")
	ErrBadNumber      = errors.New("number out of range (hours 0-24, rain gauge 0-1000 mm, headcount 0-10000)")
	ErrBadChoice      = errors.New("value is not one of the allowed choices")
	ErrBadTime        = errors.New("time must be on the diary's day")
	ErrTwoOwnTeams    = errors.New("only one labour row can be own_team")
	ErrTooManyRows    = errors.New("at most 200 rows in a section")
	ErrBadWorkerIDs   = errors.New("worker_ids must be register workers, each once, at most the row's headcount")
	ErrMissingTime    = errors.New("received_at is required")
	ErrEndBeforeStart = errors.New("an end time must not be before its start")

	ErrBadRange = errors.New("from and to must be YYYY-MM-DD, from not after to")
	ErrBadLimit = errors.New("limit must be 1-200")
)

// SectionError says which row of which section a request got wrong.
type SectionError struct {
	Err     error
	Section string
	Row     int // 0-based; -1 for the section as a whole
	Field   string
}

func (e *SectionError) Error() string { return e.Err.Error() }
func (e *SectionError) Unwrap() error { return e.Err }

// EvidenceError names the photos a request can't use.
type EvidenceError struct {
	Err error
	IDs []string
}

func (e *EvidenceError) Error() string { return e.Err.Error() }
func (e *EvidenceError) Unwrap() error { return e.Err }
