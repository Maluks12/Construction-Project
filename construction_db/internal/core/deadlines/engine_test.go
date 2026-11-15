package deadlines_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"construction_db/internal/core/deadlines"
	"construction_db/internal/regimes"
)

const (
	gar8a  = "ohsa_gar8_1a_written_notice"
	gar8b  = "ohsa_gar8_1b_immediate_notice"
	gar82  = "ohsa_gar8_2_death_after_notice"
	gar92  = "ohsa_gar9_2_investigation"
	coida  = "coida_s39_employer_report"
	sixAM  = "2026-11-16T04:00:00Z"
	tenAM  = "2026-11-16T08:00:00Z"
	noon   = "2026-11-16T10:00:00Z"
	onePM  = "2026-11-16T11:00:00Z"
	nextAM = "2026-11-17T08:00:00Z"
)

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func ptr[T any](v T) *T { return &v }

func zaData(t *testing.T) *regimes.DataSet {
	t.Helper()
	reg, err := regimes.Load()
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := reg.Get("za_construction_2014")
	return ds
}

// clockKeys renders clocks as "rule@basis" for comparison.
func clockKeys(clocks []deadlines.Clock) []string {
	out := []string{}
	for _, c := range clocks {
		out = append(out, c.Rule.Key+"@"+c.BasisAt.UTC().Format(time.RFC3339))
	}
	return out
}

func TestApplicable(t *testing.T) {
	ds := zaData(t)
	occurred := ts(t, tenAM)
	aware := ts(t, noon)
	employee := deadlines.Person{EmployeeOfOrg: true, Injured: true}
	contractor := deadlines.Person{EmployeeOfOrg: false, Injured: true}
	notified := []deadlines.Duty{{RuleKey: gar8b, SatisfiedAt: ptr(ts(t, noon))}}

	tests := []struct {
		name   string
		facts  deadlines.Facts
		duties []deadlines.Duty
		want   []string
	}{
		{name: "near miss: no clocks", facts: deadlines.Facts{TaxonomyCodes: []string{"near_miss"}, OccurredAt: occurred}},
		{
			name:  "death: written notice, forthwith call, investigation",
			facts: deadlines.Facts{TaxonomyCodes: []string{"fatality"}, OccurredAt: occurred},
			want:  []string{gar8a + "@" + tenAM, gar8b + "@" + tenAM, gar92 + "@" + tenAM},
		},
		{
			// s24(1)(c)(ii) alone starts no written notice; (a) adds it. The
			// union, each rule once, in the regime's order.
			name:  "several types: the union",
			facts: deadlines.Facts{TaxonomyCodes: []string{"pressure_release", "unconsciousness"}, OccurredAt: occurred},
			want:  []string{gar8a + "@" + tenAM, gar8b + "@" + tenAM, gar92 + "@" + tenAM},
		},
		{
			name:  "pressure release alone",
			facts: deadlines.Facts{TaxonomyCodes: []string{"pressure_release"}, OccurredAt: occurred},
			want:  []string{gar8b + "@" + tenAM, gar92 + "@" + tenAM},
		},
		{
			// D2: COIDA for an injured employee whatever the type, first aid
			// included, from when the employer learned of it.
			name:  "first aid to an employee: COIDA from employer_aware_at",
			facts: deadlines.Facts{TaxonomyCodes: []string{"first_aid"}, OccurredAt: occurred, EmployerAwareAt: &aware, People: []deadlines.Person{employee}},
			want:  []string{coida + "@" + noon},
		},
		{
			name:  "injured contractor: no COIDA for this organisation",
			facts: deadlines.Facts{TaxonomyCodes: []string{"first_aid"}, OccurredAt: occurred, People: []deadlines.Person{contractor}},
		},
		{
			name: "a death counts as an injury for COIDA",
			facts: deadlines.Facts{TaxonomyCodes: []string{"fatality"}, OccurredAt: occurred, EmployerAwareAt: &occurred,
				People: []deadlines.Person{{EmployeeOfOrg: true, DiedAt: ptr(occurred)}}},
			want: []string{gar8a + "@" + tenAM, gar8b + "@" + tenAM, gar92 + "@" + tenAM, coida + "@" + tenAM},
		},
		{
			// GAR 8(2): the forthwith call was made at noon; the person died at
			// 1 PM, so the death must be notified too, timed from the death.
			name: "death after the notice",
			facts: deadlines.Facts{TaxonomyCodes: []string{"loss_of_limb"}, OccurredAt: occurred,
				People: []deadlines.Person{{Injured: true, DiedAt: ptr(ts(t, nextAM))}, {Injured: true, DiedAt: ptr(ts(t, onePM))}}},
			duties: notified,
			want:   []string{gar8a + "@" + tenAM, gar8b + "@" + tenAM, gar82 + "@" + onePM, gar92 + "@" + tenAM},
		},
		{
			// A death before any notice is covered by the GAR 8(1) notices.
			name: "death before the notice",
			facts: deadlines.Facts{TaxonomyCodes: []string{"fatality"}, OccurredAt: occurred,
				People: []deadlines.Person{{Injured: true, DiedAt: ptr(ts(t, tenAM))}}},
			duties: notified,
			want:   []string{gar8a + "@" + tenAM, gar8b + "@" + tenAM, gar92 + "@" + tenAM},
		},
		{
			name: "a death but no notice met yet",
			facts: deadlines.Facts{TaxonomyCodes: []string{"fatality"}, OccurredAt: occurred,
				People: []deadlines.Person{{Injured: true, DiedAt: ptr(ts(t, nextAM))}}},
			want: []string{gar8a + "@" + tenAM, gar8b + "@" + tenAM, gar92 + "@" + tenAM},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deadlines.Applicable(ds, tc.facts, tc.duties)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == nil {
				tc.want = []string{}
			}
			if keys := clockKeys(got); !slices.Equal(keys, tc.want) {
				t.Errorf("got  %v\nwant %v", keys, tc.want)
			}
		})
	}

	t.Run("reg 3 and 4 never attach to an incident", func(t *testing.T) {
		all := []string{}
		for _, ty := range ds.Taxonomy {
			all = append(all, ty.Key)
		}
		got, err := deadlines.Applicable(ds, deadlines.Facts{TaxonomyCodes: all, OccurredAt: occurred, EmployerAwareAt: &aware,
			People: []deadlines.Person{employee}}, notified)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range got {
			if c.Rule.AppliesWhen == regimes.AppliesSiteWork {
				t.Errorf("%s attached to an incident", c.Rule.Key)
			}
		}
	})
	t.Run("an injured employee needs employer_aware_at", func(t *testing.T) {
		_, err := deadlines.Applicable(ds, deadlines.Facts{TaxonomyCodes: []string{"first_aid"}, OccurredAt: occurred,
			People: []deadlines.Person{employee}}, nil)
		if !errors.Is(err, deadlines.ErrEmployerAwareRequired) {
			t.Errorf("got %v, want ErrEmployerAwareRequired", err)
		}
	})
	t.Run("unknown type", func(t *testing.T) {
		_, err := deadlines.Applicable(ds, deadlines.Facts{TaxonomyCodes: []string{"alien_abduction"}, OccurredAt: occurred}, nil)
		if !errors.Is(err, deadlines.ErrUnknownType) {
			t.Errorf("got %v, want ErrUnknownType", err)
		}
	})
}

