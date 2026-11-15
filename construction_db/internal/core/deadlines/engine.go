package deadlines

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"construction_db/internal/regimes"
)

// Person is what the engine needs to know about someone in an incident.
type Person struct {
	EmployeeOfOrg bool
	Injured       bool
	DiedAt        *time.Time
}

// Facts are what one version of an incident says.
type Facts struct {
	TaxonomyCodes   []string
	OccurredAt      time.Time
	EmployerAwareAt *time.Time
	People          []Person
}

// Duty is an obligation already stored for the subject: its rule, its live
// computation (nil if withdrawn), and when it was satisfied (nil if open).
type Duty struct {
	ID          string
	RuleKey     string
	Live        *LiveDeadline
	SatisfiedAt *time.Time
}

type LiveDeadline struct {
	ID      string
	BasisAt time.Time
}

// Clock is one rule that applies, timed from BasisAt.
type Clock struct {
	Rule    regimes.DeadlineRule
	BasisAt time.Time
}

var (
	ErrUnknownType            = errors.New("unknown incident type")
	ErrEmployerAwareRequired  = errors.New("employer_aware_at is required when an employee of the organisation is injured")
	errRuleWithoutApplication = errors.New("deadline rule has an applies_when the engine doesn't know")
)

// Applicable returns the clocks a version starts, in the order the regime
// lists its rules. Which rules apply is regime data (DeadlineRule.AppliesWhen),
// so core never names a rule:
//   - incident_type: listed by any of the version's types (the union);
//   - employee_injured: someone is an employee of the organisation and
//     injured or dead, timed from employer_aware_at;
//   - person_died_after_notice: someone died after one of AfterSatisfied was
//     met, timed from the earliest such death;
//   - site_work: never for an incident.
//
// duties gives the satisfactions person_died_after_notice looks at.
func Applicable(ds *regimes.DataSet, f Facts, duties []Duty) ([]Clock, error) {
	listed := map[string]bool{}
	for _, code := range f.TaxonomyCodes {
		i := slices.IndexFunc(ds.Taxonomy, func(t regimes.IncidentType) bool { return t.Key == code })
		if i < 0 {
			return nil, fmt.Errorf("%w: %s", ErrUnknownType, code)
		}
		for _, r := range ds.Taxonomy[i].DeadlineRules {
			listed[r] = true
		}
	}

	injuredEmployee := slices.ContainsFunc(f.People, func(p Person) bool {
		return p.EmployeeOfOrg && (p.Injured || p.DiedAt != nil)
	})

	satisfiedAt := map[string]time.Time{}
	for _, d := range duties {
		if d.SatisfiedAt != nil {
			satisfiedAt[d.RuleKey] = *d.SatisfiedAt
		}
	}

	var clocks []Clock
	for _, r := range ds.Deadlines {
		switch r.AppliesWhen {
		case regimes.AppliesIncidentType:
			if listed[r.Key] {
				clocks = append(clocks, Clock{Rule: r, BasisAt: f.OccurredAt})
			}
		case regimes.AppliesEmployeeInjured:
			if !injuredEmployee {
				continue
			}
			if f.EmployerAwareAt == nil {
				return nil, ErrEmployerAwareRequired
			}
			clocks = append(clocks, Clock{Rule: r, BasisAt: *f.EmployerAwareAt})
		case regimes.AppliesPersonDiedAfterNotice:
			if died, ok := deathAfterNotice(f.People, r.AfterSatisfied, satisfiedAt); ok {
				clocks = append(clocks, Clock{Rule: r, BasisAt: died})
			}
		case regimes.AppliesSiteWork:
			// Starting work on a site, not an incident.
		default:
			return nil, fmt.Errorf("%w: %s %q", errRuleWithoutApplication, r.Key, r.AppliesWhen)
		}
	}
	return clocks, nil
}

// deathAfterNotice is the earliest death later than the earliest satisfaction
// of any of notices.
func deathAfterNotice(people []Person, notices []string, satisfiedAt map[string]time.Time) (time.Time, bool) {
	var first time.Time
	for _, n := range notices {
		if at, ok := satisfiedAt[n]; ok && (first.IsZero() || at.Before(first)) {
			first = at
		}
	}
	if first.IsZero() {
		return time.Time{}, false
	}
	var earliest time.Time
	for _, p := range people {
		if p.DiedAt != nil && p.DiedAt.After(first) && (earliest.IsZero() || p.DiedAt.Before(earliest)) {
			earliest = *p.DiedAt
		}
	}
	return earliest, !earliest.IsZero()
}

