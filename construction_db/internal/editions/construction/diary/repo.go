package diary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func isRule(err error, fragment string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, fragment)
}

// headerSQL selects DiaryHeader and the head revision's id; closed is judged
// by the server's clock now.
const headerSQL = `
	SELECT d.site_id, d.diary_date::text, d.time_zone, d.opens_at, d.closes_at, d.closes_at + site_diary_grace(), now() >= d.closes_at,
	       d.late, d.created_by, d.created_at, d.revision_count, d.conflict_revision_ids::text[],
	       (SELECT count(*) FROM site_diary_addenda a WHERE a.site_id = d.site_id AND a.diary_date = d.diary_date)::int,
	       d.head_revision_id
	FROM site_diaries d`

func scanHeader(row pgx.Row) (*DiaryHeader, string, error) {
	var (
		h    DiaryHeader
		head *string // null until the first revision lands
	)
	err := row.Scan(&h.SiteID, &h.DiaryDate, &h.TimeZone, &h.OpensAt, &h.ClosesAt, &h.GraceEndsAt, &h.Closed, &h.Late, &h.CreatedBy, &h.CreatedAt,
		&h.RevisionCount, &h.ConflictRevisionIDs, &h.AddendumCount, &head)
	if err != nil {
		return nil, "", err
	}
	if head == nil {
		return &h, "", nil
	}
	return &h, *head, nil
}

func getHeader(ctx context.Context, db DBTX, orgID, siteID, date string) (*DiaryHeader, string, error) {
	return scanHeader(db.QueryRow(ctx, headerSQL+` WHERE d.org_id = $1 AND d.site_id = $2 AND d.diary_date = $3`, orgID, siteID, date))
}

// lockHeader takes the lock a revision's trigger takes, so the checks below
// it see the diary as the insert will.
func lockHeader(ctx context.Context, db DBTX, orgID, siteID, date string) (*DiaryHeader, error) {
	if _, err := db.Exec(ctx, `SELECT 1 FROM site_diaries WHERE org_id = $1 AND site_id = $2 AND diary_date = $3 FOR NO KEY UPDATE`, orgID, siteID, date); err != nil {
		return nil, err
	}
	h, _, err := getHeader(ctx, db, orgID, siteID, date)
	return h, err
}

// ensureHeader creates the day's diary unless it exists. Two phones creating
// it at once: the second waits for the first, then does nothing.
func ensureHeader(ctx context.Context, db DBTX, orgID, siteID, date, zone, by string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO site_diaries (org_id, site_id, diary_date, time_zone, created_by) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (site_id, diary_date) DO NOTHING`, orgID, siteID, date, zone, by)
	return err
}

const revisionSQL = `
	SELECT r.id, r.site_id, r.diary_date::text, r.seq, r.base_revision_id, r.weather_conditions, r.weather_note, r.headcount,
	       r.headcount_suggested, r.work_done, r.sections,
	       COALESCE((SELECT array_agg(e.evidence_id::text ORDER BY e.evidence_id) FROM site_diary_revision_evidence e WHERE e.revision_id = r.id), '{}'),
	       r.saved_by, r.client_saved_at, r.server_received_at, r.saved_after_close, r.received_after_close, r.late_reason, r.device_id
	FROM site_diary_revisions r`

func scanRevision(row pgx.Row) (*Revision, error) {
	var (
		r        Revision
		sections []byte
	)
	err := row.Scan(&r.ID, &r.SiteID, &r.DiaryDate, &r.Seq, &r.BaseRevisionID, &r.Weather.Conditions, &r.Weather.Note, &r.Headcount,
		&r.HeadcountSuggested, &r.WorkDone, &sections, &r.EvidenceIDs, &r.SavedBy, &r.ClientSavedAt, &r.ServerReceivedAt,
		&r.SavedAfterClose, &r.ReceivedAfterClose, &r.LateReason, &r.DeviceID)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sections, &r.Sections); err != nil {
		return nil, fmt.Errorf("revision %s sections: %w", r.ID, err)
	}
	return &r, nil
}

// getRevision finds a revision id on any site of the org.
func getRevision(ctx context.Context, db DBTX, orgID, id string) (*Revision, error) {
	return scanRevision(db.QueryRow(ctx, revisionSQL+` WHERE r.org_id = $1 AND r.id = $2`, orgID, id))
}

func listRevisions(ctx context.Context, db DBTX, orgID, siteID, date string) ([]Revision, error) {
	rows, err := db.Query(ctx, revisionSQL+` WHERE r.org_id = $1 AND r.site_id = $2 AND r.diary_date = $3 ORDER BY r.seq`, orgID, siteID, date)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		r, err := scanRevision(rows)
		if err != nil {
			return nil, fmt.Errorf("list revisions: %w", err)
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	return out, nil
}

func insertRevision(ctx context.Context, db DBTX, orgID, siteID, date, id, by string, req RevisionRequest, sections []byte) error {
	_, err := db.Exec(ctx, `
		INSERT INTO site_diary_revisions (id, org_id, site_id, diary_date, base_revision_id, weather_conditions, weather_note, headcount,
		                                  work_done, sections, evidence_count, saved_by, client_saved_at, late_reason, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		id, orgID, siteID, date, req.BaseRevisionID, req.Weather.Conditions, req.Weather.Note, *req.Headcount, req.WorkDone, sections,
		len(req.EvidenceIDs), by, req.ClientSaved, req.LateReason, req.DeviceID)
	if err != nil {
		return err
	}
	for _, ev := range req.EvidenceIDs {
		if _, err := db.Exec(ctx, `
			INSERT INTO site_diary_revision_evidence (org_id, site_id, diary_date, revision_id, evidence_id) VALUES ($1, $2, $3, $4, $5)`,
			orgID, siteID, date, id, ev); err != nil {
			return err
		}
	}
	return nil
}

