package attendance

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

const signInSQL = `
	SELECT id::text, site_id::text, worker_id::text, name, firm, clock_number, method, matched_existing, recorded_by::text,
	       client_signed_in_at, received_at, sign_in_date::text, time_zone, received_after_close, device_id,
	       sign_out_method, signed_out_by::text, client_signed_out_at, sign_out_received_at,
	       retract_reason, retracted_by::text, retracted_at, site_sign_in_ends_at(site_sign_ins.*),
	       client_signed_out_at IS NULL AND retracted_at IS NULL AND site_sign_in_ends_at(site_sign_ins.*) <= now()
	FROM site_sign_ins`

func scanSignIn(row pgx.Row) (*SignIn, error) {
	var (
		s                SignIn
		outMethod, outBy *string
		outAt, outRecv   *time.Time
		reason, retBy    *string
		retAt            *time.Time
	)
	err := row.Scan(&s.ID, &s.SiteID, &s.WorkerID, &s.Name, &s.Firm, &s.ClockNumber, &s.Method, &s.MatchedExisting, &s.RecordedBy,
		&s.ClientSignedInAt, &s.ReceivedAt, &s.SignInDate, &s.TimeZone, &s.ReceivedAfterClose, &s.DeviceID,
		&outMethod, &outBy, &outAt, &outRecv, &reason, &retBy, &retAt, &s.EndsAt, &s.Lapsed)
	if err != nil {
		return nil, err
	}
	if outMethod != nil {
		s.SignOut = &SignOut{Method: *outMethod, SignedOutBy: *outBy, ClientSignedOutAt: *outAt, ReceivedAt: *outRecv}
	}
	if reason != nil {
		s.Retraction = &Retraction{Reason: *reason, RetractedBy: *retBy, RetractedAt: *retAt}
	}
	s.Warnings = []string{}
	return &s, nil
}

func getSignIn(ctx context.Context, db DBTX, orgID, id string) (*SignIn, error) {
	return scanSignIn(db.QueryRow(ctx, signInSQL+` WHERE org_id = $1 AND id = $2`, orgID, id))
}

func listSignIns(ctx context.Context, db DBTX, orgID, siteID, date string) ([]SignIn, error) {
	rows, err := db.Query(ctx, signInSQL+` WHERE org_id = $1 AND site_id = $2 AND sign_in_date = $3
		ORDER BY client_signed_in_at, id`, orgID, siteID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SignIn{}
	for rows.Next() {
		s, err := scanSignIn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func insertSignIn(ctx context.Context, db DBTX, orgID, siteID, id, workerID, by, zone string, matched bool, req SignInRequest) error {
	// name is a placeholder: the trigger copies the register's.
	_, err := db.Exec(ctx, `
		INSERT INTO site_sign_ins (id, org_id, site_id, worker_id, name, method, matched_existing, recorded_by, client_signed_in_at,
		                           time_zone, device_id)
		VALUES ($1, $2, $3, $4, '-', $5, $6, $7, $8, $9, $10)`,
		id, orgID, siteID, workerID, req.Method, matched, by, req.ClientSignedInAt, zone, req.DeviceID)
	return err
}

func signOut(ctx context.Context, db DBTX, orgID, id, by string, req SignOutRequest) error {
	_, err := db.Exec(ctx, `
		UPDATE site_sign_ins SET client_signed_out_at = $3, sign_out_method = $4, signed_out_by = $5
		WHERE org_id = $1 AND id = $2`, orgID, id, req.ClientSignedOutAt, req.Method, by)
	return err
}

func retract(ctx context.Context, db DBTX, orgID, id, by, reason string) error {
	_, err := db.Exec(ctx, `UPDATE site_sign_ins SET retracted_by = $3, retract_reason = $4 WHERE org_id = $1 AND id = $2`, orgID, id, by, reason)
	return err
}

// covering is the worker's standing sign-in on the site at at, if any.
func covering(ctx context.Context, db DBTX, siteID, workerID string, at time.Time) (string, error) {
	var id *string
	err := db.QueryRow(ctx, `SELECT site_sign_in_covering($1, $2, $3, '00000000-0000-0000-0000-000000000000')::text`,
		siteID, workerID, at).Scan(&id)
	if err != nil || id == nil {
		return "", err
	}
	return *id, nil
}

func signInAnywhere(ctx context.Context, db DBTX, orgID, id string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM site_sign_ins WHERE org_id = $1 AND id = $2)`, orgID, id).Scan(&ok)
	return ok, err
}

func hasSheet(ctx context.Context, db DBTX, siteID string, at time.Time, zone string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM site_sign_in_sheets WHERE site_id = $1 AND sheet_date = action_local_date($2, $3))`,
		siteID, at, zone).Scan(&ok)
	return ok, err
}

const sheetSQL = `SELECT site_id::text, sheet_date::text, evidence_id::text, added_by::text, added_at FROM site_sign_in_sheets`

func listSheets(ctx context.Context, db DBTX, orgID, siteID, date string) ([]Sheet, error) {
	rows, err := db.Query(ctx, sheetSQL+` WHERE org_id = $1 AND site_id = $2 AND sheet_date = $3 ORDER BY added_at`, orgID, siteID, date)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Sheet, error) {
		var s Sheet
		err := r.Scan(&s.SiteID, &s.SheetDate, &s.EvidenceID, &s.AddedBy, &s.AddedAt)
		return s, err
	})
}

func getSheet(ctx context.Context, db DBTX, orgID, siteID, date, evidenceID string) (*Sheet, error) {
	var s Sheet
	err := db.QueryRow(ctx, sheetSQL+` WHERE org_id = $1 AND site_id = $2 AND sheet_date = $3 AND evidence_id = $4`, orgID, siteID, date, evidenceID).
		Scan(&s.SiteID, &s.SheetDate, &s.EvidenceID, &s.AddedBy, &s.AddedAt)
	return &s, err
}

