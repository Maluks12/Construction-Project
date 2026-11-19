package workers

import (
	"context"
	"errors"
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

const workerCols = `id::text, name, firm, clock_number, trade, active, created_by::text, created_at, updated_by::text, updated_at`

func scanWorker(row pgx.Row) (*Worker, error) {
	var w Worker
	err := row.Scan(&w.ID, &w.Name, &w.Firm, &w.ClockNumber, &w.Trade, &w.Active, &w.CreatedBy, &w.CreatedAt, &w.UpdatedBy, &w.UpdatedAt)
	return &w, err
}

// GetWorker reads one worker of the tenant's organisation.
func GetWorker(ctx context.Context, db DBTX, orgID, id string) (*Worker, error) {
	return scanWorker(db.QueryRow(ctx, `SELECT `+workerCols+` FROM workers WHERE org_id = $1 AND id = $2`, orgID, id))
}

// WorkerByKey is the register worker with this identity key, if any.
func WorkerByKey(ctx context.Context, db DBTX, orgID, key string) (*Worker, error) {
	return scanWorker(db.QueryRow(ctx, `SELECT `+workerCols+` FROM workers WHERE org_id = $1 AND identity_key = $2`, orgID, key))
}

func listWorkers(ctx context.Context, db DBTX, orgID string, active *bool, q string, limit int) ([]Worker, error) {
	rows, err := db.Query(ctx, `
		SELECT `+workerCols+` FROM workers
		WHERE org_id = $1 AND ($2::boolean IS NULL OR active = $2)
		  AND ($3 = '' OR identity_key LIKE '%' || toolbox_norm($3) || '%')
		ORDER BY lower(name), id LIMIT $4`, orgID, active, q, limit)
	if err != nil {
		return nil, err
	}
	out := []Worker{}
	for rows.Next() {
		w, err := scanWorker(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// InsertWorker adds a worker under the device's id.
func InsertWorker(ctx context.Context, db DBTX, orgID, id, by string, r WorkerRequest) error {
	active := true
	if r.Active != nil {
		active = *r.Active
	}
	_, err := db.Exec(ctx, `
		INSERT INTO workers (id, org_id, name, firm, clock_number, trade, active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, orgID, r.Name, r.Firm, r.ClockNumber, r.Trade, active, by)
	return err
}

func updateWorker(ctx context.Context, db DBTX, orgID, id, by string, r WorkerRequest, active bool) error {
	_, err := db.Exec(ctx, `
		UPDATE workers SET name = $3, firm = $4, clock_number = $5, trade = $6, active = $7, updated_by = $8
		WHERE org_id = $1 AND id = $2`, orgID, id, r.Name, r.Firm, r.ClockNumber, r.Trade, active, by)
	return err
}

func keysOnRegister(ctx context.Context, db DBTX, orgID string, keys []string) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT identity_key FROM workers WHERE org_id = $1 AND identity_key = ANY($2)`, orgID, keys)
	if err != nil {
		return nil, err
	}
	taken, err := pgx.CollectRows(rows, pgx.RowTo[string])
	out := map[string]bool{}
	for _, k := range taken {
		out[k] = true
	}
	return out, err
}

// Records: competencies and medicals share their header columns. The table
// name is always one of the two constants, never input.
const (
	tableCompetencies = "worker_competencies"
	tableMedicals     = "worker_medicals"
)

const headerCols = `r.id::text, r.worker_id::text, r.regime_code, r.file_id::text, r.supersedes_id::text,
	(SELECT s.id::text FROM %[1]s s WHERE s.supersedes_id = r.id AND s.retracted_at IS NULL),
	r.recorded_by::text, r.client_recorded_at, r.recorded_at, r.device_id, r.retracted_at, r.retracted_by::text, r.retract_reason`

type rawHeader struct {
	RecordHeader
	retractedAt *time.Time
	retractedBy *string
	reason      *string
}

func (h *rawHeader) dest() []any {
	return []any{&h.ID, &h.WorkerID, &h.RegimeCode, &h.FileID, &h.SupersedesID, &h.SupersededBy, &h.RecordedBy,
		&h.ClientRecordedAt, &h.RecordedAt, &h.DeviceID, &h.retractedAt, &h.retractedBy, &h.reason}
}

func (h *rawHeader) finish() RecordHeader {
	out := h.RecordHeader
	if h.retractedAt != nil {
		out.Retraction = &Retraction{Reason: *h.reason, RetractedBy: *h.retractedBy, RetractedAt: *h.retractedAt}
	}
	return out
}

func header(table string) string { return strings.ReplaceAll(headerCols, "%[1]s", table) }

const competencyCols = `, r.type_key, r.number, r.issuer, r.issued_on::text, r.expires_on::text`

func listCompetencies(ctx context.Context, db DBTX, orgID, where string, args ...any) ([]Competency, error) {
	rows, err := db.Query(ctx, `SELECT `+header(tableCompetencies)+competencyCols+`
		FROM worker_competencies r WHERE r.org_id = $1 AND `+where+` ORDER BY r.issued_on, r.recorded_at`, append([]any{orgID}, args...)...)
	if err != nil {
		return nil, err
	}
	out := []Competency{}
	for rows.Next() {
		var h rawHeader
		var c Competency
		if err := rows.Scan(append(h.dest(), &c.TypeKey, &c.Number, &c.Issuer, &c.IssuedOn, &c.ExpiresOn)...); err != nil {
			rows.Close()
			return nil, err
		}
		c.RecordHeader = h.finish()
		out = append(out, c)
	}
	return out, rows.Err()
}

const medicalCols = `, r.outcome, r.examined_on::text, r.expires_on::text, r.practice_number`

func listMedicals(ctx context.Context, db DBTX, orgID, where string, args ...any) ([]Medical, error) {
	rows, err := db.Query(ctx, `SELECT `+header(tableMedicals)+medicalCols+`
		FROM worker_medicals r WHERE r.org_id = $1 AND `+where+` ORDER BY r.examined_on, r.recorded_at`, append([]any{orgID}, args...)...)
	if err != nil {
		return nil, err
	}
	out := []Medical{}
	for rows.Next() {
		var h rawHeader
		var m Medical
		if err := rows.Scan(append(h.dest(), &m.Outcome, &m.ExaminedOn, &m.ExpiresOn, &m.PracticeNumber)...); err != nil {
			rows.Close()
			return nil, err
		}
		m.RecordHeader = h.finish()
		out = append(out, m)
	}
	return out, rows.Err()
}

func getCompetency(ctx context.Context, db DBTX, orgID, id string) (*Competency, error) {
	out, err := listCompetencies(ctx, db, orgID, `r.id = $2`, id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &out[0], nil
}

func getMedical(ctx context.Context, db DBTX, orgID, id string) (*Medical, error) {
	out, err := listMedicals(ctx, db, orgID, `r.id = $2`, id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &out[0], nil
}

func insertCompetency(ctx context.Context, db DBTX, orgID, workerID, id, by string, r CompetencyRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO worker_competencies (id, org_id, worker_id, regime_code, type_key, number, issuer, issued_on, expires_on, file_id,
		                                 supersedes_id, recorded_by, client_recorded_at, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		id, orgID, workerID, r.RegimeCode, r.TypeKey, r.Number, r.Issuer, r.IssuedOn, r.ExpiresOn, r.FileID, r.SupersedesID, by,
		r.ClientRecordedAt, r.DeviceID)
	return err
}

func insertMedical(ctx context.Context, db DBTX, orgID, workerID, id, by string, r MedicalRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO worker_medicals (id, org_id, worker_id, regime_code, outcome, examined_on, expires_on, practice_number, file_id,
		                             supersedes_id, recorded_by, client_recorded_at, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		id, orgID, workerID, r.RegimeCode, r.Outcome, r.ExaminedOn, r.ExpiresOn, r.PracticeNumber, r.FileID, r.SupersedesID, by,
		r.ClientRecordedAt, r.DeviceID)
	return err
}

func retractRecord(ctx context.Context, db DBTX, table, orgID, id, by, reason string) error {
	_, err := db.Exec(ctx, `UPDATE `+table+` SET retracted_by = $3, retract_reason = $4 WHERE org_id = $1 AND id = $2`, orgID, id, by, reason)
	return err
}

// recordAnywhere: the id is used in this organisation by either table.
func recordAnywhere(ctx context.Context, db DBTX, orgID, id string) (bool, error) {
	var found bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM worker_competencies WHERE org_id = $1 AND id = $2)
		    OR EXISTS (SELECT 1 FROM worker_medicals WHERE org_id = $1 AND id = $2)`, orgID, id).Scan(&found)
	return found, err
}

const linkCols = `id::text, attendance_id::text, worker_id::text, method, linked_by::text, linked_at, unlinked_by::text, unlinked_at, unlink_reason`

func liveLink(ctx context.Context, db DBTX, orgID, attendanceID string) (*Link, error) {
	var l Link
	err := db.QueryRow(ctx, `SELECT `+linkCols+` FROM toolbox_attendance_workers
		WHERE org_id = $1 AND attendance_id = $2 AND unlinked_at IS NULL`, orgID, attendanceID).
		Scan(&l.ID, &l.AttendanceID, &l.WorkerID, &l.Method, &l.LinkedBy, &l.LinkedAt, &l.UnlinkedBy, &l.UnlinkedAt, &l.UnlinkReason)
	return &l, err
}

func getLink(ctx context.Context, db DBTX, orgID, id string) (*Link, error) {
	var l Link
	err := db.QueryRow(ctx, `SELECT `+linkCols+` FROM toolbox_attendance_workers WHERE org_id = $1 AND id = $2`, orgID, id).
		Scan(&l.ID, &l.AttendanceID, &l.WorkerID, &l.Method, &l.LinkedBy, &l.LinkedAt, &l.UnlinkedBy, &l.UnlinkedAt, &l.UnlinkReason)
	return &l, err
}

func attendanceSite(ctx context.Context, db DBTX, orgID, attendanceID string) (string, error) {
	var site string
	err := db.QueryRow(ctx, `SELECT site_id::text FROM toolbox_attendance WHERE org_id = $1 AND id = $2`, orgID, attendanceID).Scan(&site)
	return site, err
}

func insertLink(ctx context.Context, db DBTX, orgID, siteID, attendanceID, workerID, method, by string) (string, error) {
	var id string
	err := db.QueryRow(ctx, `
		INSERT INTO toolbox_attendance_workers (org_id, site_id, attendance_id, worker_id, method, linked_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id::text`, orgID, siteID, attendanceID, workerID, method, by).Scan(&id)
	return id, err
}

func unlink(ctx context.Context, db DBTX, orgID, id, by, reason string) error {
	_, err := db.Exec(ctx, `UPDATE toolbox_attendance_workers SET unlinked_by = $3, unlink_reason = $4 WHERE org_id = $1 AND id = $2`,
		orgID, id, by, reason)
	return err
}
