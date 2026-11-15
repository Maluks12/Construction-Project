package actions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/sites"
)

// WaiverRequest marks a low or medium finding "no action needed" (D1).
type WaiverRequest struct {
	InspectionID   string    `json:"inspection_id"`
	ItemKey        string    `json:"item_key"`
	Reason         string    `json:"reason"`
	ClientWaivedAt time.Time `json:"client_waived_at"`
	DeviceID       *string   `json:"device_id"`
}

// RetractWaiverRequest takes a waiver back; the finding is unactioned again.
type RetractWaiverRequest struct {
	Reason string `json:"reason"`
}

// Waiver is a finding marked "no action needed", and its retraction if any.
type Waiver struct {
	ID             string     `json:"id"`
	SiteID         string     `json:"site_id"`
	InspectionID   string     `json:"inspection_id"`
	ItemKey        string     `json:"item_key"`
	Severity       string     `json:"severity"`
	Reason         string     `json:"reason"`
	WaivedBy       string     `json:"waived_by"`
	ClientWaivedAt time.Time  `json:"client_waived_at"`
	WaivedAt       time.Time  `json:"waived_at"`
	DeviceID       *string    `json:"device_id"`
	RetractedBy    *string    `json:"retracted_by"`
	RetractedAt    *time.Time `json:"retracted_at"`
	RetractReason  *string    `json:"retract_reason"`
}

var (
	ErrBadWaiverID             = errors.New("waiver id must be a UUID")
	ErrWaiverIDReused          = errors.New("waiver id already used with different details")
	ErrWaiverNotFound          = errors.New("waiver not found")
	ErrCantWaive               = errors.New("only an owner or a SHE officer can waive a finding or retract a waiver")
	ErrNotWaivable             = errors.New("only a low or medium finding can be waived; raise an action instead")
	ErrAlreadyWaived           = errors.New("finding already has a waiver; retract it first")
	ErrWaiverRetractionDiffers = errors.New("waiver was already retracted with a different reason")
	ErrNoWaivedAt              = errors.New("client_waived_at is required")
	ErrWaivedInFuture          = errors.New("client_waived_at is in the future; check the device clock")
)

func init() {
	sentinels = append(sentinels, ErrBadWaiverID, ErrWaiverIDReused, ErrWaiverNotFound, ErrCantWaive, ErrNotWaivable,
		ErrAlreadyWaived, ErrWaiverRetractionDiffers, ErrNoWaivedAt, ErrWaivedInFuture)
}

// Waive records a waiver under an id the device chose. Order of checks:
// site scope, a repeat of the id (200), who may, the site archived, the
// finding, superseded, its severity, a live waiver already.
func (s *Service) Waive(ctx context.Context, orgID, userID, role, siteID, waiverID string, req WaiverRequest) (*Waiver, bool, error) {
	wid, err := uuid.Parse(waiverID)
	if err != nil {
		return nil, false, ErrBadWaiverID
	}
	if err := normaliseWaiver(&req, time.Now()); err != nil {
		return nil, false, err
	}
	var (
		w       *Waiver
		created bool
	)
	err = once(func() error {
		w, created, err = s.waiveOnce(ctx, orgID, userID, role, siteID, wid.String(), req)
		return err
	})
	if errors.Is(err, errRetry) {
		err = ErrWaiverIDReused
	}
	return finishWaiver(w, created, err, "waive finding")
}

func (s *Service) waiveOnce(ctx context.Context, orgID, userID, role, siteID, id string, req WaiverRequest) (w *Waiver, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if stored, err := getWaiver(ctx, tx, orgID, id); err == nil {
			if stored.SiteID != site.ID || !sameWaiver(stored, userID, req) {
				return ErrWaiverIDReused
			}
			w = stored
			return nil
		} else if !isNotFound(err) {
			return err
		}
		if !isLead(role) {
			return ErrCantWaive
		}
		if site.Status == sites.StatusArchived {
			return ErrSiteArchived
		}
		f, err := getFinding(ctx, tx, orgID, site.ID, req.InspectionID, req.ItemKey)
		if isNotFound(err) {
			return ErrFindingNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case f.SupersededBy != nil:
			return ErrFindingSuperseded
		case f.Severity != "low" && f.Severity != "medium":
			return ErrNotWaivable
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO finding_waivers (id, org_id, site_id, inspection_id, item_key, severity, reason, waived_by, client_waived_at, device_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, orgID, site.ID, req.InspectionID, req.ItemKey, f.Severity, req.Reason, userID, req.ClientWaivedAt, req.DeviceID)
		switch {
		case isUniqueViolation(err, "finding_waivers_pkey"):
			return errRetry
		case isUniqueViolation(err, "finding_waivers_one_live"):
			return ErrAlreadyWaived
		case err != nil:
			return fmt.Errorf("insert waiver: %w", err)
		}
		created = true
		w, err = getWaiver(ctx, tx, orgID, id)
		return err
	})
	return w, created, err
}