// Action is what a new version does to one duty.
type Action string

const (
	// Carry: same rule, same basis; the live row stands.
	Carry Action = "carry"
	// Supersede: same rule, new basis; a new row replaces the live one. The
	// duty's satisfaction carries over and is judged against the new row.
	Supersede Action = "supersede"
	// Withdraw: the rule no longer applies; the live row is closed and kept.
	Withdraw Action = "withdraw"
	// Add: the rule applies and the duty has no live row. DutyID is set when
	// the duty exists (it was withdrawn before and applies again), so its
	// satisfaction carries over.
	Add Action = "add"
)

type Step struct {
	Action  Action
	Rule    regimes.DeadlineRule // zero for Withdraw
	BasisAt time.Time            // zero for Withdraw
	DutyID  string               // "" for an Add of a new duty
	LiveID  string               // the live row Carry keeps, or Supersede and Withdraw close
}

// Plan compares the clocks a version starts with the stored duties. Steps
// come in the order of the clocks, then withdrawals in the order of duties.
// Bases compare at microseconds, as Postgres stores them.
func Plan(clocks []Clock, duties []Duty) []Step {
	byRule := map[string]Duty{}
	for _, d := range duties {
		byRule[d.RuleKey] = d
	}
	applies := map[string]bool{}

	var steps []Step
	for _, c := range clocks {
		applies[c.Rule.Key] = true
		d, ok := byRule[c.Rule.Key]
		switch {
		case !ok:
			steps = append(steps, Step{Action: Add, Rule: c.Rule, BasisAt: c.BasisAt})
		case d.Live == nil:
			steps = append(steps, Step{Action: Add, Rule: c.Rule, BasisAt: c.BasisAt, DutyID: d.ID})
		case d.Live.BasisAt.Truncate(time.Microsecond).Equal(c.BasisAt.Truncate(time.Microsecond)):
			steps = append(steps, Step{Action: Carry, Rule: c.Rule, BasisAt: d.Live.BasisAt, DutyID: d.ID, LiveID: d.Live.ID})
		default:
			steps = append(steps, Step{Action: Supersede, Rule: c.Rule, BasisAt: c.BasisAt, DutyID: d.ID, LiveID: d.Live.ID})
		}
	}
	for _, d := range duties {
		if d.Live != nil && !applies[d.RuleKey] {
			steps = append(steps, Step{Action: Withdraw, DutyID: d.ID, LiveID: d.Live.ID})
		}
	}
	return steps
}

// The states of a deadline, as the API shows them.
const (
	StateOpen          = "open"           // due in the future, not met
	StateDueNow        = "due_now"        // an "immediately" duty not met yet
	StateOverdue       = "overdue"        // past due, not met
	StateSatisfied     = "satisfied"      // met by the due time
	StateSatisfiedLate = "satisfied_late" // met after the due time
	StateSuperseded    = "superseded"     // replaced by a recomputation
	StateWithdrawn     = "withdrawn"      // the rule no longer applies
)

var States = []string{StateOpen, StateDueNow, StateOverdue, StateSatisfied, StateSatisfiedLate, StateSuperseded, StateWithdrawn}

// State of one deadline row at now. closedAs is "" for a live row. A
// satisfaction is judged against this row's due time: on time means before
// due_at (the first instant after the deadline). "Immediately" has no due
// time to miss (forthwith is a prompt, not a countdown), so any satisfaction
// meets it.
func State(closedAs, unit string, dueAt time.Time, satisfiedAt *time.Time, now time.Time) string {
	switch {
	case closedAs == "superseded":
		return StateSuperseded
	case closedAs == "withdrawn":
		return StateWithdrawn
	case satisfiedAt != nil && (unit == "immediately" || satisfiedAt.Before(dueAt)):
		return StateSatisfied
	case satisfiedAt != nil:
		return StateSatisfiedLate
	case unit == "immediately":
		return StateDueNow
	case !now.Before(dueAt):
		return StateOverdue
	default:
		return StateOpen
	}
}