func insertSheet(ctx context.Context, db DBTX, orgID, siteID, date, evidenceID, by string) error {
	_, err := db.Exec(ctx, `INSERT INTO site_sign_in_sheets (org_id, site_id, sheet_date, evidence_id, added_by) VALUES ($1, $2, $3, $4, $5)`,
		orgID, siteID, date, evidenceID, by)
	return err
}

func evidenceStatus(ctx context.Context, db DBTX, orgID, siteID, id string) (string, error) {
	var st string
	err := db.QueryRow(ctx, `SELECT status FROM evidence WHERE org_id = $1 AND site_id = $2 AND id = $3`, orgID, siteID, id).Scan(&st)
	return st, err
}

const rosterSQL = `
	SELECT r.id::text, r.worker_id::text, w.name, w.firm, w.clock_number, w.active, r.added_via, r.added_by::text, r.added_at,
	       r.removed_by::text, r.removed_at, r.remove_reason,
	       (SELECT max(x.sign_in_date)::text FROM site_sign_ins x
	        WHERE x.site_id = r.site_id AND x.worker_id = r.worker_id AND x.retracted_at IS NULL)
	FROM site_roster r JOIN workers w ON w.id = r.worker_id`

func scanRoster(r pgx.CollectableRow) (RosterEntry, error) {
	var e RosterEntry
	err := r.Scan(&e.ID, &e.WorkerID, &e.Name, &e.Firm, &e.ClockNumber, &e.Active, &e.AddedVia, &e.AddedBy, &e.AddedAt,
		&e.RemovedBy, &e.RemovedAt, &e.RemoveReason, &e.LastSignInOn)
	return e, err
}

func listRoster(ctx context.Context, db DBTX, orgID, siteID string, liveOnly bool) ([]RosterEntry, error) {
	rows, err := db.Query(ctx, rosterSQL+` WHERE r.org_id = $1 AND r.site_id = $2 AND ($3 = false OR r.removed_at IS NULL)
		ORDER BY lower(w.name), r.added_at`, orgID, siteID, liveOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRoster)
}

func liveRosterEntry(ctx context.Context, db DBTX, orgID, siteID, workerID string) (*RosterEntry, error) {
	rows, err := db.Query(ctx, rosterSQL+` WHERE r.org_id = $1 AND r.site_id = $2 AND r.worker_id = $3 AND r.removed_at IS NULL`,
		orgID, siteID, workerID)
	if err != nil {
		return nil, err
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanRoster)
	return &e, err
}

// lastRemoval is the worker's most recent removal from the site's roster.
func lastRemoval(ctx context.Context, db DBTX, orgID, siteID, workerID string) (*RosterEntry, error) {
	rows, err := db.Query(ctx, rosterSQL+` WHERE r.org_id = $1 AND r.site_id = $2 AND r.worker_id = $3 AND r.removed_at IS NOT NULL
		ORDER BY r.removed_at DESC, r.id LIMIT 1`, orgID, siteID, workerID)
	if err != nil {
		return nil, err
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanRoster)
	return &e, err
}

func addToRoster(ctx context.Context, db DBTX, orgID, siteID, workerID, by string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO site_roster (org_id, site_id, worker_id, added_via, added_by) VALUES ($1, $2, $3, 'manual', $4)
		ON CONFLICT (site_id, worker_id) WHERE removed_at IS NULL DO NOTHING`, orgID, siteID, workerID, by)
	return err
}

func removeFromRoster(ctx context.Context, db DBTX, orgID, id, by, reason string) error {
	_, err := db.Exec(ctx, `UPDATE site_roster SET removed_by = $3, remove_reason = $4 WHERE org_id = $1 AND id = $2`, orgID, id, by, reason)
	return err
}

// absent is site_absent (000032): who was expected on that day and didn't
// sign in, as the roster and the register stood then.
func absent(ctx context.Context, db DBTX, siteID, date, zone string, lapseDays int) ([]RosterEntry, error) {
	rows, err := db.Query(ctx, `
		SELECT roster_id::text, worker_id::text, name, firm, clock_number, true, added_via, added_by::text, added_at,
		       removed_by::text, removed_at, remove_reason, NULL::text
		FROM site_absent($1, $2::date, $3, $4)`, siteID, date, zone, lapseDays)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, scanRoster)
	for i := range out {
		out[i].Expected = true
	}
	return out, err
}

// overlaps: the day's standing sign-ins here whose time overlaps a standing
// sign-in of the same worker on another site (D14).
func overlaps(ctx context.Context, db DBTX, orgID, siteID, date string) ([]Overlap, error) {
	rows, err := db.Query(ctx, `
		SELECT s.id::text, o.site_id::text, o.id::text
		FROM site_sign_ins s JOIN site_sign_ins o
		  ON o.worker_id = s.worker_id AND o.site_id <> s.site_id AND o.retracted_at IS NULL
		 AND o.client_signed_in_at < site_sign_in_ends_at(s) AND s.client_signed_in_at < site_sign_in_ends_at(o)
		WHERE s.org_id = $1 AND s.site_id = $2 AND s.sign_in_date = $3 AND s.retracted_at IS NULL
		ORDER BY s.client_signed_in_at, o.client_signed_in_at`, orgID, siteID, date)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Overlap, error) {
		var o Overlap
		err := r.Scan(&o.SignInID, &o.OtherSiteID, &o.OtherSignInID)
		return o, err
	})
}

func headcount(ctx context.Context, db DBTX, siteID, date string) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT site_headcount($1, $2)`, siteID, date).Scan(&n)
	return n, err
}
