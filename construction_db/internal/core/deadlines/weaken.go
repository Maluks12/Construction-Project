package deadlines

import (
	"context"
	"slices"
	"time"

	"construction_db/internal/regimes"
)

// Kinds whose weakening needs an owner or SHE officer, as
// deadlines_weakening_check (000046).
var weakenableKinds = []string{"report", "notification"}

// Weakens: steps withdraw an open report or notification, or move one later
// (satisfied or not). The database refuses it at commit unless a lead
// submitted the version; checking first lets the API answer 403.
func Weakens(ctx context.Context, db DBTX, ds *regimes.DataSet, sub Subject, duties []Duty, steps []Step, now time.Time) (bool, error) {
	byID := map[string]Duty{}
	for _, d := range duties {
		byID[d.ID] = d
	}
	for _, st := range steps {
		duty := byID[st.DutyID]
		i := slices.IndexFunc(ds.Deadlines, func(r regimes.DeadlineRule) bool { return r.Key == duty.RuleKey })
		if i < 0 || !slices.Contains(weakenableKinds, ds.Deadlines[i].Kind) {
			continue
		}
		switch st.Action {
		case Withdraw:
			if duty.SatisfiedAt == nil {
				return true, nil
			}
		case Supersede:
			live, err := GetLiveForDuty(ctx, db, sub.OrgID, duty.ID, now)
			if err != nil {
				return false, err
			}
			due, err := DueAt(ctx, db, sub, st.Rule, st.BasisAt)
			if err != nil {
				return false, err
			}
			if due.After(live.DueAt) {
				return true, nil
			}
		}
	}
	return false, nil
}
