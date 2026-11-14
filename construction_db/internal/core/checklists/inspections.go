package checklists

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/core/sites"
	"construction_db/internal/platform/database"
)

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

// Start begins an inspection under an id the device chose, so a retry after a
// lost connection is safe. created is false for an idempotent repeat.
func (s *Service) Start(ctx context.Context, orgID, userID, role, siteID, inspectionID string, req StartInspectionRequest) (in *Inspection, created bool, err error) {
	id, err := uuid.Parse(inspectionID)
	if err != nil {
		return nil, false, ErrBadInspectionID
	}
	// Bytes, not characters: the column counts bytes.
	if d := req.DeviceID; d != nil && (len(*d) == 0 || len(*d) > 200 || !utf8.ValidString(*d)) {
		return nil, false, ErrBadDeviceID
	}
	if req.CorrectsID != nil {
		c, err := uuid.Parse(*req.CorrectsID)
		if err != nil {
			return nil, false, ErrBadCorrectsID
		}
		// Canonical form, so a retry compares equal to what Postgres returns.
		canonical := c.String()
		req.CorrectsID = &canonical
	}
	// Postgres keeps microseconds; truncating here makes a retry compare equal.
	req.ClientStartedAt = req.ClientStartedAt.Truncate(time.Microsecond)

	// The insert can lose a race to an identical start (a client retrying
	// before its first request finished). The second attempt sees the
	// winner's committed row and answers as an idempotent repeat.
	for attempt := 1; ; attempt++ {
		in, created, err = s.startOnce(ctx, orgID, userID, role, siteID, id.String(), req)
		if !errors.Is(err, errIDTaken) || attempt == 2 {
			break
		}
	}
	if errors.Is(err, errIDTaken) {
		err = ErrInspectionIDReused
	}
	if isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("start inspection: %w", err)
	}
	return in, created, nil
}

// errIDTaken: the insert hit the primary key. Either a concurrent start of
// the same id committed first, or the id belongs to another organisation and
// RLS hides it. Start tells the two apart by looking again.
var errIDTaken = errors.New("inspection id taken")

func (s *Service) startOnce(ctx context.Context, orgID, userID, role, siteID, id string, req StartInspectionRequest) (in *Inspection, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		site := sr.Site

		// Before the archived check: a retry of a start that succeeded
		// before the site was archived must still answer 200.
		existing, err := GetInspection(ctx, tx, orgID, id)
		switch {
		case err == nil && !sameStart(existing, site.ID, userID, req):
			return ErrInspectionIDReused
		case err == nil:
			in, err = withItems(ctx, tx, orgID, existing)
			return err
		case !isNotFound(err):
			return err
		}

		// Only new inspections are refused. Reads, and finishing one already
		// in progress, stay allowed on an archived site.
		if site.Status == sites.StatusArchived {
			return ErrSiteArchived
		}

		tmpl, err := resolveTemplate(sr.Data, req.TemplateKey, req.TemplateVersion, req.TemplateSHA256)
		if err != nil {
			return err
		}
		items := make([]newResponse, 0, len(tmpl.Items))
		for _, it := range tmpl.Items {
			if !slices.Contains(SupportedResponseTypes, it.ResponseType) {
				return fmt.Errorf("template %s version %d item %s: response type %q is not supported", tmpl.Key, tmpl.Version, it.Key, it.ResponseType)
			}
			var ref *string
			if it.SourceRef != "" {
				ref = &it.SourceRef
			}
			items = append(items, newResponse{Key: it.Key, Prompt: it.Prompt, ResponseType: it.ResponseType, SourceRef: ref})
		}

		if req.CorrectsID != nil {
			// The insert trigger and the same-site foreign key enforce this
			// too; checking here gives the client a 400 it can act on.
			corrected, err := GetSiteInspection(ctx, tx, orgID, site.ID, *req.CorrectsID)
			if isNotFound(err) || (err == nil && corrected.Status != StatusCompleted) {
				return ErrBadCorrectsID
			}
			if err != nil {
				return err
			}
		}

		// The hash stored is the server's own for this version;
		// resolveTemplate has just checked the client's equals it.
		sum, err := hex.DecodeString(tmpl.ContentHash())
		if err != nil {
			return fmt.Errorf("template hash: %w", err)
		}
		err = InsertInspection(ctx, tx, newInspection{
			ID: id, OrgID: orgID, SiteID: site.ID, RegimeID: sr.RegimeID, StartedBy: userID,
			TemplateKey: tmpl.Key, TemplateVersion: tmpl.Version, TemplateSHA256: sum,
			TemplateTitle: tmpl.Title, TemplateSourceRef: tmpl.SourceRef, ItemCount: len(items),
			ClientStartedAt: req.ClientStartedAt, DeviceID: req.DeviceID, CorrectsID: req.CorrectsID,
		})
		if isUniqueViolation(err, "inspections_pkey") {
			return errIDTaken
		}
		if err != nil {
			return err
		}
		if err := InsertResponses(ctx, tx, orgID, site.ID, id, items); err != nil {
			return err
		}

		created = true
		stored, err := GetInspection(ctx, tx, orgID, id)
		if err != nil {
			return err
		}
		in, err = withItems(ctx, tx, orgID, stored)
		return err
	})
	return in, created, err
}

// sameStart: a retry must match everything the first start recorded.
func sameStart(in *Inspection, siteID, userID string, req StartInspectionRequest) bool {
	return in.SiteID == siteID &&
		in.StartedBy == userID &&
		in.TemplateKey == req.TemplateKey &&
		in.TemplateVersion == req.TemplateVersion &&
		strings.EqualFold(in.TemplateSHA256, req.TemplateSHA256) &&
		in.ClientStartedAt.Equal(req.ClientStartedAt) &&
		equalPtr(in.DeviceID, req.DeviceID) &&
		equalPtr(in.CorrectsID, req.CorrectsID)
}

// Get returns one inspection with its items. It works on an archived site:
// history stays readable.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, inspectionID string) (*Inspection, error) {
	var in *Inspection
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		found, err := GetSiteInspection(ctx, tx, orgID, site.ID, inspectionID)
		if isNotFound(err) {
			return ErrInspectionNotFound
		}
		if err != nil {
			return err
		}
		in, err = withItems(ctx, tx, orgID, found)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get inspection: %w", err)
	}
	return in, nil
}

func withItems(ctx context.Context, db DBTX, orgID string, in *Inspection) (*Inspection, error) {
	items, err := ListItems(ctx, db, orgID, in.ID)
	if err != nil {
		return nil, err
	}
	in.Items = items
	return in, nil
}

// isSentinel reports errors the handler maps to a status, which must reach it unwrapped.
func isSentinel(err error) bool {
	for _, s := range []error{
		sites.ErrSiteNotFound, ErrInspectionNotFound, ErrInspectionIDReused, ErrSiteArchived,
		ErrUnknownTemplateVersion, ErrTemplateMismatch, ErrBadCorrectsID,
	} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraint
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
