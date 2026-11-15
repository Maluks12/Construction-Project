package checklists

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/auth"
)

// Abandon gives up an inspection still in progress (decided 15 Nov). It stays
// as it is, marked abandoned with who, when and why: its answers are kept, it
// raises no findings, and it stays in lists and the audit pack. Abandoning
// frees its place if it was a correction.
//
// Who: the inspector who started it, an owner or a SHE officer. If any item
// is answered "fail", only an owner or a SHE officer, so a fail can't be made
// to disappear by abandoning instead of completing; the database enforces
// that rule too. created is false when it was already abandoned.
func (s *Service) Abandon(ctx context.Context, orgID, userID, role, siteID, inspectionID string, req AbandonRequest) (in *Inspection, created bool, err error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > maxReasonChars {
		return nil, false, ErrBadReason
	}
	lead := role == auth.RoleOwner || role == auth.RoleSHEOfficer

	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		found, err := GetSiteInspectionForUpdate(ctx, tx, orgID, site.ID, inspectionID)
		if isNotFound(err) {
			return ErrInspectionNotFound
		}
		if err != nil {
			return err
		}

		// Who may, before what state it's in: someone who may not abandon it
		// learns nothing more from a repeat.
		if !lead {
			if found.StartedBy != userID {
				return ErrCantAbandon
			}
			failed, err := HasFail(ctx, tx, orgID, found.ID)
			if err != nil {
				return err
			}
			if failed {
				return ErrAbandonNeedsLead
			}
		}

		switch found.Status {
		case StatusAbandoned:
			in, err = withItems(ctx, tx, orgID, found)
			return err
		case StatusCompleted:
			return ErrInspectionCompleted
		}

		if err := AbandonInspection(ctx, tx, orgID, found.ID, userID, reason); err != nil {
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
		return nil, false, fmt.Errorf("abandon inspection: %w", err)
	}
	return in, created, nil
}
