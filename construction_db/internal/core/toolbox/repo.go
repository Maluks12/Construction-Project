package toolbox

import (
	"context"
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

// isRule: the database refused the row with a guard's check_violation.
func isRule(err error, fragment string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, fragment)
}

// headerSQL selects TalkHeader; signed_count leaves out retracted signatures.
const headerSQL = `
	SELECT t.id, t.site_id, t.topic_key, t.topic, t.presenter_name, t.notes, t.started_at, t.ended_at, t.talk_date::text,
	       t.time_zone, t.recorded_by, t.device_id, t.created_at, t.updated_at,
	       (SELECT count(*) FROM toolbox_attendance a
	        WHERE a.talk_id = t.id AND NOT EXISTS (SELECT 1 FROM toolbox_attendance_retractions r WHERE r.attendance_id = a.id))::int,
	       (SELECT count(*) FROM toolbox_talk_evidence e WHERE e.talk_id = t.id)::int
	FROM toolbox_talks t`

func scanHeader(row pgx.Row) (*TalkSummary, error) {
	var s TalkSummary
	h := &s.TalkHeader
	err := row.Scan(&h.ID, &h.SiteID, &h.TopicKey, &h.Topic, &h.PresenterName, &h.Notes, &h.StartedAt, &h.EndedAt, &h.TalkDate,
		&h.TimeZone, &h.RecordedBy, &h.DeviceID, &h.CreatedAt, &h.UpdatedAt, &h.SignedCount, &s.PhotoCount)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func getHeader(ctx context.Context, db DBTX, orgID, siteID, id string) (*TalkSummary, error) {
	return scanHeader(db.QueryRow(ctx, headerSQL+` WHERE t.org_id = $1 AND t.site_id = $2 AND t.id = $3`, orgID, siteID, id))
}

// lockTalk reads a talk FOR NO KEY UPDATE, the lock its UPDATE takes: a
// signature (FOR SHARE on the talk) then waits for this transaction, and an
// open signature makes this wait.
func lockTalk(ctx context.Context, db DBTX, orgID, siteID, id string) (*TalkSummary, error) {
	if _, err := db.Exec(ctx, `SELECT 1 FROM toolbox_talks WHERE org_id = $1 AND site_id = $2 AND id = $3 FOR NO KEY UPDATE`, orgID, siteID, id); err != nil {
		return nil, err
	}
	return getHeader(ctx, db, orgID, siteID, id)
}

// talkSite is the site of the talk with this id, on any site of the org.
func talkSite(ctx context.Context, db DBTX, orgID, id string) (string, error) {
	var site string
	err := db.QueryRow(ctx, `SELECT site_id FROM toolbox_talks WHERE org_id = $1 AND id = $2`, orgID, id).Scan(&site)
	return site, err
}

// hasSignatures counts retracted ones too: a retraction doesn't unfreeze a talk.
func hasSignatures(ctx context.Context, db DBTX, talkID string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM toolbox_attendance WHERE talk_id = $1)`, talkID).Scan(&ok)
	return ok, err
}

type talkRow struct {
	TopicKey      *string
	Topic         string
	PresenterName string
	Notes         *string
	StartedAt     time.Time
	EndedAt       *time.Time
}

func insertTalk(ctx context.Context, db DBTX, orgID, siteID, id, by, zone string, r talkRow, deviceID *string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO toolbox_talks (id, org_id, site_id, topic_key, topic, presenter_name, notes, started_at, ended_at, time_zone, recorded_by, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		id, orgID, siteID, r.TopicKey, r.Topic, r.PresenterName, r.Notes, r.StartedAt, r.EndedAt, zone, by, deviceID)
	if err != nil {
		return fmt.Errorf("insert talk: %w", err)
	}
	return nil
}

func updateTalk(ctx context.Context, db DBTX, orgID, id string, r talkRow) error {
	_, err := db.Exec(ctx, `
		UPDATE toolbox_talks SET topic_key = $3, topic = $4, presenter_name = $5, notes = $6, started_at = $7, ended_at = $8
		WHERE org_id = $1 AND id = $2`, orgID, id, r.TopicKey, r.Topic, r.PresenterName, r.Notes, r.StartedAt, r.EndedAt)
	if err != nil {
		return fmt.Errorf("update talk: %w", err)
	}
	return nil
}

type listFilter struct {
	SiteID string
	Date   string // YYYY-MM-DD or ""
	Limit  int
}

func listTalks(ctx context.Context, db DBTX, orgID string, f listFilter) ([]TalkSummary, error) {
	rows, err := db.Query(ctx, headerSQL+`
		WHERE t.org_id = $1 AND t.site_id = $2 AND ($3 = '' OR t.talk_date = NULLIF($3, '')::date)
		ORDER BY t.started_at DESC, t.id
		LIMIT $4`, orgID, f.SiteID, f.Date, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list talks: %w", err)
	}
	defer rows.Close()
	out := []TalkSummary{}
	for rows.Next() {
		s, err := scanHeader(rows)
		if err != nil {
			return nil, fmt.Errorf("list talks: %w", err)
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list talks: %w", err)
	}
	return out, nil
}

func listPhotos(ctx context.Context, db DBTX, orgID, talkID string) ([]TalkPhoto, error) {
	rows, err := db.Query(ctx, `
		SELECT evidence_id, kind, added_by, added_at FROM toolbox_talk_evidence
		WHERE org_id = $1 AND talk_id = $2 ORDER BY added_at, evidence_id`, orgID, talkID)
	if err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}
	defer rows.Close()
	out := []TalkPhoto{}
	for rows.Next() {
		var p TalkPhoto
		if err := rows.Scan(&p.EvidenceID, &p.Kind, &p.AddedBy, &p.AddedAt); err != nil {
			return nil, fmt.Errorf("list photos: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}
	return out, nil
}

const attendanceSQL = `
	SELECT a.id, a.talk_id, a.name, a.firm, a.clock_number, a.method, a.recorded_by, a.client_signed_at, a.signed_at, a.device_id,
	       r.reason, r.retracted_by, r.client_retracted_at, r.retracted_at,
	       (SELECT substr(p.person, 3) FROM (SELECT toolbox_attendance_person(a.id) AS person) p WHERE p.person LIKE 'w:%')
	FROM toolbox_attendance a
	LEFT JOIN toolbox_attendance_retractions r ON r.attendance_id = a.id`

func scanAttendance(row pgx.Row) (*Attendance, error) {
	var (
		a           Attendance
		reason, by  *string
		client, srv *time.Time
	)
	err := row.Scan(&a.ID, &a.TalkID, &a.Name, &a.Firm, &a.ClockNumber, &a.Method, &a.RecordedBy, &a.ClientSignedAt, &a.SignedAt, &a.DeviceID,
		&reason, &by, &client, &srv, &a.WorkerID)
	if err != nil {
		return nil, err
	}
	if reason != nil {
		a.Retraction = &Retraction{Reason: *reason, RetractedBy: *by, ClientRetractedAt: *client, RetractedAt: *srv}
	}
	return &a, nil
}

func getAttendance(ctx context.Context, db DBTX, orgID, siteID, id string) (*Attendance, error) {
	return scanAttendance(db.QueryRow(ctx, attendanceSQL+` WHERE a.org_id = $1 AND a.site_id = $2 AND a.id = $3`, orgID, siteID, id))
}

// attendanceAnywhere finds an attendance id on any site of the org.
func attendanceAnywhere(ctx context.Context, db DBTX, orgID, id string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM toolbox_attendance WHERE org_id = $1 AND id = $2)`, orgID, id).Scan(&ok)
	return ok, err
}

