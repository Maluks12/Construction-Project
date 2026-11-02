package evidence

import (
	"context"
	"encoding/hex"
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

// newEvidence is a declaration about to be stored as pending.
type newEvidence struct {
	ID, OrgID, SiteID, UploadedBy string
	ContentSHA256                 []byte
	SizeBytes                     int64
	ContentType                   string
	ClientCapturedAt              time.Time
	Lat, Lng                      *float64
	DeviceID                      *string
	ObjectKey                     string
}

const selectEvidence = `
	SELECT e.id, e.site_id, e.status, e.rejected_reason, e.content_sha256, e.size_bytes,
	       e.content_type, e.client_captured_at, e.lat, e.lng, e.device_id, e.uploaded_by,
	       e.created_at, e.object_key,
	       c.seq, c.server_received_at, c.prev_chain_hash, c.metadata_hash, c.chain_hash
	FROM evidence e
	LEFT JOIN evidence_chain c ON c.evidence_id = e.id
`

func scanEvidence(row pgx.Row) (*Evidence, error) {
	var (
		e                    Evidence
		sum                  []byte
		seq                  *int64
		received             *time.Time
		prev, meta, chainSum []byte
	)
	err := row.Scan(
		&e.ID, &e.SiteID, &e.Status, &e.RejectedReason, &sum, &e.SizeBytes,
		&e.ContentType, &e.ClientCapturedAt, &e.Lat, &e.Lng, &e.DeviceID, &e.UploadedBy,
		&e.CreatedAt, &e.ObjectKey,
		&seq, &received, &prev, &meta, &chainSum,
	)
	if err != nil {
		return nil, err
	}
	e.ContentSHA256 = hex.EncodeToString(sum)
	e.ClientCapturedAt = e.ClientCapturedAt.UTC()
	e.CreatedAt = e.CreatedAt.UTC()
	if seq != nil {
		e.Chain = &ChainEntry{
			Seq:              *seq,
			ServerReceivedAt: received.UTC(),
			PrevChainHash:    hex.EncodeToString(prev),
			MetadataHash:     hex.EncodeToString(meta),
			ChainHash:        hex.EncodeToString(chainSum),
		}
	}
	return &e, nil
}

func InsertEvidence(ctx context.Context, db DBTX, n newEvidence) error {
	query := `
		INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes,
		                      content_type, client_captured_at, lat, lng, device_id, object_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := db.Exec(ctx, query, n.ID, n.OrgID, n.SiteID, n.UploadedBy, n.ContentSHA256, n.SizeBytes,
		n.ContentType, n.ClientCapturedAt, n.Lat, n.Lng, n.DeviceID, n.ObjectKey)
	if err != nil {
		return fmt.Errorf("insert evidence: %w", err)
	}
	return nil
}

// GetEvidence looks across the whole organisation, so a declare can tell an
// id reused on another site from a new one.
func GetEvidence(ctx context.Context, db DBTX, orgID, id string) (*Evidence, error) {
	e, err := scanEvidence(db.QueryRow(ctx, selectEvidence+`WHERE e.org_id = $1 AND e.id = $2`, orgID, id))
	if err != nil {
		return nil, fmt.Errorf("get evidence: %w", err)
	}
	return e, nil
}

func GetSiteEvidence(ctx context.Context, db DBTX, orgID, siteID, id string) (*Evidence, error) {
	e, err := scanEvidence(db.QueryRow(ctx, selectEvidence+`WHERE e.org_id = $1 AND e.site_id = $2 AND e.id = $3`, orgID, siteID, id))
	if err != nil {
		return nil, fmt.Errorf("get site evidence: %w", err)
	}
	return e, nil
}

// ListEvidence returns a site's evidence, newest first. status "" means any.
func ListEvidence(ctx context.Context, db DBTX, orgID, siteID, status string, limit int) ([]Evidence, error) {
	query := selectEvidence + `
		WHERE e.org_id = $1 AND e.site_id = $2 AND ($3 = '' OR e.status = $3)
		ORDER BY e.created_at DESC, e.id
		LIMIT $4
	`
	rows, err := db.Query(ctx, query, orgID, siteID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	defer rows.Close()

	items := []Evidence{}
	for rows.Next() {
		e, err := scanEvidence(rows)
		if err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		items = append(items, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	return items, nil
}