func TestPlan(t *testing.T) {
	ds := zaData(t)
	rule := func(key string) regimes.DeadlineRule {
		i := slices.IndexFunc(ds.Deadlines, func(r regimes.DeadlineRule) bool { return r.Key == key })
		return ds.Deadlines[i]
	}
	at := ts(t, tenAM)
	earlier := ts(t, sixAM)
	live := func(id string, basis time.Time) *deadlines.LiveDeadline {
		return &deadlines.LiveDeadline{ID: id, BasisAt: basis}
	}

	steps := deadlines.Plan(
		[]deadlines.Clock{
			{Rule: rule(gar8a), BasisAt: earlier},                       // occurred_at corrected
			{Rule: rule(gar8b), BasisAt: at.Add(300 * time.Nanosecond)}, // same at microseconds
			{Rule: rule(gar92), BasisAt: at},                            // withdrawn before, applies again
			{Rule: rule(coida), BasisAt: at},                            // new
		},
		[]deadlines.Duty{
			{ID: "d1", RuleKey: gar8a, Live: live("l1", at), SatisfiedAt: ptr(at)},
			{ID: "d2", RuleKey: gar8b, Live: live("l2", at)},
			{ID: "d3", RuleKey: gar92},
			{ID: "d4", RuleKey: gar82, Live: live("l4", at)}, // no longer applies
		},
	)
	got := []string{}
	for _, s := range steps {
		got = append(got, fmt.Sprintf("%s %s duty=%s live=%s", s.Action, s.Rule.Key, s.DutyID, s.LiveID))
	}
	want := []string{
		"supersede " + gar8a + " duty=d1 live=l1",
		"carry " + gar8b + " duty=d2 live=l2",
		"add " + gar92 + " duty=d3 live=",
		"add " + coida + " duty= live=",
		"withdraw  duty=d4 live=l4",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestState(t *testing.T) {
	now := ts(t, noon)
	due := ts(t, onePM)
	tests := []struct {
		name, closedAs, unit string
		satisfied            *time.Time
		due                  time.Time
		want                 string
	}{
		{"open", "", "calendar_days", nil, due, deadlines.StateOpen},
		{"overdue at the due instant", "", "calendar_days", nil, now, deadlines.StateOverdue},
		{"forthwith, not met", "", "immediately", nil, ts(t, tenAM), deadlines.StateDueNow},
		{"forthwith, met any time", "", "immediately", ptr(ts(t, nextAM)), ts(t, tenAM), deadlines.StateSatisfied},
		{"met in time", "", "calendar_days", ptr(now), due, deadlines.StateSatisfied},
		{"met at the due instant is late", "", "calendar_days", ptr(due), due, deadlines.StateSatisfiedLate},
		{"superseded", "superseded", "calendar_days", nil, due, deadlines.StateSuperseded},
		{"withdrawn, even if met", "withdrawn", "calendar_days", ptr(now), due, deadlines.StateWithdrawn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := deadlines.State(tc.closedAs, tc.unit, tc.due, tc.satisfied, now); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
