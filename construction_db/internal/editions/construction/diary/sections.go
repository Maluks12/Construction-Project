package diary

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// day is a diary's local day: [opens, closes).
type day struct {
	opens, closes time.Time
}

// checkSections validates every optional section and normalises row ids and
// times (canonical UUIDs, microseconds) so a repeat compares equal.
func checkSections(s *Sections, d day) error {
	if w := s.WeatherTimes; w != nil {
		const sec = "weather_times"
		for _, f := range []struct {
			name string
			t    **time.Time
		}{{"rain_started_at", &w.RainStartedAt}, {"rain_stopped_at", &w.RainStoppedAt}, {"work_stopped_at", &w.WorkStoppedAt}, {"work_restarted_at", &w.WorkRestartedAt}} {
			if err := onDay(f.t, d); err != nil {
				return &SectionError{err, sec, -1, f.name}
			}
		}
		switch {
		case before(w.RainStoppedAt, w.RainStartedAt):
			return &SectionError{ErrEndBeforeStart, sec, -1, "rain_stopped_at"}
		case before(w.WorkRestartedAt, w.WorkStoppedAt):
			return &SectionError{ErrEndBeforeStart, sec, -1, "work_restarted_at"}
		case !between(w.RainGaugeMM, 0, 1000):
			return &SectionError{ErrBadNumber, sec, -1, "rain_gauge_mm"}
		case !between(w.HoursLost, 0, 24):
			return &SectionError{ErrBadNumber, sec, -1, "hours_lost"}
		case !optText(w.ActivitiesAffected, 2000):
			return &SectionError{ErrBadText, sec, -1, "activities_affected"}
		}
	}

	ids := map[string]map[string]bool{}
	rowID := func(sec string, i int, id *string) error {
		u, err := uuid.Parse(*id)
		if ids[sec] == nil {
			ids[sec] = map[string]bool{}
		}
		if err != nil || ids[sec][u.String()] {
			return &SectionError{ErrBadRowID, sec, i, "id"}
		}
		*id = u.String()
		ids[sec][*id] = true
		return nil
	}
	for _, n := range []struct {
		sec string
		n   int
	}{{"labour", len(s.Labour)}, {"plant", len(s.Plant)}, {"instructions", len(s.Instructions)}, {"delays", len(s.Delays)},
		{"visitors", len(s.Visitors)}, {"deliveries", len(s.Deliveries)}} {
		if n.n > 200 {
			return &SectionError{ErrTooManyRows, n.sec, -1, ""}
		}
	}

	own := false
	for i := range s.Labour {
		r := &s.Labour[i]
		switch err := rowID("labour", i, &r.ID); {
		case err != nil:
			return err
		case !text(r.Firm, 500):
			return &SectionError{ErrBadText, "labour", i, "firm"}
		case !optText(r.Trade, 500):
			return &SectionError{ErrBadText, "labour", i, "trade"}
		case r.Headcount < 0 || r.Headcount > 10000:
			return &SectionError{ErrBadNumber, "labour", i, "headcount"}
		case !workerIDsOK(r):
			return &SectionError{ErrBadWorkerIDs, "labour", i, "worker_ids"}
		case r.OwnTeam && own:
			return &SectionError{ErrTwoOwnTeams, "labour", i, "own_team"}
		}
		own = own || r.OwnTeam
	}
	for i := range s.Plant {
		r := &s.Plant[i]
		switch err := rowID("plant", i, &r.ID); {
		case err != nil:
			return err
		case !text(r.Description, 2000):
			return &SectionError{ErrBadText, "plant", i, "description"}
		case !optText(r.Ref, 500):
			return &SectionError{ErrBadText, "plant", i, "ref"}
		case !slices.Contains(PlantStates, r.State):
			return &SectionError{ErrBadChoice, "plant", i, "state"}
		case !between(r.HoursWorking, 0, 24):
			return &SectionError{ErrBadNumber, "plant", i, "hours_working"}
		case !between(r.HoursStanding, 0, 24):
			return &SectionError{ErrBadNumber, "plant", i, "hours_standing"}
		}
	}
	for i := range s.Instructions {
		r := &s.Instructions[i]
		switch err := rowID("instructions", i, &r.ID); {
		case err != nil:
			return err
		case !text(r.FromName, 500):
			return &SectionError{ErrBadText, "instructions", i, "from_name"}
		case !optText(r.FromRole, 500):
			return &SectionError{ErrBadText, "instructions", i, "from_role"}
		case r.ReceivedAt.IsZero():
			return &SectionError{ErrMissingTime, "instructions", i, "received_at"}
		case !onDayValue(&r.ReceivedAt, d):
			return &SectionError{ErrBadTime, "instructions", i, "received_at"}
		case !slices.Contains(InstructionForms, r.Form):
			return &SectionError{ErrBadChoice, "instructions", i, "form"}
		case !text(r.Description, 2000):
			return &SectionError{ErrBadText, "instructions", i, "description"}
		case !optText(r.Reference, 500):
			return &SectionError{ErrBadText, "instructions", i, "reference"}
		}
	}
	for i := range s.Delays {
		r := &s.Delays[i]
		switch err := rowID("delays", i, &r.ID); {
		case err != nil:
			return err
		case !text(r.Description, 2000):
			return &SectionError{ErrBadText, "delays", i, "description"}
		case onDay(&r.StartedAt, d) != nil:
			return &SectionError{ErrBadTime, "delays", i, "started_at"}
		case onDay(&r.EndedAt, d) != nil:
			return &SectionError{ErrBadTime, "delays", i, "ended_at"}
		case before(r.EndedAt, r.StartedAt):
			return &SectionError{ErrEndBeforeStart, "delays", i, "ended_at"}
		case !between(r.HoursLost, 0, 24):
			return &SectionError{ErrBadNumber, "delays", i, "hours_lost"}
		case !optText(r.Cause, 500):
			return &SectionError{ErrBadText, "delays", i, "cause"}
		}
	}
	for i := range s.Visitors {
		r := &s.Visitors[i]
		switch err := rowID("visitors", i, &r.ID); {
		case err != nil:
			return err
		case !text(r.Name, 500):
			return &SectionError{ErrBadText, "visitors", i, "name"}
		case !optText(r.Organisation, 500):
			return &SectionError{ErrBadText, "visitors", i, "organisation"}
		case !optText(r.Purpose, 500):
			return &SectionError{ErrBadText, "visitors", i, "purpose"}
		case onDay(&r.ArrivedAt, d) != nil:
			return &SectionError{ErrBadTime, "visitors", i, "arrived_at"}
		case onDay(&r.LeftAt, d) != nil:
			return &SectionError{ErrBadTime, "visitors", i, "left_at"}
		case before(r.LeftAt, r.ArrivedAt):
			return &SectionError{ErrEndBeforeStart, "visitors", i, "left_at"}
		}
	}
	for i := range s.Deliveries {
		r := &s.Deliveries[i]
		switch err := rowID("deliveries", i, &r.ID); {
		case err != nil:
			return err
		case !text(r.Description, 2000):
			return &SectionError{ErrBadText, "deliveries", i, "description"}
		case !optText(r.Supplier, 500):
			return &SectionError{ErrBadText, "deliveries", i, "supplier"}
		case !optText(r.Quantity, 500):
			return &SectionError{ErrBadText, "deliveries", i, "quantity"}
		case !slices.Contains(DeliveryStatuses, r.Status):
			return &SectionError{ErrBadChoice, "deliveries", i, "status"}
		case onDay(&r.ReceivedAt, d) != nil:
			return &SectionError{ErrBadTime, "deliveries", i, "received_at"}
		}
	}
	return nil
}

