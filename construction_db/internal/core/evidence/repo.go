package evidence

import (
	"context"
	"encoding/hex"
	"errors"
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
	       e.created_at, e.object_key, e.object_version_id,
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
		&e.CreatedAt, &e.ObjectKey, &e.ObjectVersionID,
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

// GetEvidenceForUpdate locks the evidence row. OF e because Postgres can't
// lock the nullable side of the LEFT JOIN.
func GetEvidenceForUpdate(ctx context.Context, db DBTX, orgID, id string) (*Evidence, error) {
	e, err := scanEvidence(db.QueryRow(ctx, selectEvidence+`WHERE e.org_id = $1 AND e.id = $2 FOR UPDATE OF e`, orgID, id))
	if err != nil {
		return nil, fmt.Errorf("get evidence for update: %w", err)
	}
	return e, nil
}

// EnsureHead creates a site's chain head (seq 0, genesis hash) if it has none.
// ON CONFLICT makes two first uploads to a site safe.
func EnsureHead(ctx context.Context, db DBTX, orgID, siteID string) error {
	_, err := db.Exec(ctx, `INSERT INTO site_chain_heads (site_id, org_id) VALUES ($1, $2) ON CONFLICT (site_id) DO NOTHING`, siteID, orgID)
	if err != nil {
		return fmt.Errorf("ensure chain head: %w", err)
	}
	return nil
}

// LockHead locks a site's chain head until the transaction ends; this is what
// serialises appends to one site's chain.
func LockHead(ctx context.Context, db DBTX, orgID, siteID string) (seq int64, headHash []byte, err error) {
	err = db.QueryRow(ctx, `SELECT seq, head_hash FROM site_chain_heads WHERE org_id = $1 AND site_id = $2 FOR UPDATE`, orgID, siteID).Scan(&seq, &headHash)
	if err != nil {
		return 0, nil, fmt.Errorf("lock chain head: %w", err)
	}
	return seq, headHash, nil
}

// Now is the database clock at this moment, not at the transaction's start.
func Now(ctx context.Context, db DBTX) (time.Time, error) {
	var t time.Time
	if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t); err != nil {
		return time.Time{}, fmt.Errorf("clock: %w", err)
	}
	return t.UTC(), nil
}

type chainEntry struct {
	EvidenceID, OrgID, SiteID              string
	Seq                                    int64
	ContentSHA256                          []byte
	ServerReceivedAt                       time.Time
	PrevChainHash, MetadataHash, ChainHash []byte
}

