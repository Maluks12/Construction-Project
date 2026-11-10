package checklists

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// catalogOf offers only the latest version of each template. Older versions
// stay in the data so inspections already started on them remain valid.
func catalogOf(ds *regimes.DataSet) *ChecklistCatalog {
	latest := ds.Latest()
	c := &ChecklistCatalog{
		Regime:     RegimeRef{Code: ds.Regime.Code, Name: ds.Regime.Name, Version: ds.Regime.Version},
		Checklists: make([]ChecklistTemplate, 0, len(latest)),
		Severities: Severities,
	}
	for _, t := range latest {
		items := make([]ChecklistItem, 0, len(t.Items))
		for _, it := range t.Items {
			items = append(items, ChecklistItem{Key: it.Key, Prompt: it.Prompt, ResponseType: it.ResponseType, SourceRef: it.SourceRef})
		}
		c.Checklists = append(c.Checklists, ChecklistTemplate{
			Key: t.Key, Version: t.Version, SHA256: t.ContentHash(), Title: t.Title, Cadence: t.Cadence,
			SourceRef: t.SourceRef, Items: items,
		})
	}
	return c
}

// resolveTemplate finds the exact template version an inspection is started
// on, and checks that the hash the phone sent is that version's content hash.
// Any shipped version is accepted, so an inspection started offline before a
// newer version shipped still syncs. Comparison ignores hex case; a malformed
// hash is simply a mismatch. Used by start (7.5).
func resolveTemplate(ds *regimes.DataSet, key string, version int, sha256Hex string) (regimes.ChecklistTemplate, error) {
	t, ok := ds.Checklist(key, version)
	if !ok {
		return regimes.ChecklistTemplate{}, ErrUnknownTemplateVersion
	}
	if !strings.EqualFold(sha256Hex, t.ContentHash()) {
		return regimes.ChecklistTemplate{}, ErrTemplateMismatch
	}
	return t, nil
}