func listAttendance(ctx context.Context, db DBTX, orgID, talkID string) ([]Attendance, error) {
	rows, err := db.Query(ctx, attendanceSQL+` WHERE a.org_id = $1 AND a.talk_id = $2 ORDER BY a.client_signed_at, a.id`, orgID, talkID)
	if err != nil {
		return nil, fmt.Errorf("list attendance: %w", err)
	}
	defer rows.Close()
	out := []Attendance{}
	for rows.Next() {
		a, err := scanAttendance(rows)
		if err != nil {
			return nil, fmt.Errorf("list attendance: %w", err)
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list attendance: %w", err)
	}
	return out, nil
}

// sameIdentity: someone with this key (IdentityKey) already signed the talk.
func sameIdentity(ctx context.Context, db DBTX, talkID, key string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM toolbox_attendance WHERE talk_id = $1 AND identity_key = $2)`,
		talkID, key).Scan(&ok)
	return ok, err
}

// signedAfter: a signature that stands (not retracted) was given after at.
func signedAfter(ctx context.Context, db DBTX, talkID string, at time.Time) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM toolbox_attendance a WHERE a.talk_id = $1 AND a.client_signed_at > $2
		    AND NOT EXISTS (SELECT 1 FROM toolbox_attendance_retractions r WHERE r.attendance_id = a.id))`,
		talkID, at).Scan(&ok)
	return ok, err
}

