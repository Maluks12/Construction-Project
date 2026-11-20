package construction

import (
	"context"
	"fmt"

	"construction_db/internal/core/documents"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/reports"
	"construction_db/internal/core/workers"
	"construction_db/internal/editions/construction/appointments"
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
	return []reports.Category{hs, appointmentCategory(sc, list)}, nil
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