func InsertChainEntry(ctx context.Context, db DBTX, c chainEntry) error {
	query := `
		INSERT INTO evidence_chain (evidence_id, org_id, site_id, seq, content_sha256, server_received_at,
		                            prev_chain_hash, metadata_hash, chain_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := db.Exec(ctx, query, c.EvidenceID, c.OrgID, c.SiteID, c.Seq, c.ContentSHA256, c.ServerReceivedAt,
		c.PrevChainHash, c.MetadataHash, c.ChainHash)
	if err != nil {
		return fmt.Errorf("insert chain entry: %w", err)
	}
	return nil
}

func AdvanceHead(ctx context.Context, db DBTX, orgID, siteID string, seq int64, headHash []byte) (bool, error) {
	tag, err := db.Exec(ctx, `UPDATE site_chain_heads SET seq = $3, head_hash = $4, updated_at = now() WHERE org_id = $1 AND site_id = $2`,
		orgID, siteID, seq, headHash)
	if err != nil {
		return false, fmt.Errorf("advance chain head: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func MarkVerified(ctx context.Context, db DBTX, orgID, id, versionID string, retainUntil time.Time) (bool, error) {
	tag, err := db.Exec(ctx, `
		UPDATE evidence SET status = 'verified', object_version_id = $3, retain_until = $4, updated_at = now()
		WHERE org_id = $1 AND id = $2 AND status = 'pending'
	`, orgID, id, versionID, retainUntil)
	if err != nil {
		return false, fmt.Errorf("mark verified: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func MarkRejected(ctx context.Context, db DBTX, orgID, id, reason string) (bool, error) {
	tag, err := db.Exec(ctx, `
		UPDATE evidence SET status = 'rejected', rejected_reason = $3, updated_at = now()
		WHERE org_id = $1 AND id = $2 AND status = 'pending'
	`, orgID, id, reason)
	if err != nil {
		return false, fmt.Errorf("mark rejected: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// GetHead returns a site's chain head; a site with no evidence yet has none
// (ok false).
func GetHead(ctx context.Context, db DBTX, orgID, siteID string) (seq int64, headHash []byte, ok bool, err error) {
	err = db.QueryRow(ctx, `SELECT seq, head_hash FROM site_chain_heads WHERE org_id = $1 AND site_id = $2`, orgID, siteID).Scan(&seq, &headHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, fmt.Errorf("get chain head: %w", err)
	}
	return seq, headHash, true, nil
}

// storedEntry is one chain entry with the evidence fields it seals.
type storedEntry struct {
	SiteID                                 string
	Seq                                    int64
	ContentSHA256                          []byte
	ServerReceivedAt                       time.Time
	PrevChainHash, MetadataHash, ChainHash []byte
	Metadata                               Metadata
	EvidenceStatus                         string // must be verified for a chained entry
}

// StreamChain calls fn for each of a site's entries in seq order, one row at
// a time, so a long chain never sits in memory.
func StreamChain(ctx context.Context, db DBTX, orgID, siteID string, fn func(storedEntry) error) error {
	rows, err := db.Query(ctx, `
		SELECT c.site_id, c.seq, c.content_sha256, c.server_received_at, c.prev_chain_hash, c.metadata_hash, c.chain_hash,
		       e.id, e.client_captured_at, e.lat, e.lng, e.uploaded_by, e.device_id, e.status
		FROM evidence_chain c
		JOIN evidence e ON e.id = c.evidence_id
		WHERE c.org_id = $1 AND c.site_id = $2
		ORDER BY c.seq
	`, orgID, siteID)
	if err != nil {
		return fmt.Errorf("stream chain: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s storedEntry
		m := &s.Metadata
		if err := rows.Scan(&s.SiteID, &s.Seq, &s.ContentSHA256, &s.ServerReceivedAt, &s.PrevChainHash, &s.MetadataHash, &s.ChainHash,
			&m.EvidenceID, &m.ClientCapturedAt, &m.Lat, &m.Lng, &m.UploadedBy, &m.DeviceID, &s.EvidenceStatus); err != nil {
			return fmt.Errorf("scan chain entry: %w", err)
		}
		if err := fn(s); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("stream chain: %w", err)
	}
	return nil
}

// VerifiedWithoutEntry returns one verified evidence id on the site that has
// no chain entry, or "" if there is none.
func VerifiedWithoutEntry(ctx context.Context, db DBTX, orgID, siteID string) (string, error) {
	var id string
	err := db.QueryRow(ctx, `
		SELECT e.id FROM evidence e
		WHERE e.org_id = $1 AND e.site_id = $2 AND e.status = 'verified'
		  AND NOT EXISTS (SELECT 1 FROM evidence_chain c WHERE c.evidence_id = e.id)
		ORDER BY e.created_at, e.id
		LIMIT 1
	`, orgID, siteID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("verified without entry: %w", err)
	}
	return id, nil
}

// accessEntry is one evidence_access_log row. EvidenceID is nil only for a
// list; the filter and count fields are set only for a list.
type accessEntry struct {
	OrgID, SiteID, UserID, Action string
	EvidenceID                    *string
	ObjectVersionID               *string // view_bytes only
	FilterStatus                  *string
	FilterLimit, ResultCount      *int
	Client                        Client
}

func InsertAccess(ctx context.Context, db DBTX, a accessEntry) error {
	_, err := db.Exec(ctx, `
		INSERT INTO evidence_access_log (org_id, site_id, evidence_id, user_id, action, object_version_id,
		                                 filter_status, filter_limit, result_count, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::inet, $11)
	`, a.OrgID, a.SiteID, a.EvidenceID, a.UserID, a.Action, a.ObjectVersionID,
		a.FilterStatus, a.FilterLimit, a.ResultCount, a.Client.logIP(), a.Client.logUserAgent())
	if err != nil {
		return fmt.Errorf("insert access log: %w", err)
	}
	return nil
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
