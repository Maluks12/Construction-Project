package checklists

import (
	"context"
	"errors"
	"fmt"

	"construction_db/internal/core/sites"
	"construction_db/internal/regimes"
)

type Service struct {
	sites *sites.Service
}

func NewService(sitesSvc *sites.Service) *Service {
	return &Service{sites: sitesSvc}
}

// Catalog returns the templates of the site's regime (sites.Service.Regime,
// so the same scoping as GET /v1/sites/:id) and the severity scale.
func (s *Service) Catalog(ctx context.Context, orgID, userID, role, siteID string) (*ChecklistCatalog, error) {
	ds, err := s.sites.Regime(ctx, orgID, userID, role, siteID)
	if errors.Is(err, sites.ErrSiteNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("checklist catalog: %w", err)
	}
	return catalogOf(ds), nil
}

func catalogOf(ds *regimes.DataSet) *ChecklistCatalog {
	c := &ChecklistCatalog{
		Regime:     RegimeRef{Code: ds.Regime.Code, Name: ds.Regime.Name, Version: ds.Regime.Version},
		Checklists: make([]ChecklistTemplate, 0, len(ds.Checklists)),
		Severities: Severities,
	}
	for _, t := range ds.Checklists {
		items := make([]ChecklistItem, 0, len(t.Items))
		for _, it := range t.Items {
			items = append(items, ChecklistItem{Key: it.Key, Prompt: it.Prompt, ResponseType: it.ResponseType, SourceRef: it.SourceRef})
		}
		c.Checklists = append(c.Checklists, ChecklistTemplate{
			Key: t.Key, Version: t.Version, Title: t.Title, Cadence: t.Cadence, SourceRef: t.SourceRef, Items: items,
		})
	}
	return c
}
