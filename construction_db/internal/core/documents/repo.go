package documents

import (
	"context"
	"errors"
	"strings"

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

const docCols = `d.id::text, d.scope, d.site_id::text, d.regime_code, d.type_key, d.title, d.created_by::text, d.created_at, d.latest_version,
	d.retract_reason, d.retracted_by::text, d.retracted_at`

const versionCols = `v.version, v.file_id::text, v.issuer, v.reference, v.issued_on::text, v.expires_on::text, v.uploaded_by::text,
	v.client_uploaded_at, v.received_at, v.device_id, v.retract_reason, v.retracted_by::text, v.retracted_at`

func versionDest(v *Version) []any {
	return []any{&v.Version, &v.FileID, &v.Issuer, &v.Reference, &v.IssuedOn, &v.ExpiresOn, &v.UploadedBy, &v.ClientUploadedAt, &v.ReceivedAt, &v.DeviceID,
		&v.ret.Reason, &v.ret.RetractedBy, &v.ret.RetractedAt}
}

// listDocs: the documents of a scope (and site), each with its current version.
func listDocs(ctx context.Context, db DBTX, orgID, scope string, siteID *string, where string, args ...any) ([]DocumentSummary, error) {
	q := `SELECT ` + docCols + `, ` + versionCols + `
		FROM documents d JOIN document_versions v ON v.document_id = d.id AND v.version = document_current_version(d.id)
		WHERE d.org_id = $1 AND d.scope = $2 AND d.site_id IS NOT DISTINCT FROM $3::uuid`
	if where != "" {
		q += ` AND ` + where
	}
	rows, err := db.Query(ctx, q+` ORDER BY d.type_key, lower(d.title), d.id`, append([]any{orgID, scope, siteID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentSummary{}
	for rows.Next() {
		var d DocumentSummary
		dest := append([]any{&d.ID, &d.Scope, &d.SiteID, &d.RegimeCode, &d.TypeKey, &d.Title, &d.CreatedBy, &d.CreatedAt, &d.LatestVersion,
			&d.ret.Reason, &d.ret.RetractedBy, &d.ret.RetractedAt},
			versionDest(&d.Current)...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		d.Retraction = d.ret.finish()
		d.Current.Retraction = d.Current.ret.finish()
		out = append(out, d)
	}
	return out, rows.Err()
}

func getDoc(ctx context.Context, db DBTX, orgID, scope string, siteID *string, id string) (*DocumentSummary, error) {
	out, err := listDocs(ctx, db, orgID, scope, siteID, `d.id = $4`, id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &out[0], nil
}

func listVersions(ctx context.Context, db DBTX, orgID, docID string) ([]Version, error) {
	rows, err := db.Query(ctx, `SELECT `+versionCols+` FROM document_versions v WHERE v.org_id = $1 AND v.document_id = $2 ORDER BY v.version`,
		orgID, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(versionDest(&v)...); err != nil {
			return nil, err
		}
		v.Retraction = v.ret.finish()
		out = append(out, v)
	}
	return out, rows.Err()
}

func docAnywhere(ctx context.Context, db DBTX, orgID, id string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM documents WHERE org_id = $1 AND id = $2)`, orgID, id).Scan(&ok)
	return ok, err
}

func insertDoc(ctx context.Context, db DBTX, orgID, id, scope string, siteID *string, regime, typeKey, title, by string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO documents (id, org_id, site_id, scope, regime_code, type_key, title, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, orgID, siteID, scope, regime, typeKey, title, by)
	return err
}

func insertVersion(ctx context.Context, db DBTX, orgID, docID, scope string, siteID *string, n int, by string, r VersionRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO document_versions (org_id, site_id, document_id, version, scope, file_id, file_purpose, issuer, reference,
		                               issued_on, expires_on, uploaded_by, client_uploaded_at, device_id)
		VALUES ($1, $2, $3, $4, $5, $6, $5 || '_document', $7, $8, $9, $10, $11, $12, $13)`,
		orgID, siteID, docID, n, scope, r.FileID, r.Issuer, r.Reference, r.IssuedOn, r.ExpiresOn, by, r.ClientUploadedAt, r.DeviceID)
	return err
}