// empty: no section has anything in it.
func (s Sections) empty() bool {
	return s.WeatherTimes == nil && len(s.Labour) == 0 && len(s.Plant) == 0 && len(s.Instructions) == 0 &&
		len(s.Delays) == 0 && len(s.Visitors) == 0 && len(s.Deliveries) == 0
}

// onDay: absent, or on the diary's local day; truncated to microseconds.
func onDay(t **time.Time, d day) error {
	if *t == nil {
		return nil
	}
	v := micro(**t)
	if v.Before(d.opens) || !v.Before(d.closes) {
		return ErrBadTime
	}
	*t = &v
	return nil
}

// onDayValue is onDay for a required time.
func onDayValue(t *time.Time, d day) bool {
	v := micro(*t)
	if v.Before(d.opens) || !v.Before(d.closes) {
		return false
	}
	*t = v
	return true
}

func before(end, start *time.Time) bool { return end != nil && start != nil && end.Before(*start) }

func between(f *float64, lo, hi float64) bool { return f == nil || (*f >= lo && *f <= hi) }

// text: 1 to max characters of valid UTF-8 without NUL, not only spaces.
func text(s string, max int) bool {
	n := utf8.RuneCountInString(s)
	return utf8.ValidString(s) && n >= 1 && n <= max && !strings.ContainsRune(s, 0) && strings.TrimSpace(s) != ""
}

func optText(s *string, max int) bool { return s == nil || text(*s, max) }

func micro(t time.Time) time.Time { return t.Truncate(time.Microsecond).UTC() }

// workerIDsOK: canonical UUIDs, each once, no more than the row's headcount
// (M11 D8). Whether they are register workers is checked in the transaction.
func workerIDsOK(r *LabourRow) bool {
	if len(r.WorkerIDs) > r.Headcount {
		return false
	}
	seen := map[string]bool{}
	for i, s := range r.WorkerIDs {
		id, err := uuid.Parse(s)
		if err != nil || seen[id.String()] {
			return false
		}
		r.WorkerIDs[i] = id.String()
		seen[id.String()] = true
	}
	return true
}
