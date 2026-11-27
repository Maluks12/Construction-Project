package construction

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/documents"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/onboarding"
	"construction_db/internal/core/workers"
	"construction_db/internal/editions/construction/appointments"
	"construction_db/internal/platform/database"
)

// Notes on the construction wizard's steps (ONBOARD D12).
var (
	NoteRequiredAppointments = "Optional here: CR 2014 reg 8(1) and 8(7) bind the principal contractor; each gap says when it may not apply."
	NoteSiteWork             = "Optional: regulation 4 notification covers only excavation, work at height, demolition, explosives, or a single-storey dwelling for the client; record site work when it applies."
)

// OnboardingContributor adds the H&S file, the required appointments and
// site work to the wizard, for construction sites.
func (Edition) OnboardingContributor(deps edition.Deps) onboarding.Contributor {
	return onboardingSteps{deps: deps, appts: appointments.NewService(deps)}
}

type onboardingSteps struct {
	deps  edition.Deps
	appts *appointments.Service
}

func (o onboardingSteps) OnboardingSteps(ctx context.Context, sc onboarding.StepContext) ([]onboarding.Step, error) {
	var ids []string
	hs := onboarding.Step{Key: onboarding.StepHSFile, Required: true, Gaps: []onboarding.Gap{}}
	appts := onboarding.Step{Key: onboarding.StepRequiredAppointments, Note: &NoteRequiredAppointments, Gaps: []onboarding.Gap{}}
	hsTotal, apptTotal := 0, 0
	for _, site := range sc.Sites {
		if site.Edition != (Edition{}).Name() {
			continue
		}
		ids = append(ids, site.ID)
		siteID := site.ID
		reg, err := o.deps.Documents.Register(ctx, sc.OrgID, sc.UserID, sc.Role, documents.Where{Scope: documents.ScopeSite, SiteID: site.ID}, "")
		if err != nil {
			return nil, fmt.Errorf("h&s file: %w", err)
		}
		gaps, present, required := onboarding.RequiredGaps(reg, &siteID)
		hs.Gaps = append(hs.Gaps, gaps...)
		hs.Count += present
		hsTotal += required

		list, err := o.appts.List(ctx, sc.OrgID, sc.UserID, sc.Role, site.ID)
		if err != nil {
			return nil, fmt.Errorf("appointments: %w", err)
		}
		ds, ok := o.deps.Regimes.Get(site.RegimeCode)
		if !ok {
			continue
		}
		for _, t := range ds.Workforce.AppointmentTypes {
			if !t.Required {
				continue
			}
			apptTotal++
			live := false
			for _, a := range list {
				live = live || (a.TypeKey == t.Key && (a.State == workers.StateValid || a.State == workers.StateExpiring))
			}
			if live {
				appts.Count++
				continue
			}
			key, note := t.Key, t.RequiredNote
			appts.Gaps = append(appts.Gaps, onboarding.Gap{SiteID: &siteID, TypeKey: &key, Label: t.Label, Note: &note})
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	hs.Total, hs.Done = &hsTotal, len(hs.Gaps) == 0
	appts.Total, appts.Done = &apptTotal, len(appts.Gaps) == 0

	work := onboarding.Step{Key: onboarding.StepSiteWork, Note: &NoteSiteWork, Gaps: []onboarding.Gap{}}
	err := database.WithTenant(ctx, o.deps.Pool, database.Tenant{OrgID: sc.OrgID, UserID: sc.UserID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM site_work WHERE org_id = $1 AND site_id = ANY ($2::uuid[])`,
			sc.OrgID, ids).Scan(&work.Count)
	})
	if err != nil {
		return nil, fmt.Errorf("site work: %w", err)
	}
	work.Done = work.Count > 0
	return []onboarding.Step{hs, appts, work}, nil
}
