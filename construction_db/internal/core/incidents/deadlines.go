package incidents

import (
	"context"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/deadlines"
	"construction_db/internal/regimes"
)

// DeadlineHooks are the incident's part in the site deadline endpoints
// (internal/core/deadlines).
func (s *Service) DeadlineHooks() deadlines.SubjectHooks { return deadlineHooks{} }

type deadlineHooks struct{}

// Recording may add clocks computed in the incident's latest version, so it
// queues with amendments (see lockHeader).
func (deadlineHooks) LockForSatisfaction(ctx context.Context, tx pgx.Tx, orgID, siteID, incidentID string) error {
	_, err := lockHeader(ctx, tx, orgID, siteID, incidentID)
	return err
}

func (deadlineHooks) Satisfied(ctx context.Context, tx pgx.Tx, s deadlines.Satisfied) error {
	return addClocksAfterSatisfaction(ctx, tx, s.Data, s.RegimeID, s.OrgID, s.SiteID, s.SubjectID, s.SatisfactionID)
}

// addClocksAfterSatisfaction evaluates the incident's latest version again
// and adds any clock that applies now and didn't before (a death after the
// notice just recorded). Only additions: anything else needs a new version.
// The rows name the satisfaction, so no version lists them as its own.
func addClocksAfterSatisfaction(ctx context.Context, tx pgx.Tx, ds *regimes.DataSet, regimeID, orgID, siteID, incidentID, satisfactionID string) error {
	h, err := getHeader(ctx, tx, orgID, siteID, incidentID)
	if err != nil {
		return err
	}
	v, err := getVersion(ctx, tx, orgID, incidentID, h.LatestVersion)
	if err != nil {
		return err
	}
	people := make([]PersonInput, 0, len(v.People))
	for _, p := range v.People {
		people = append(people, p.PersonInput)
	}
	ref := deadlines.Ref{Kind: deadlines.SubjectIncident, ID: incidentID}
	duties, err := deadlines.Duties(ctx, tx, orgID, ref)
	if err != nil {
		return err
	}
	clocks, err := deadlines.Applicable(ds, factsOf(v.Codes, v.OccurredAt, v.EmployerAwareAt, people), duties)
	if err != nil {
		return err
	}
	var adds []deadlines.Step
	for _, st := range deadlines.Plan(clocks, duties) {
		if st.Action == deadlines.Add {
			adds = append(adds, st)
		}
	}
	sub := deadlines.Subject{OrgID: orgID, SiteID: siteID, Ref: ref, RegimeID: regimeID, Regime: ds.Regime,
		Version: h.LatestVersion, AddedBySatisfaction: &satisfactionID}
	return deadlines.Apply(ctx, tx, sub, adds)
}
