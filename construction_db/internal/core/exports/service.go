package exports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/storage"
)

// CanSeeHealthDetails is the one D8 rule; DEPLOY's decision narrows it here.
func CanSeeHealthDetails(role string) bool {
	return role == auth.RoleOwner || role == auth.RoleSHEOfficer
}

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	store storage.Store // the exports bucket
	Clock clock.Clock   // nil is the real time
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service, exportStore storage.Store) *Service {
	return &Service{pool: pool, sites: sitesSvc, store: exportStore}
}

const futureLeeway = 5 * time.Minute

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

// validRange: inclusive local dates, to not before from, at most MaxDays.
func validRange(from, to string) bool {
	f, err1 := time.Parse(time.DateOnly, from)
	t, err2 := time.Parse(time.DateOnly, to)
	return err1 == nil && err2 == nil && !t.Before(f) && t.Sub(f) < MaxDays*24*time.Hour
}

func (s *Service) Queue(ctx context.Context, orgID, userID, role, siteID, exportID string, req Request) (*Export, bool, error) {
	id, err := uuid.Parse(exportID)
	if err != nil {
		return nil, false, ErrBadID
	}
	if !validRange(req.From, req.To) {
		return nil, false, ErrBadRange
	}
	if req.ClientRequestedAt.IsZero() || req.ClientRequestedAt.After(s.Clock.Now().Add(futureLeeway)) {
		return nil, false, ErrBadRequestedAt
	}
	req.ClientRequestedAt = req.ClientRequestedAt.UTC().Truncate(time.Microsecond)
	if req.DeviceID != nil && (len(*req.DeviceID) == 0 || len(*req.DeviceID) > 200) {
		return nil, false, ErrBadDeviceID
	}
	for try := 0; ; try++ {
		out, created, err := s.queueOnce(ctx, orgID, userID, role, siteID, id.String(), req)
		if errors.Is(err, errIDTaken) && try == 0 {
			continue // a concurrent identical PUT committed first: answer as a repeat
		}
		if errors.Is(err, errIDTaken) {
			err = ErrIDReused
		}
		return out, created, finish(err, "queue export")
	}
}

var errIDTaken = errors.New("export id taken")

func (s *Service) queueOnce(ctx context.Context, orgID, userID, role, siteID, id string, req Request) (*Export, bool, error) {
	var (
		out     *Export
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if e, err := getExport(ctx, tx, orgID, "", id, s.Clock.Now()); err == nil {
			if e.SiteID != sr.Site.ID || e.From != req.From || e.To != req.To || e.RequestedBy != userID ||
				!e.ClientRequestedAt.Equal(req.ClientRequestedAt) || !equalPtr(e.DeviceID, req.DeviceID) {
				return ErrIDReused
			}
			out = e
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		sum := sr.Data.SHA256()
		_, err = tx.Exec(ctx, `
			INSERT INTO audit_exports (id, org_id, site_id, requested_by, requester_role, from_date, to_date, time_zone, as_of,
			                           pack_format, regime_code, regime_version, regime_data_sha256, includes_health,
			                           client_requested_at, device_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, clock_timestamp(), $9, $10, $11, $12, $13, $14, $15)`,
			id, orgID, sr.Site.ID, userID, role, req.From, req.To, sr.Data.Regime.TimeZone, PackFormat,
			sr.Data.Regime.Code, sr.Data.Regime.Version, sum[:], CanSeeHealthDetails(role), req.ClientRequestedAt, req.DeviceID)
		if isUnique(err, "audit_exports_pkey") {
			return errIDTaken
		}
		if err != nil {
			return err
		}
		created = true
		out, err = getExport(ctx, tx, orgID, sr.Site.ID, id, s.Clock.Now())
		return err
	})
	return out, created, err
}

// Get is one export of the site, with its status.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, exportID string) (*Export, error) {
	var out *Export
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = getExport(ctx, tx, orgID, site.ID, exportID, s.Clock.Now())
		return err
	})
	return out, finish(err, "get export")
}

// List is the site's exports, newest first.
func (s *Service) List(ctx context.Context, orgID, userID, role, siteID string) ([]Export, error) {
	var out []Export
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = listExports(ctx, tx, orgID, site.ID, "", s.Clock.Now())
		return err
	})
	return out, finish(err, "list exports")
}

