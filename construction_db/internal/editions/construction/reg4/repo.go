package reg4

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type header struct {
	ID, SiteID, RegimeID, CreatedBy string
	CreatedAt                       time.Time
	LatestVersion                   int
}

func getHeader(ctx context.Context, db DBTX, orgID, siteID, id string) (*header, error) {
	return readHeader(ctx, db, orgID, siteID, id, "")
}

// lockHeader takes the lock a new version's insert takes (000045's UPDATE of
// latest_version), so an amendment and a satisfaction queue.
func lockHeader(ctx context.Context, db DBTX, orgID, siteID, id string) (*header, error) {
	return readHeader(ctx, db, orgID, siteID, id, " FOR NO KEY UPDATE")
}

func readHeader(ctx context.Context, db DBTX, orgID, siteID, id, lock string) (*header, error) {
	var h header
	err := db.QueryRow(ctx, `
		SELECT id, site_id, regime_id, created_by, created_at, latest_version
		FROM site_work WHERE org_id = $1 AND site_id = $2 AND id = $3`+lock, orgID, siteID, id).
		Scan(&h.ID, &h.SiteID, &h.RegimeID, &h.CreatedBy, &h.CreatedAt, &h.LatestVersion)
	if err != nil {
		return nil, fmt.Errorf("get site work: %w", err)
	}
	h.CreatedAt = h.CreatedAt.UTC()
	return &h, nil
}

func onAnotherSite(ctx context.Context, db DBTX, orgID, id string) (bool, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM site_work WHERE org_id = $1 AND id = $2`, orgID, id).Scan(&n)
	return n > 0, err
}

func siteArchivedAt(ctx context.Context, db DBTX, orgID, siteID string) (*time.Time, error) {
	var at *time.Time
	err := db.QueryRow(ctx, `SELECT CASE WHEN status = 'archived' THEN archived_at END FROM sites WHERE org_id = $1 AND id = $2`,
		orgID, siteID).Scan(&at)
	if err != nil {
		return nil, fmt.Errorf("site archived_at: %w", err)
	}
	return at, nil
}

func insertHeader(ctx context.Context, db DBTX, orgID, siteID, regimeID, id, by string) error {
	_, err := db.Exec(ctx, `INSERT INTO site_work (id, org_id, site_id, regime_id, created_by) VALUES ($1, $2, $3, $4, $5)`,
		id, orgID, siteID, regimeID, by)
	return err
}

func insertVersion(ctx context.Context, db DBTX, orgID, siteID, id string, n int, by string, r VersionRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO site_work_versions (org_id, site_id, site_work_id, version, description, starts_on, ends_on, facts, status,
		                                submitted_by, client_submitted_at, device_id, amend_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		orgID, siteID, id, n, r.Description, r.StartsOn, r.EndsOn, r.Facts, r.Status, by, r.ClientSubmittedAt, r.DeviceID, r.AmendReason)
	return err
}

type storedVersion struct {
	Version
	FactKeys []string
}

func getVersion(ctx context.Context, db DBTX, orgID, id string, n int) (*storedVersion, error) {
	var v storedVersion
	err := db.QueryRow(ctx, `
		SELECT version, description, starts_on::text, ends_on::text, facts, status, submitted_by, client_submitted_at,
		       submitted_at, device_id, amend_reason
		FROM site_work_versions WHERE org_id = $1 AND site_work_id = $2 AND version = $3`, orgID, id, n).
		Scan(&v.Version.Version, &v.Description, &v.StartsOn, &v.EndsOn, &v.FactKeys, &v.Status, &v.SubmittedBy,
			&v.ClientSubmittedAt, &v.SubmittedAt, &v.DeviceID, &v.AmendReason)
	if err != nil {
		return nil, fmt.Errorf("get site work version: %w", err)
	}
	if v.FactKeys == nil {
		v.FactKeys = []string{}
	}
	v.ClientSubmittedAt, v.SubmittedAt = v.ClientSubmittedAt.UTC(), v.SubmittedAt.UTC()
	return &v, nil
}

// listSummaries: the site's records as of their latest versions, the next
// start first, with their live deadlines not met yet.
func listSummaries(ctx context.Context, db DBTX, orgID, siteID, status string, now time.Time, limit int) ([]Summary, error) {
	rows, err := db.Query(ctx, `
		SELECT w.id, w.site_id, w.latest_version, v.description, v.starts_on::text, v.ends_on::text, v.facts, v.status, w.created_at,
		       count(d.id) FILTER (WHERE du.satisfaction_id IS NULL),
		       count(d.id) FILTER (WHERE du.satisfaction_id IS NULL AND d.due_at <= $4),
		       min(d.due_at) FILTER (WHERE du.satisfaction_id IS NULL)
		FROM site_work w
		JOIN site_work_versions v ON v.site_work_id = w.id AND v.version = w.latest_version
		LEFT JOIN deadlines d ON d.site_work_id = w.id AND d.closed_as IS NULL
		LEFT JOIN deadline_duties du ON du.id = d.duty_id
		WHERE w.org_id = $1 AND w.site_id = $2 AND ($3 = '' OR v.status = $3)
		GROUP BY w.id, v.site_work_id, v.version
		ORDER BY v.starts_on, w.created_at, w.id
		LIMIT $5`, orgID, siteID, status, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list site work: %w", err)
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.SiteID, &s.LatestVersion, &s.Description, &s.StartsOn, &s.EndsOn, &s.Facts, &s.Status,
			&s.CreatedAt, &s.OpenDeadlines, &s.OverdueDeadlines, &s.NextDueAt); err != nil {
			return nil, fmt.Errorf("scan site work: %w", err)
		}
		if s.Facts == nil {
			s.Facts = []string{}
		}
		s.CreatedAt = s.CreatedAt.UTC()
		if s.NextDueAt != nil {
			u := s.NextDueAt.UTC()
			s.NextDueAt = &u
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
