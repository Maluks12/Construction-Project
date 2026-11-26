// Package toolbox is toolbox talks with attendance (Rev 3 §2.3.4, Rev 4
// A.5): a talk's topic (from the site regime's list, or free text), who gave
// it and when, photos, and the people who signed. Decisions D1–D3 and D10 are
// in construction_db/CLAUDE.md; endpoints in docs/api/toolbox.md. Migration
// 000020 enforces every rule.
package toolbox

import (
	"errors"
	"time"
)

// Signature methods (D2).
const (
	MethodTap   = "tap"   // the worker tapped their name on the recorder's phone
	MethodPaper = "paper" // transcribed from a photographed signed register
)

var Methods = []string{MethodTap, MethodPaper}

// Photo kinds.
const (
	KindPhoto         = "photo"
	KindPaperRegister = "paper_register" // a signed paper register: needed for paper attendance
)

var Kinds = []string{KindPhoto, KindPaperRegister}

// SigningWindow: a talk with no end can be signed this long after it started,
// and no talk lasts longer. It mirrors toolbox_signing_window() (000023).
const SigningWindow = 12 * time.Hour

// AlreadySignedHint goes with ErrAlreadySigned in the 409 body.
const AlreadySignedHint = "a different person with the same name should add their firm or clock number"

// TalkRequest creates a talk under an id the device chose, or edits it. Until
// someone signs, every field can change; after that only ended_at can be set,
// once.
type TalkRequest struct {
	// A key of GET …/toolbox-topics. Optional: without it, topic is free text.
	TopicKey *string `json:"topic_key"`
	// Defaults to the topic's title when topic_key is given.
	Topic         *string    `json:"topic"`
	PresenterName string     `json:"presenter_name"`
	Notes         *string    `json:"notes"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	DeviceID      *string    `json:"device_id"`
	// The talk's updated_at as the editor last read it (created_at if never
	// edited). An edit of a talk changed since is 409 with the current talk
	// (SYNC conflict rule). Optional: without it, the last edit wins.
	BaseUpdatedAt *time.Time `json:"base_updated_at"`
}

// Changed is the 409 for an edit based on an older copy.
type Changed struct {
	Current *Talk
}

func (e *Changed) Error() string { return ErrChanged.Error() }
func (e *Changed) Unwrap() error { return ErrChanged }

// PhotoRequest adds a verified photo of the talk's site to the talk.
type PhotoRequest struct {
	Kind string `json:"kind"`
}

// SignRequest records one person's signature under an id the device chose.
// The caller is the recorder: the account holding the phone.
type SignRequest struct {
	Name           string    `json:"name"`
	Firm           *string   `json:"firm"`
	ClockNumber    *string   `json:"clock_number"`
	Method         string    `json:"method"`
	ClientSignedAt time.Time `json:"client_signed_at"`
	DeviceID       *string   `json:"device_id"`
}

// RetractRequest withdraws a signature: the recorder or a lead, with a
// reason. The signature stays, marked retracted.
type RetractRequest struct {
	Reason            string    `json:"reason"`
	ClientRetractedAt time.Time `json:"client_retracted_at"`
}

// TalkHeader is what a list and a full talk share.
type TalkHeader struct {
	ID            string     `json:"id"`
	SiteID        string     `json:"site_id"`
	TopicKey      *string    `json:"topic_key"`
	Topic         string     `json:"topic"`
	PresenterName string     `json:"presenter_name"`
	Notes         *string    `json:"notes"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	// started_at's date in time_zone (the site regime's): the diary's day.
	TalkDate   string     `json:"talk_date"`
	TimeZone   string     `json:"time_zone"`
	RecordedBy string     `json:"recorded_by"`
	DeviceID   *string    `json:"device_id"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  *time.Time `json:"updated_at"`
	// People who signed and weren't retracted.
	SignedCount int `json:"signed_count"`
}

type TalkSummary struct {
	TalkHeader
	PhotoCount int `json:"photo_count"`
}

type Talk struct {
	TalkHeader
	Photos     []TalkPhoto  `json:"photos"`
	Attendance []Attendance `json:"attendance"`
	// Workers signed in on the site on the talk's day before it ended (or
	// now, while open) who didn't sign it: absent or refused (M11 D17).
	NotSigned []NotSigned `json:"not_signed"`
}

// TalkPhoto is a photo's id only; viewing it goes through the logged
// evidence view.
type TalkPhoto struct {
	EvidenceID string    `json:"evidence_id"`
	Kind       string    `json:"kind"`
	AddedBy    string    `json:"added_by"`
	AddedAt    time.Time `json:"added_at"`
}

type Attendance struct {
	ID             string      `json:"id"`
	TalkID         string      `json:"talk_id"`
	Name           string      `json:"name"`
	Firm           *string     `json:"firm"`
	ClockNumber    *string     `json:"clock_number"`
	Method         string      `json:"method"`
	RecordedBy     string      `json:"recorded_by"`
	ClientSignedAt time.Time   `json:"client_signed_at"`
	SignedAt       time.Time   `json:"signed_at"`
	DeviceID       *string     `json:"device_id"`
	Retraction     *Retraction `json:"retraction"`
	// The register worker this signature counts as (M11 D7): its link, or
	// the worker with the same name, firm and clock number. null otherwise.
	WorkerID *string `json:"worker_id"`
}

type Retraction struct {
	Reason            string    `json:"reason"`
	RetractedBy       string    `json:"retracted_by"`
	ClientRetractedAt time.Time `json:"client_retracted_at"`
	RetractedAt       time.Time `json:"retracted_at"`
}

var (
	ErrTalkNotFound       = errors.New("toolbox talk not found")
	ErrAttendanceNotFound = errors.New("attendance not found")

	ErrBadTalkID         = errors.New("talk id must be a UUID")
	ErrTalkIDReused      = errors.New("talk id already used on another site")
	ErrUnknownTopic      = errors.New("topic_key is not a topic of this site's regime")
	ErrTopicRequired     = errors.New("topic is required without a topic_key")
	ErrBadTopic          = errors.New("topic must be 1-200 characters")
	ErrBadPresenter      = errors.New("presenter_name must be 1-200 characters")
	ErrBadNotes          = errors.New("notes must be 1-4000 characters")
	ErrNoStartedAt       = errors.New("started_at is required")
	ErrStartedInFuture   = errors.New("started_at is in the future; check the device clock")
	ErrEndedInFuture     = errors.New("ended_at is in the future; check the device clock")
	ErrEndsBeforeStart   = errors.New("ended_at must not be before started_at")
	ErrEndsTooLate       = errors.New("ended_at must be at most 12 hours after started_at")
	ErrEndsBeforeSigned  = errors.New("ended_at is before a signature already given")
	ErrBadDeviceID       = errors.New("device_id must be 1-200 bytes")
	ErrTalkSigned        = errors.New("people have signed this talk; only its end time can be set, once")
	ErrChanged           = errors.New("talk changed since base_updated_at; get it and edit again")
	ErrStartedAfterClose = errors.New("the site was archived before this talk started")

	ErrBadEvidenceID       = errors.New("evidence id must be a UUID")
	ErrBadKind             = errors.New("kind must be photo or paper_register")
	ErrEvidenceNotFound    = errors.New("evidence not found on this site")
	ErrEvidenceRejected    = errors.New("evidence was rejected")
	ErrEvidenceNotVerified = errors.New("evidence not verified yet")
	ErrPhotoKindDiffers    = errors.New("photo already added to this talk as another kind")

	ErrBadAttendanceID    = errors.New("attendance id must be a UUID")
	ErrAttendanceIDReused = errors.New("attendance id already used with different details")
	ErrBadName            = errors.New("name must be 1-200 characters")
	ErrBadFirm            = errors.New("firm must be 1-200 characters")
	ErrBadClockNumber     = errors.New("clock_number must be 1-50 characters")
	ErrBadMethod          = errors.New("method must be tap or paper")
	ErrNoSignedAt         = errors.New("client_signed_at is required")
	ErrSignedInFuture     = errors.New("client_signed_at is in the future; check the device clock")
	ErrSignedBeforeStart  = errors.New("signed before the talk started")
	ErrSignedAfterEnd     = errors.New("signed after the talk ended")
	ErrSignedTooLate      = errors.New("signed more than 12 hours after the talk started")
	ErrAlreadySigned      = errors.New("this person has already signed this talk")
	ErrNeedsRegisterPhoto = errors.New("paper attendance needs a photo of the signed register; add it first")
	ErrBadReason          = errors.New("reason must be 1-500 characters")
	ErrNoRetractedAt      = errors.New("client_retracted_at is required")
	ErrRetractedInFuture  = errors.New("client_retracted_at is in the future; check the device clock")
	ErrCantRetract        = errors.New("only the person who recorded a signature, an owner or a SHE officer can retract it")
	ErrAlreadyRetracted   = errors.New("signature already retracted with another reason")
	ErrBadDate            = errors.New("date must be YYYY-MM-DD")
	ErrBadLimit           = errors.New("limit must be 1-200")
)

// NotSigned is a worker signed in on site who hasn't signed the talk. Read
// only: signatures are never inferred.
type NotSigned struct {
	WorkerID    string  `json:"worker_id"`
	Name        string  `json:"name"`
	Firm        *string `json:"firm"`
	ClockNumber *string `json:"clock_number"`
	SignInID    string  `json:"sign_in_id"`
}
