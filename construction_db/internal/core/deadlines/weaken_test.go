package deadlines_test

import (
	"context"
	"testing"
	"time"

	"construction_db/internal/core/deadlines"
)

// Weakens on its own, for withdrawals (no database needed): the API's 403
// must not depend on the trigger behind it (000046).
func TestWeakensWithdrawals(t *testing.T) {
	ds := zaData(t)
	met := time.Now()
	for _, tc := range []struct {
		rule      string
		satisfied *time.Time
		want      bool
	}{
		{"cr2014_reg4_notification", nil, true},
		{"cr2014_reg3_permit_application", nil, true},
		{gar8a, nil, true},
		{"cr2014_reg4_notification", &met, false},
		{gar92, nil, false},
	} {
		duties := []deadlines.Duty{{ID: "d1", RuleKey: tc.rule, Live: &deadlines.LiveDeadline{ID: "l1"}, SatisfiedAt: tc.satisfied}}
		steps := []deadlines.Step{{Action: deadlines.Withdraw, DutyID: "d1", LiveID: "l1"}}
		got, err := deadlines.Weakens(context.Background(), nil, ds, deadlines.Subject{}, duties, steps, time.Now())
		if err != nil || got != tc.want {
			t.Errorf("withdraw %s (satisfied %v): %v, %v; want %v", tc.rule, tc.satisfied != nil, got, err, tc.want)
		}
	}
}
