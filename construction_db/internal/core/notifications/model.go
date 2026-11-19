// Package notifications is reminders (M11 D22–D26): the in-app inbox
// (this package's API), the email outbox, and the jobs cmd/worker runs
// (jobs.go). Redis decides when a job runs; Postgres decides whether
// anything is sent: a reminder is unique per recipient, kind, subject and
// threshold (000029), so a restart or a second worker sends nothing twice.
package notifications

import (
	"errors"
	"time"
)

// Kinds, subject types and states as 000029 has them.
var (
	Kinds = []string{"incident_deadline", "action_overdue", "verification_stalled", "competency_expiry", "medical_expiry",
		"appointment_expiry", "document_expiry"}
	SubjectTypes = []string{"deadline_duty", "action", "closeout", "competency", "medical", "appointment", "document"}
)

const (
	// The subject still stands as it did when told.
	SubjectCurrent = "current"
	// Renewed, retracted, satisfied, closed or replaced since.
	SubjectSuperseded = "superseded"
)

var SubjectStates = []string{SubjectCurrent, SubjectSuperseded}

type Notification struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id"`
	Threshold   string     `json:"threshold"`
	SiteID      *string    `json:"site_id"`
	Title       string     `json:"title"`
	DueOn       *string    `json:"due_on"`
	DueAt       *time.Time `json:"due_at"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at"`
	// Whether what it was about still stands (a renewal, retraction,
	// satisfaction or close makes it superseded).
	SubjectState string `json:"subject_state"`
}

var (
	ErrNotFound  = errors.New("notification not found")
	ErrBadLimit  = errors.New("limit must be 1-200")
	ErrBadUnread = errors.New("unread must be true or false")
)