// isRevisionOf: base is a revision of this diary.
func isRevisionOf(ctx context.Context, db DBTX, siteID, date, base string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM site_diary_revisions WHERE site_id = $1 AND diary_date = $2 AND id = $3)`,
		siteID, date, base).Scan(&ok)
	return ok, err
}

const addendumSQL = `
	SELECT a.id, a.site_id, a.diary_date::text, a.reason, a.note, a.sections,
	       COALESCE((SELECT array_agg(e.evidence_id::text ORDER BY e.evidence_id) FROM site_diary_addendum_evidence e WHERE e.addendum_id = a.id), '{}'),
	       a.added_by, a.client_added_at, a.server_received_at, a.device_id
	FROM site_diary_addenda a`

func scanAddendum(row pgx.Row) (*Addendum, error) {
	var (
		a        Addendum
		sections []byte
	)
	err := row.Scan(&a.ID, &a.SiteID, &a.DiaryDate, &a.Reason, &a.Note, &sections, &a.EvidenceIDs, &a.AddedBy, &a.ClientAddedAt,
		&a.ServerReceivedAt, &a.DeviceID)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sections, &a.Sections); err != nil {
		return nil, fmt.Errorf("addendum %s sections: %w", a.ID, err)
	}
	return &a, nil
}

func getAddendum(ctx context.Context, db DBTX, orgID, id string) (*Addendum, error) {
	return scanAddendum(db.QueryRow(ctx, addendumSQL+` WHERE a.org_id = $1 AND a.id = $2`, orgID, id))
}

func listAddenda(ctx context.Context, db DBTX, orgID, siteID, date string) ([]Addendum, error) {
	rows, err := db.Query(ctx, addendumSQL+` WHERE a.org_id = $1 AND a.site_id = $2 AND a.diary_date = $3 ORDER BY a.server_received_at, a.id`,
		orgID, siteID, date)
	if err != nil {
		return nil, fmt.Errorf("list addenda: %w", err)
	}
	defer rows.Close()
	out := []Addendum{}
	for rows.Next() {
		a, err := scanAddendum(rows)
		if err != nil {
			return nil, fmt.Errorf("list addenda: %w", err)
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list addenda: %w", err)
	}
	return out, nil
}

func insertAddendum(ctx context.Context, db DBTX, orgID, siteID, date, id, by string, req AddendumRequest, sections []byte) error {
	_, err := db.Exec(ctx, `
		INSERT INTO site_diary_addenda (id, org_id, site_id, diary_date, reason, note, sections, evidence_count, added_by, client_added_at, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, orgID, siteID, date, req.Reason, req.Note, sections, len(req.EvidenceIDs), by, req.ClientAdded, req.DeviceID)
	if err != nil {
		return err
	}
	for _, ev := range req.EvidenceIDs {
		if _, err := db.Exec(ctx, `
			INSERT INTO site_diary_addendum_evidence (org_id, site_id, diary_date, addendum_id, evidence_id) VALUES ($1, $2, $3, $4, $5)`,
			orgID, siteID, date, id, ev); err != nil {
			return err
		}
	}
	return nil
}