func insertAttendance(ctx context.Context, db DBTX, orgID, siteID, talkID, id, by string, req SignRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO toolbox_attendance (id, org_id, site_id, talk_id, name, firm, clock_number, method, recorded_by, client_signed_at, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, orgID, siteID, talkID, req.Name, req.Firm, req.ClockNumber, req.Method, by, req.ClientSignedAt, req.DeviceID)
	return err
}

func insertRetraction(ctx context.Context, db DBTX, orgID, siteID, attendanceID, by string, req RetractRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO toolbox_attendance_retractions (attendance_id, org_id, site_id, reason, retracted_by, client_retracted_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, attendanceID, orgID, siteID, req.Reason, by, req.ClientRetractedAt)
	return err
}

func hasRegisterPhoto(ctx context.Context, db DBTX, talkID string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM toolbox_talk_evidence WHERE talk_id = $1 AND kind = 'paper_register')`, talkID).Scan(&ok)
	return ok, err
}

// photoKind is the kind a photo was added to the talk as, if it was.
func photoKind(ctx context.Context, db DBTX, talkID, evidenceID string) (string, error) {
	var kind string
	err := db.QueryRow(ctx, `SELECT kind FROM toolbox_talk_evidence WHERE talk_id = $1 AND evidence_id = $2`, talkID, evidenceID).Scan(&kind)
	return kind, err
}

func evidenceStatus(ctx context.Context, db DBTX, orgID, siteID, id string) (string, error) {
	var status string
	err := db.QueryRow(ctx, `SELECT status FROM evidence WHERE org_id = $1 AND site_id = $2 AND id = $3`, orgID, siteID, id).Scan(&status)
	return status, err
}

func insertPhoto(ctx context.Context, db DBTX, orgID, siteID, talkID, evidenceID, kind, by string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO toolbox_talk_evidence (org_id, site_id, talk_id, evidence_id, kind, added_by) VALUES ($1, $2, $3, $4, $5, $6)`,
		orgID, siteID, talkID, evidenceID, kind, by)
	return err
}

// autoLink links a new signature to the register worker with the same
// identity key, if there is one (M11 D7). The signature itself never changes.
func autoLink(ctx context.Context, db DBTX, orgID, siteID, attendanceID, by string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO toolbox_attendance_workers (org_id, site_id, attendance_id, worker_id, method, linked_by)
		SELECT $1, $2, a.id, w.id, $5, $4
		FROM toolbox_attendance a JOIN workers w ON w.org_id = a.org_id AND w.identity_key = a.identity_key
		WHERE a.id = $3`, orgID, siteID, attendanceID, by, linkAuto)
	return err
}

const linkAuto = "auto"

// listNotSigned: the workers signed in on the talk's site on its day, before
// it ended (or now), who aren't the person of any standing signature of it.
func listNotSigned(ctx context.Context, db DBTX, orgID, talkID string) ([]NotSigned, error) {
	rows, err := db.Query(ctx, notSignedSQL, orgID, talkID)
	if err != nil {
		return nil, fmt.Errorf("list not signed: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (NotSigned, error) {
		var n NotSigned
		err := r.Scan(&n.WorkerID, &n.Name, &n.Firm, &n.ClockNumber, &n.SignInID)
		return n, err
	})
	if err != nil {
		return nil, fmt.Errorf("list not signed: %w", err)
	}
	return out, nil
}