// RetractWaiver takes a waiver back, once. Repeating it with the same reason
// answers 200.
func (s *Service) RetractWaiver(ctx context.Context, orgID, userID, role, siteID, waiverID string, req RetractWaiverRequest) (*Waiver, bool, error) {
	if !textOK(&req.Reason, 500) {
		return nil, false, ErrBadReason
	}
	var (
		w       *Waiver
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		// Locked, so two retractions take turns and the second sees the first.
		var retracted bool
		err = tx.QueryRow(ctx, `SELECT retracted_at IS NOT NULL FROM finding_waivers WHERE org_id = $1 AND site_id = $2 AND id = $3 FOR UPDATE`,
			orgID, site.ID, waiverID).Scan(&retracted)
		if isNotFound(err) {
			return ErrWaiverNotFound
		}
		if err != nil {
			return fmt.Errorf("lock waiver: %w", err)
		}
		if !isLead(role) {
			return ErrCantWaive
		}
		if !retracted {
			if _, err := tx.Exec(ctx, `UPDATE finding_waivers SET retracted_by = $2, retracted_at = now(), retract_reason = $3 WHERE id = $1`,
				waiverID, userID, req.Reason); err != nil {
				return fmt.Errorf("retract waiver: %w", err)
			}
			created = true
		}
		if w, err = getWaiver(ctx, tx, orgID, waiverID); err != nil {
			return err
		}
		if !created && (w.RetractReason == nil || *w.RetractReason != req.Reason) {
			return ErrWaiverRetractionDiffers
		}
		return nil
	})
	return finishWaiver(w, created, err, "retract waiver")
}

func finishWaiver(w *Waiver, created bool, err error, prefix string) (*Waiver, bool, error) {
	if err = mapRule(err); isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", prefix, err)
	}
	return w, created, nil
}

func getWaiver(ctx context.Context, db DBTX, orgID, id string) (*Waiver, error) {
	var w Waiver
	err := db.QueryRow(ctx, `
		SELECT id, site_id, inspection_id, item_key, severity, reason, waived_by, client_waived_at, waived_at, device_id,
		       retracted_by, retracted_at, retract_reason
		FROM finding_waivers WHERE org_id = $1 AND id = $2`, orgID, id).Scan(
		&w.ID, &w.SiteID, &w.InspectionID, &w.ItemKey, &w.Severity, &w.Reason, &w.WaivedBy, &w.ClientWaivedAt, &w.WaivedAt,
		&w.DeviceID, &w.RetractedBy, &w.RetractedAt, &w.RetractReason)
	if err != nil {
		return nil, fmt.Errorf("get waiver: %w", err)
	}
	w.ClientWaivedAt, w.WaivedAt, w.RetractedAt = w.ClientWaivedAt.UTC(), w.WaivedAt.UTC(), utcPtr(w.RetractedAt)
	return &w, nil
}

func sameWaiver(w *Waiver, by string, req WaiverRequest) bool {
	return w.WaivedBy == by && w.InspectionID == req.InspectionID && w.ItemKey == req.ItemKey && w.Reason == req.Reason &&
		w.ClientWaivedAt.Equal(req.ClientWaivedAt) && equalPtr(w.DeviceID, req.DeviceID)
}

func normaliseWaiver(req *WaiverRequest, now time.Time) error {
	id, err := uuid.Parse(req.InspectionID)
	if err != nil || req.ItemKey == "" || len(req.ItemKey) > 200 {
		return ErrBadFinding
	}
	req.InspectionID = id.String()
	if !textOK(&req.Reason, 500) {
		return ErrBadReason
	}
	if req.ClientWaivedAt.IsZero() {
		return ErrNoWaivedAt
	}
	req.ClientWaivedAt = micro(req.ClientWaivedAt)
	if req.ClientWaivedAt.After(now.Add(clockSkew)) {
		return ErrWaivedInFuture
	}
	return checkDevice(req.DeviceID)
}
