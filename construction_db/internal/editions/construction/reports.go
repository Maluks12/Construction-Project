package construction

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/documents"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/reports"
	"construction_db/internal/core/workers"
	"construction_db/internal/editions/construction/appointments"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

// ReportsContributor adds the H&S file and appointments to the summary.
func (Edition) ReportsContributor(deps edition.Deps) reports.Contributor {
	return contributor{deps: deps, appts: appointments.NewService(deps)}
}

type contributor struct {
	deps  edition.Deps
	appts *appointments.Service
}

func (c contributor) ComplianceCategories(ctx context.Context, sc reports.SiteContext) ([]reports.Category, error) {
	if sc.Regime.Regime.Edition != (Edition{}).Name() {
		return nil, nil
	}
	reg, err := c.deps.Documents.Register(ctx, sc.OrgID, sc.UserID, sc.Role, documents.Where{Scope: documents.ScopeSite, SiteID: sc.Site.ID}, "")
	if err != nil {
		return nil, fmt.Errorf("h&s file: %w", err)
	}
	hs := reports.Category{Key: reports.CatHSFile, Counted: true, Items: reports.DocumentItems(reg, true)}

	list, err := c.appts.List(ctx, sc.OrgID, sc.UserID, sc.Role, sc.Site.ID)
	if err != nil {
		return nil, fmt.Errorf("appointments: %w", err)
	}
	work, err := c.siteWorkCategory(ctx, sc)
	if err != nil {
		return nil, err
	}
	return []reports.Category{hs, appointmentCategory(sc, list), work}, nil
}

// siteWorkCategory (REG4 D3, D10): an overdue notice is red when the rule
// binds the contractor (reg 4), amber with its caveat when it binds the
// client (reg 3), since SiteProof doesn't record the organisation's role on
// the site yet. Due within the first notification lead day: amber.
func (c contributor) siteWorkCategory(ctx context.Context, sc reports.SiteContext) (reports.Category, error) {
	cat := reports.Category{Key: reports.CatSiteWorkDeadlines, Counted: true, Items: []reports.Item{}}
	var live []deadlines.Deadline
	err := database.WithTenant(ctx, c.deps.Pool, database.Tenant{OrgID: sc.OrgID, UserID: sc.UserID}, func(tx pgx.Tx) error {
		var err error
		live, err = deadlines.ListLiveForSite(ctx, tx, sc.OrgID, sc.Site.ID, sc.Now)
		return err
	})
	if err != nil {
		return cat, fmt.Errorf("site work deadlines: %w", err)
	}
	soon := 0
	if d := sc.Regime.Reminders.NotificationDaysBefore; len(d) > 0 {
		soon = d[0]
	}
	red, amber := reports.Red, reports.Amber
	for _, d := range live {
		if d.SubjectType != deadlines.SubjectSiteWork {
			continue
		}
		client := false
		if i := slices.IndexFunc(sc.Regime.Deadlines, func(r regimes.DeadlineRule) bool { return r.Key == d.RuleKey }); i >= 0 {
			client = sc.Regime.Deadlines[i].DutyHolder == regimes.DutyClient
		}
		item := reports.Item{SubjectType: reports.SubjectDeadline, SubjectID: &d.ID, Label: d.Label, SiteWorkID: d.SiteWorkID,
			DueOn: d.DueOn, Since: &d.DueAt}
		switch {
		case d.State == deadlines.StateOverdue && client:
			item.State, item.Colour, item.Note = reports.ItemOverdue, &amber, d.Caveat
		case d.State == deadlines.StateOverdue:
			item.State, item.Colour = reports.ItemOverdue, &red
		case d.State == deadlines.StateOpen && d.DueOn != nil && dueWithin(*d.DueOn, sc.Today, soon):
			item.State, item.Colour = reports.ItemDueSoon, &amber
			if client {
				item.Note = d.Caveat
			}
		default:
			continue
		}
		cat.Items = append(cat.Items, item)
	}
	return cat, nil
}

func dueWithin(dueOn, today string, days int) bool {
	due, err1 := time.Parse(time.DateOnly, dueOn)
	t, err2 := time.Parse(time.DateOnly, today)
	return err1 == nil && err2 == nil && !due.After(t.AddDate(0, 0, days))
}

// appointmentCategory: a required type (D4) with no live appointee is amber, with its note.
func appointmentCategory(sc reports.SiteContext, list []appointments.Appointment) reports.Category {
	amber := reports.Amber
	cat := reports.Category{Key: reports.CatAppointments, Counted: true, Items: []reports.Item{}}
	for _, t := range sc.Regime.Workforce.AppointmentTypes {
		if !t.Required {
			continue
		}
		live := false
		for _, a := range list {
			if a.TypeKey == t.Key && (a.State == workers.StateValid || a.State == workers.StateExpiring) {
				live = true
			}
		}
		if !live {
			key, note := t.Key, t.RequiredNote
			cat.Items = append(cat.Items, reports.Item{SubjectType: reports.SubjectAppointmentType, State: reports.ItemNoAppointee,
				Colour: &amber, Label: t.Label, TypeKey: &key, Note: &note})
		}
	}
	for _, a := range list {
		var state string
		switch a.State {
		case workers.StateExpiring:
			state = reports.ItemExpiring
		case workers.StateExpired:
			state = reports.ItemExpired
		default:
			continue
		}
		id, worker, key := a.ID, a.WorkerID, a.TypeKey
		label := a.WorkerName
		if t, ok := sc.Regime.AppointmentType(a.TypeKey); ok {
			label += ": " + t.Label
		}
		cat.Items = append(cat.Items, reports.Item{SubjectType: reports.SubjectAppointment, SubjectID: &id, State: state,
			Colour: &amber, Label: label, WorkerID: &worker, TypeKey: &key, DueOn: a.ExpiresOn})
	}
	return cat
}
