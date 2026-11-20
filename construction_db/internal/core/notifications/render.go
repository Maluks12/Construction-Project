package notifications

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"construction_db/internal/regimes"
)

// Labels is notifications_due's p_labels: the regime's labels for the type
// keys titles name (competencies, appointments).
func Labels(ds *regimes.DataSet) string {
	labels := map[string]map[string]string{"competency": {}, "appointment": {}}
	for _, c := range ds.Workforce.CompetencyTypes {
		labels["competency"][c.Key] = c.Label
	}
	for _, a := range ds.Workforce.AppointmentTypes {
		labels["appointment"][a.Key] = a.Label
	}
	b, _ := json.Marshal(labels)
	return string(b)
}

// zone is where a reminder's dates are told: its regime's time zone
// (notification_regime, 000036). A regime that isn't loaded is a data error:
// it falls back to the first loaded regime, with a warning.
func (j *Jobs) zone(code *string) *time.Location {
	if code != nil {
		if ds, ok := j.Registry.Get(*code); ok {
			if loc, err := time.LoadLocation(ds.Regime.TimeZone); err == nil {
				return loc
			}
		}
	}
	slog.Warn("reminder email: regime not loaded; using the first regime's time zone", "regime", code)
	for _, c := range j.Registry.Codes() {
		ds, _ := j.Registry.Get(c)
		if loc, err := time.LoadLocation(ds.Regime.TimeZone); err == nil {
			return loc
		}
	}
	return time.UTC
}

// Render is a reminder's email: the title as the subject, and one plain
// sentence saying when (M11 review). No kind or threshold codes, and never
// a medical outcome (titles carry none either).
func Render(title, kind, threshold string, dueOn *string, dueAt *time.Time, now time.Time, loc *time.Location) (subject, body string) {
	subject = truncate("SiteProof: "+title, 200)
	today := dayOf(now.In(loc))
	var due time.Time
	var hasDay bool
	if dueOn != nil {
		if d, err := time.ParseInLocation(time.DateOnly, *dueOn, loc); err == nil {
			due, hasDay = d, true
		}
	}
	date := due.Format("2 Jan 2006")
	left := calendarDays(today, due)

	var sentence string
	switch {
	case kind == "integrity_alarm":
		sentence = title + ". An audit pack found a stored photo or file whose bytes no longer match the SHA-256 sealed " +
			"when it was received. The export was stopped. Tell SiteProof support straight away."
	case kind == "verification_stalled":
		sentence = title + ": a close-out has been waiting for verification for 2 days or more."
	case threshold == "forthwith":
		sentence = title + " must be done forthwith: now, not later."
	case kind == "action_overdue" && hasDay:
		sentence = title + " is overdue by " + days(-left) + " (it was due on " + date + ")."
	case kind == "incident_deadline" && hasDay && now.After(deadlineEnd(due)):
		sentence = title + " is overdue by " + days(-left) + " (it was due on " + date + ")."
	case kind == "incident_deadline" && hasDay:
		sentence = title + " is due " + when(left, date) + "."
	case kind == "incident_deadline" && dueAt != nil && now.After(*dueAt):
		sentence = title + " is overdue: it was due at " + dueAt.In(loc).Format("15:04 on 2 Jan 2006") + "."
	case kind == "incident_deadline" && dueAt != nil:
		sentence = title + " is due at " + dueAt.In(loc).Format("15:04 on 2 Jan 2006") + "."
	case hasDay && left < 0:
		sentence = title + " expired on " + date + ", " + days(-left) + " ago."
	case hasDay:
		sentence = title + " expires " + when(left, date) + "."
	default:
		sentence = title + "."
	}
	return subject, sentence + "\n\nOpen SiteProof to see it.\n"
}

// calendarDays counts dates, not 24-hour spans, so a daylight-saving change
// in between can't make it one short.
func calendarDays(from, to time.Time) int {
	a := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a) / (24 * time.Hour))
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// deadlineEnd: a day-based deadline is due by the end of its day.
func deadlineEnd(day time.Time) time.Time { return day.AddDate(0, 0, 1) }

func when(left int, date string) string {
	if left == 0 {
		return "today, " + date
	}
	return "on " + date + ", in " + days(left)
}

func days(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}
