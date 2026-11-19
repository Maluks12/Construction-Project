package files

import (
	"context"
	"encoding/hex"
	"errors"
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

const fileSQL = `
	SELECT id::text, purpose, site_id::text, uploaded_by::text, content_sha256, size_bytes, content_type, file_name,
	       device_id, status, rejected_reason, retain_until, created_at, object_key, object_version_id
	FROM files`

func scanFile(row pgx.Row) (*File, error) {
	var f File
	var sum []byte
	err := row.Scan(&f.ID, &f.Purpose, &f.SiteID, &f.UploadedBy, &sum, &f.SizeBytes, &f.ContentType, &f.FileName,
		&f.DeviceID, &f.Status, &f.RejectedReason, &f.RetainUntil, &f.CreatedAt, &f.objectKey, &f.objectVersion)
	if err != nil {
		return nil, err
	}
	f.ContentSHA256 = hex.EncodeToString(sum)
	return &f, nil
}

// GetFile reads a file of the tenant's organisation.
func GetFile(ctx context.Context, db DBTX, orgID, id string) (*File, error) {
	return scanFile(db.QueryRow(ctx, fileSQL+` WHERE org_id = $1 AND id = $2`, orgID, id))
}

type newFile struct {
	ID, OrgID, UploadedBy, Purpose, ContentType, ObjectKey string
	SiteID, FileName, DeviceID                             *string
	Sum                                                    []byte
	Size                                                   int64
}

func insertFile(ctx context.Context, db DBTX, f newFile) error {
	_, err := db.Exec(ctx, `
		INSERT INTO files (id, org_id, site_id, purpose, uploaded_by, content_sha256, size_bytes, content_type, file_name, device_id, object_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		f.ID, f.OrgID, f.SiteID, f.Purpose, f.UploadedBy, f.Sum, f.Size, f.ContentType, f.FileName, f.DeviceID, f.ObjectKey)
	return err
}

// Both outcomes change only a pending row, so two completes can't both win.
func markVerified(ctx context.Context, db DBTX, orgID, id, version string, retain time.Time) (bool, error) {
	tag, err := db.Exec(ctx, `
		UPDATE files SET status = 'verified', object_version_id = $3, retain_until = $4, updated_at = now()
		WHERE org_id = $1 AND id = $2 AND status = 'pending'`, orgID, id, version, retain)
	return tag.RowsAffected() == 1, err
}

func markRejected(ctx context.Context, db DBTX, orgID, id, reason string) (bool, error) {
	tag, err := db.Exec(ctx, `
		UPDATE files SET status = 'rejected', rejected_reason = $3, updated_at = now()
		WHERE org_id = $1 AND id = $2 AND status = 'pending'`, orgID, id, reason)
	return tag.RowsAffected() == 1, err
}

func insertAccess(ctx context.Context, db DBTX, orgID, fileID, userID, version string, ip, ua *string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO file_access_log (org_id, file_id, user_id, action, object_version_id, ip, user_agent)
		VALUES ($1, $2, $3, 'view_bytes', $4, $5::inet, $6)`, orgID, fileID, userID, version, ip, ua)
	return err
}
