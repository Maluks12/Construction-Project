// Package attendance is daily site sign-in and sign-out (M11 D11–D18):
// every sign-in is a register worker, tapped on a phone or a site tablet or
// copied from a photographed paper sheet; the day's register with who was
// expected (the site roster) and didn't sign in. Endpoints are in
// docs/api/attendance.md; migration 000026 enforces every rule.
package attendance

import (
	"errors"
	"time"

	"construction_db/internal/core/workers"
)

const (
	MethodTap   = "tap"
	MethodPaper = "paper"
)

var Methods = []string{MethodTap, MethodPaper}

// OpenWindow: a sign-in without a sign-out stands this long, and a sign-out
// can be recorded at most this long after it. Mirrors site_sign_in_window()
// (000026).
const OpenWindow = 16 * time.Hour

// SignInRequest signs a worker in under an id the device chose. Either
// worker_id is a register worker (a tap on their name), or new_worker is
// given with a new device-chosen worker_id: the server adds them to the
// register, or signs in the worker already there with the same name, firm
// and clock number (matched_existing).
type SignInRequest struct {
	WorkerID         string                 `json:"worker_id"`
	NewWorker        *workers.WorkerRequest `json:"new_worker"`
	Method           string                 `json:"method"`
	ClientSignedInAt time.Time              `json:"client_signed_in_at"`
	DeviceID         *string                `json:"device_id"`
}

type SignOutRequest struct {
	Method            string    `json:"method"`
	ClientSignedOutAt time.Time `json:"client_signed_out_at"`
}

type RetractRequest struct {
	Reason string `json:"reason"`
}

type SheetRequest struct{}

type SignIn struct {
	ID          string  `json:"id"`
	SiteID      string  `json:"site_id"`
	WorkerID    string  `json:"worker_id"`
	Name        string  `json:"name"`
	Firm        *string `json:"firm"`
	ClockNumber *string `json:"clock_number"`
	Method      string  `json:"method"`
	// On-the-spot entry that matched a worker already on the register: show
	// who, so the supervisor can say "different person" (retract, then sign
	// in a new worker with a clock number).
	MatchedExisting    bool        `json:"matched_existing"`
	RecordedBy         string      `json:"recorded_by"`
	ClientSignedInAt   time.Time   `json:"client_signed_in_at"`
	ReceivedAt         time.Time   `json:"received_at"`
	SignInDate         string      `json:"sign_in_date"`
	TimeZone           string      `json:"time_zone"`
	ReceivedAfterClose bool        `json:"received_after_close"`
	DeviceID           *string     `json:"device_id"`
	SignOut            *SignOut    `json:"sign_out"`
	Retraction         *Retraction `json:"retraction"`
	// No sign-out and more than 16 hours since the sign-in: none can be
	// recorded any more.
	Lapsed bool `json:"lapsed"`
	// Never a refusal (D18): no_valid_medical, medical_unfit, competency_expired.
	Warnings []string `json:"warnings"`
}

type SignOut struct {
	Method            string    `json:"method"`
	SignedOutBy       string    `json:"signed_out_by"`
	ClientSignedOutAt time.Time `json:"client_signed_out_at"`
	ReceivedAt        time.Time `json:"received_at"`
}

type Retraction struct {
	Reason      string    `json:"reason"`
	RetractedBy string    `json:"retracted_by"`
	RetractedAt time.Time `json:"retracted_at"`
}

type Sheet struct {
	SiteID     string    `json:"site_id"`
	SheetDate  string    `json:"sheet_date"`
	EvidenceID string    `json:"evidence_id"`
	AddedBy    string    `json:"added_by"`
	AddedAt    time.Time `json:"added_at"`
}

// Overlap: this site's sign-in was standing while the same worker was
// signed in on another site (D14). Allowed, shown.
type Overlap struct {
	SignInID      string `json:"sign_in_id"`
	OtherSiteID   string `json:"other_site_id"`
	OtherSignInID string `json:"other_sign_in_id"`
}

// Day is a site's sign-in register for one local day.
type Day struct {
	SiteID   string        `json:"site_id"`
	Date     string        `json:"date"`
	SignIns  []SignIn      `json:"sign_ins"`
	Sheets   []Sheet       `json:"sheets"`
	Absent   []RosterEntry `json:"absent"`
	Overlaps []Overlap     `json:"overlaps"`
	// Distinct people who signed in or signed a talk that day: the diary's
	// headcount_suggested_now.
	Headcount int `json:"headcount"`
}

type RosterEntry struct {
	ID           string     `json:"id"`
	WorkerID     string     `json:"worker_id"`
	Name         string     `json:"name"`
	Firm         *string    `json:"firm"`
	ClockNumber  *string    `json:"clock_number"`
	Active       bool       `json:"active"`
	AddedVia     string     `json:"added_via"`
	AddedBy      string     `json:"added_by"`
	AddedAt      time.Time  `json:"added_at"`
	RemovedBy    *string    `json:"removed_by"`
	RemovedAt    *time.Time `json:"removed_at"`
	RemoveReason *string    `json:"remove_reason"`
}

type RemoveRequest struct {
	Reason string `json:"reason"`
}

// AlreadySignedIn is the 409 for a double tap: the sign-in already standing.
type AlreadySignedIn struct {
	Error    string `json:"error"`
	SignInID string `json:"sign_in_id"`
}

var (
	ErrSignInNotFound      = errors.New("sign-in not found")
	ErrBadSignInID         = errors.New("sign-in id must be a UUID")
	ErrBadWorkerID         = errors.New("worker_id must be a UUID")
	ErrBadMethod           = errors.New("method must be tap or paper")
	ErrNoSignedInAt        = errors.New("client_signed_in_at is required")
	ErrSignedInFuture      = errors.New("client_signed_in_at is in the future; check the device clock")
	ErrNoSignedOutAt       = errors.New("client_signed_out_at is required")
	ErrSignedOutFuture     = errors.New("client_signed_out_at is in the future; check the device clock")
	ErrSignOutBeforeIn     = errors.New("signed out before signing in")
	ErrSignOutTooLate      = errors.New("signed out more than 16 hours after signing in")
	ErrSignOutAfterNext    = errors.New("signed out after this worker's next sign-in on the site")
	ErrBadDeviceID         = errors.New("device_id must be 1-200 bytes")
	ErrBadReason           = errors.New("reason must be 1-500 characters")
	ErrBadDate             = errors.New("date must be YYYY-MM-DD")
	ErrBadEvidenceID       = errors.New("evidence id must be a UUID")
	ErrIDReused            = errors.New("sign-in id already used with different details")
	ErrAlreadySignedIn     = errors.New("this worker is already signed in on this site at that time")
	ErrAlreadySignedOut    = errors.New("already signed out at another time")
	ErrAlreadyRetracted    = errors.New("sign-in already retracted with another reason")
	ErrRetracted           = errors.New("sign-in was retracted")
	ErrNeedsSheet          = errors.New("paper sign-in needs a photo of that day's signed sheet; add it first")
	ErrSiteArchived        = errors.New("the site was archived before this sign-in")
	ErrCantRetract         = errors.New("only the person who recorded a sign-in, an owner or a SHE officer can retract it")
	ErrEvidenceNotFound    = errors.New("evidence not found on this site")
	ErrEvidenceRejected    = errors.New("evidence was rejected")
	ErrEvidenceNotVerified = errors.New("evidence not verified yet")
	ErrNotOnRoster         = errors.New("worker is not on this site's roster")
)