func evidenceStatuses(ctx context.Context, db DBTX, orgID, siteID string, ids []string) (map[string]string, error) {
	rows, err := db.Query(ctx, `SELECT id::text, status FROM evidence WHERE org_id = $1 AND site_id = $2 AND id = ANY($3::uuid[])`, orgID, siteID, ids)
	if err != nil {
		return nil, fmt.Errorf("evidence statuses: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("evidence statuses: %w", err)
		}
		out[id] = status
	}
	return out, rows.Err()
}

func headcountNow(ctx context.Context, db DBTX, siteID, date string) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT toolbox_headcount($1, $2)`, siteID, date).Scan(&n)
	return n, err
}

func ids(ctx context.Context, db DBTX, sql string, args ...any) ([]string, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// linked: the day's toolbox talks and incidents (latest version's
// occurred_at, on the diary's local date), by id.
func linked(ctx context.Context, db DBTX, orgID, siteID, date, zone string) (talks, incidents []string, err error) {
	talks, err = ids(ctx, db, `SELECT id::text FROM toolbox_talks WHERE org_id = $1 AND site_id = $2 AND talk_date = $3 ORDER BY started_at, id`,
		orgID, siteID, date)
	if err != nil {
		return nil, nil, fmt.Errorf("linked talks: %w", err)
	}
	incidents, err = ids(ctx, db, `
		SELECT i.id::text FROM incidents i
		JOIN incident_versions v ON v.incident_id = i.id AND v.version = i.latest_version
		WHERE i.org_id = $1 AND i.site_id = $2 AND (v.occurred_at AT TIME ZONE $4)::date = $3
		ORDER BY v.occurred_at, i.id`, orgID, siteID, date, zone)
	if err != nil {
		return nil, nil, fmt.Errorf("linked incidents: %w", err)
	}
	return talks, incidents, nil
}

func listDiaries(ctx context.Context, db DBTX, orgID, siteID, from, to string, limit int) ([]DiarySummary, error) {
	rows, err := db.Query(ctx, `
		SELECT h.*, r.weather_conditions, r.weather_note, r.headcount, r.headcount_suggested
		FROM (`+headerSQL+`
		      WHERE d.org_id = $1 AND d.site_id = $2 AND ($3 = '' OR d.diary_date >= NULLIF($3, '')::date)
		        AND ($4 = '' OR d.diary_date <= NULLIF($4, '')::date)) h
		JOIN site_diary_revisions r ON r.id = h.head_revision_id
		ORDER BY h.diary_date DESC
		LIMIT $5`, orgID, siteID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("list diaries: %w", err)
	}
	defer rows.Close()
	out := []DiarySummary{}
	for rows.Next() {
		var s DiarySummary
		h := &s.DiaryHeader
		if err := rows.Scan(&h.SiteID, &h.DiaryDate, &h.TimeZone, &h.OpensAt, &h.ClosesAt, &h.GraceEndsAt, &h.Closed, &h.Late, &h.CreatedBy,
			&h.CreatedAt, &h.RevisionCount, &h.ConflictRevisionIDs, &h.AddendumCount, &s.HeadRevisionID,
			&s.Weather.Conditions, &s.Weather.Note, &s.Headcount, &s.HeadcountSuggested); err != nil {
			return nil, fmt.Errorf("list diaries: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list diaries: %w", err)
	}
	return out, nil
}

// firms: firm names used on the site before, in diaries and toolbox talks,
// for the phone's autocomplete (D9).
func firms(ctx context.Context, db DBTX, orgID, siteID string) ([]string, error) {
	out, err := ids(ctx, db, `
		SELECT DISTINCT firm FROM (
		    SELECT btrim(l->>'firm') AS firm
		    FROM site_diary_revisions r, jsonb_array_elements(COALESCE(r.sections->'labour', '[]')) l
		    WHERE r.org_id = $1 AND r.site_id = $2
		    UNION
		    SELECT btrim(firm) FROM toolbox_attendance WHERE org_id = $1 AND site_id = $2 AND firm IS NOT NULL
		) f
		WHERE firm <> ''
		ORDER BY firm
		LIMIT 500`, orgID, siteID)
	if err != nil {
		return nil, fmt.Errorf("firms: %w", err)
	}
	return out, nil
}

// dayOf is the local day of date in zone.
func dayOf(date, zone string) (day, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return day{}, fmt.Errorf("diary time zone: %w", err)
	}
	d, err := time.ParseInLocation(time.DateOnly, date, loc)
	if err != nil {
		return day{}, ErrBadDate
	}
	return day{opens: d, closes: time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)}, nil
}
