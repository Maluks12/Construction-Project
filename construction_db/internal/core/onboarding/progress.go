package onboarding

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/documents"
	"construction_db/internal/platform/database"
)

// Notes on the steps that aren't required (D12).
var (
	NoteTeam            = "Optional: a one-person business may never invite anyone."
	NoteLocations       = "Optional: zones and floors let records say where on site; add them when a site needs them."
	NoteWorkerDocuments = "Optional here: certificates and medicals can be loaded in bulk now or captured on site later; sign-ins warn about what's missing."
)

func ptr[T any](v T) *T { return &v }

// Progress is the first-run wizard's state (D12), computed on each call:
// nothing is stored, so it can't go stale, and it is complete when every
// required step is done.
func (s *Service) Progress(ctx context.Context, orgID, userID, role string) (*Progress, error) {
	sc := StepContext{OrgID: orgID, UserID: userID, Role: role, Now: s.Clock.Now()}
	var members, active, withDocs, locations int
	err := database.WithTenantSnapshot(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT s.id::text, s.name, rr.code, rr.edition
			FROM sites s JOIN regulatory_regimes rr ON rr.id = s.regime_id
			WHERE s.org_id = $1 AND s.status <> 'archived'
			ORDER BY s.created_at, s.id`, orgID)
		if err != nil {
			return err
		}
		sc.Sites, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SiteRef, error) {
			var x SiteRef
			return x, r.Scan(&x.ID, &x.Name, &x.RegimeCode, &x.Edition)
		})
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM org_members WHERE org_id = $1 AND status IN ('invited', 'active')),
			       (SELECT count(*) FROM workers WHERE org_id = $1 AND active),
			       (SELECT count(*) FROM workers w WHERE w.org_id = $1 AND w.active AND (
			            EXISTS (SELECT 1 FROM worker_competencies c WHERE c.worker_id = w.id AND c.retracted_at IS NULL)
			         OR EXISTS (SELECT 1 FROM worker_medicals m WHERE m.worker_id = w.id AND m.retracted_at IS NULL))),
			       (SELECT count(*) FROM site_locations l JOIN sites s ON s.id = l.site_id
			        WHERE l.org_id = $1 AND l.archived_at IS NULL AND s.status <> 'archived')`,
			orgID).Scan(&members, &active, &withDocs, &locations)
	})
	if err != nil {
		return nil, fmt.Errorf("progress: %w", err)
	}

	team := max(members-1, 0) // everyone but the person who signed up
	steps := []Step{
		{Key: StepSites, Required: true, Done: len(sc.Sites) > 0, Count: len(sc.Sites), Gaps: []Gap{}},
		{Key: StepTeam, Done: team > 0, Count: team, Note: &NoteTeam, Gaps: []Gap{}},
	}
	company, err := s.companyDocuments(ctx, sc)
	if err != nil {
		return nil, err
	}
	steps = append(steps, company,
		Step{Key: StepWorkers, Required: true, Done: active > 0, Count: active, Gaps: []Gap{}},
		Step{Key: StepLocations, Done: locations > 0, Count: locations, Note: &NoteLocations, Gaps: []Gap{}},
		Step{Key: StepWorkerDocuments, Done: active > 0 && withDocs == active, Count: withDocs, Total: ptr(active),
			Note: &NoteWorkerDocuments, Gaps: []Gap{}},
	)
	if s.contrib != nil {
		more, err := s.contrib.OnboardingSteps(ctx, sc)
		if err != nil {
			return nil, fmt.Errorf("progress: %w", err)
		}
		steps = append(steps, more...)
	}
	p := &Progress{Complete: true, Steps: steps}
	for _, st := range steps {
		if st.Required && !st.Done {
			p.Complete = false
		}
	}
	return p, nil
}

// companyDocuments: the vault's required types, for each regime the
// organisation's sites run under, present and in date.
func (s *Service) companyDocuments(ctx context.Context, sc StepContext) (Step, error) {
	st := Step{Key: StepCompanyDocuments, Required: true, Gaps: []Gap{}}
	seen := map[string]bool{}
	total := 0
	for _, site := range sc.Sites {
		if seen[site.RegimeCode] {
			continue
		}
		seen[site.RegimeCode] = true
		reg, err := s.documents.Register(ctx, sc.OrgID, sc.UserID, sc.Role, documents.Where{Scope: documents.ScopeOrg}, site.RegimeCode)
		if err != nil {
			return st, fmt.Errorf("progress: vault: %w", err)
		}
		gaps, present, required := RequiredGaps(reg, nil)
		st.Gaps = append(st.Gaps, gaps...)
		st.Count += present
		total += required
	}
	st.Total = ptr(total)
	st.Done = len(sc.Sites) > 0 && len(st.Gaps) == 0
	return st, nil
}

// RequiredGaps are a register's required types that aren't present and in
// date (an expired one is a gap too), with how many are and how many there
// are; site is the register's site, nil for the vault.
func RequiredGaps(reg *documents.Register, site *string) (gaps []Gap, present, required int) {
	gaps = []Gap{}
	for _, r := range reg.Readiness {
		if !r.Required {
			continue
		}
		required++
		if r.Status == documents.StatusPresent {
			present++
			continue
		}
		key := r.TypeKey
		g := Gap{SiteID: site, TypeKey: &key, Label: r.Label}
		if r.Status == documents.StatusExpired {
			g.Note = ptr("Expired: load the current one.")
		}
		gaps = append(gaps, g)
	}
	return gaps, present, required
}