// Download signs the link only after the log rows commit.
func (s *Service) Download(ctx context.Context, orgID, userID, role, siteID, exportID string, client evidence.Client) (*Download, error) {
	var e *Export
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if e, err = getExport(ctx, tx, orgID, site.ID, exportID, s.Clock.Now()); err != nil {
			return err
		}
		switch e.Status {
		case StatusFailed:
			return ErrFailed
		case StatusExpired:
			return ErrExpired
		case StatusQueued, StatusBuilding:
			return ErrNotReady
		}
		ip, ua := client.LogIP(), client.LogUserAgent()
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_export_downloads (org_id, export_id, user_id, ip, user_agent) VALUES ($1, $2, $3, $4, $5)`,
			orgID, e.ID, userID, ip, ua); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO evidence_access_log (org_id, site_id, evidence_id, user_id, action, ip, user_agent, export_id)
			SELECT x.org_id, x.site_id, x.evidence_id, $2, 'export', $3, $4, x.export_id
			FROM audit_export_evidence x WHERE x.export_id = $1 ORDER BY x.evidence_id`, e.ID, userID, ip, ua); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO file_access_log (org_id, file_id, user_id, action, object_version_id, ip, user_agent, export_id)
			SELECT f.org_id, f.id, $2, 'export', f.object_version_id, $3, $4, x.export_id
			FROM audit_export_files x JOIN files f ON f.id = x.file_id WHERE x.export_id = $1 ORDER BY f.id`, e.ID, userID, ip, ua)
		return err
	})
	if err = finish(err, "download export"); err != nil {
		return nil, err
	}
	url, err := s.store.PresignGet(ctx, *e.objectKey, *e.objectVersion, ContentType, DownloadTTL)
	if err != nil {
		return nil, fmt.Errorf("download export: %w", err)
	}
	return &Download{Method: url.Method, URL: url.URL, ExpiresAt: url.ExpiresAt, SHA256: *e.SHA256, SizeBytes: *e.SizeBytes}, nil
}

const exportSQL = `
	SELECT e.id::text, e.site_id::text, e.from_date::text, e.to_date::text, e.time_zone, e.as_of, e.requested_by::text,
	       e.requester_role, e.includes_health, e.pack_format, e.regime_code, e.regime_version, encode(e.regime_data_sha256, 'hex'),
	       e.client_requested_at, e.device_id,
	       (SELECT count(*)::int FROM audit_export_attempts a WHERE a.export_id = e.id),
	       (SELECT count(*)::int FROM audit_export_attempts a WHERE a.export_id = e.id
	          AND (a.outcome IN ('failed', 'lapsed') OR (a.finished_at IS NULL AND a.lease_until <= $4))),
	       EXISTS (SELECT 1 FROM audit_export_attempts a WHERE a.export_id = e.id AND a.finished_at IS NULL AND a.lease_until > $4),
	       (SELECT a.error FROM audit_export_attempts a WHERE a.export_id = e.id AND a.error IS NOT NULL ORDER BY a.attempt DESC LIMIT 1),
	       encode(r.sha256, 'hex'), r.size_bytes, r.evidence_count, r.file_count, r.chain_length, encode(r.chain_head, 'hex'),
	       r.chain_intact, r.finished_at, r.expires_at, r.object_key, r.object_version_id
	FROM audit_exports e LEFT JOIN audit_export_results r ON r.export_id = e.id
	WHERE e.org_id = $1 AND ($2 = '' OR e.site_id::text = $2) AND ($3 = '' OR e.id::text = $3)
	ORDER BY e.as_of DESC, e.id`

func listExports(ctx context.Context, db pgx.Tx, orgID, siteID, id string, now time.Time) ([]Export, error) {
	rows, err := db.Query(ctx, exportSQL, orgID, siteID, id, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Export{}
	for rows.Next() {
		var e Export
		var live bool
		if err := rows.Scan(&e.ID, &e.SiteID, &e.From, &e.To, &e.TimeZone, &e.AsOf, &e.RequestedBy, &e.RequesterRole,
			&e.IncludesHealth, &e.PackFormat, &e.RegimeCode, &e.RegimeVersion, &e.RegimeDataSHA256, &e.ClientRequestedAt,
			&e.DeviceID, &e.Attempts, &e.FailedAttempts, &live, &e.LastError, &e.SHA256, &e.SizeBytes, &e.EvidenceCount,
			&e.FileCount, &e.ChainLength, &e.ChainHead, &e.ChainIntact, &e.FinishedAt, &e.ExpiresAt,
			&e.objectKey, &e.objectVersion); err != nil {
			return nil, err
		}
		e.Status = statusOf(e, live, now)
		out = append(out, e)
	}
	return out, rows.Err()
}

func getExport(ctx context.Context, db pgx.Tx, orgID, siteID, id string, now time.Time) (*Export, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrNotFound
	}
	list, err := listExports(ctx, db, orgID, siteID, id, now)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

func statusOf(e Export, live bool, now time.Time) string {
	switch {
	case e.ExpiresAt != nil && !now.Before(*e.ExpiresAt):
		return StatusExpired
	case e.SHA256 != nil:
		return StatusReady
	case e.FailedAttempts >= MaxAttempts:
		return StatusFailed
	case live:
		return StatusBuilding
	}
	return StatusQueued
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func isSentinel(err error) bool {
	for _, s := range Sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

func finish(err error, prefix string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sites.ErrSiteNotFound):
		return ErrSiteNotFound
	case isSentinel(err):
		return err
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
