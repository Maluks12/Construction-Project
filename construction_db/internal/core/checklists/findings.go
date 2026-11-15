package checklists

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Complete closes an inspection. It must be complete: every item answered and
// a photo on every fail (Rev 4 §2.4). The database checks the same and raises
// one finding per fail; checking here first names the items to fix. Only the
// inspector who started it may complete it; an archived site doesn't stop
// them. created is false when it was already completed.
func (s *Service) Complete(ctx context.Context, orgID, userID, role, siteID, inspectionID string, req CompleteRequest) (in *Inspection, created bool, err error) {
	// Postgres keeps microseconds.
	req.ClientCompletedAt = req.ClientCompletedAt.Truncate(time.Microsecond)

	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		// Taken before reading the items, so no answer lands between the
		// check below and the update.
		found, err := GetSiteInspectionForUpdate(ctx, tx, orgID, site.ID, inspectionID)
		if isNotFound(err) {
			return ErrInspectionNotFound
		}
		if err != nil {
			return err
		}
		if found.StartedBy != userID {
			return ErrNotInspector
		}
		if found.Status == StatusCompleted {
			in, err = withItems(ctx, tx, orgID, found)
			return err
		}
		if err := inProgress(found); err != nil {
			return err
		}

		items, err := ListItems(ctx, tx, orgID, found.ID)
		if err != nil {
			return err
		}
		var unanswered, photoless []string
		for _, it := range items {
			switch {
			case it.Result == nil:
				unanswered = append(unanswered, it.Key)
			case *it.Result == ResultFail && len(it.Evidence) == 0:
				photoless = append(photoless, it.Key)
			}
		}
		if len(unanswered) > 0 {
			return &IncompleteError{Err: ErrItemsNotAnswered, ItemKeys: unanswered}
		}
		if len(photoless) > 0 {
			return &IncompleteError{Err: ErrFailNeedsPhoto, ItemKeys: photoless}
		}

		if err := CompleteInspection(ctx, tx, orgID, found.ID, req.ClientCompletedAt); err != nil {
			return err
		}
		created = true
		stored, err := GetInspection(ctx, tx, orgID, found.ID)
		if err != nil {
			return err
		}
		in, err = withItems(ctx, tx, orgID, stored)
		return err
	})
	if isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("complete inspection: %w", err)
	}
	return in, created, nil
}

// Findings lists a site's findings, newest first. It works on an archived
// site. The query is checked before the site, so a bad one is 400 anywhere.
func (s *Service) Findings(ctx context.Context, orgID, userID, role, siteID, severity, coverage string, limit int) ([]Finding, error) {
	if severity != "" && !slices.ContainsFunc(Severities, func(l SeverityLevel) bool { return l.Key == severity }) {
		return nil, ErrBadSeverity
	}
	if coverage != "" && !slices.Contains(Coverages, coverage) {
		return nil, ErrBadCoverage
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}

	var out []Finding
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = ListSiteFindings(ctx, tx, orgID, site.ID, severity, coverage, limit)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	return out, nil
}
