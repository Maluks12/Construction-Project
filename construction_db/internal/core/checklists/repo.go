package checklists

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

// newInspection is an inspection about to be stored as in progress.
type newInspection struct {
	ID, OrgID, SiteID, RegimeID, StartedBy string
	TemplateKey                            string
	TemplateVersion                        int
	TemplateSHA256                         []byte
	TemplateTitle, TemplateSourceRef       string
	ItemCount                              int
	ClientStartedAt                        time.Time
	DeviceID, CorrectsID                   *string
}

// newResponse is one template item, copied so the inspection keeps its wording.
type newResponse struct {
	Key, Prompt, ResponseType string
	SourceRef                 *string
}

const selectInspection = `
	SELECT id, site_id, status, template_key, template_version, template_sha256, template_title,
	       template_source_ref, item_count, started_by, client_started_at, started_at, device_id,
	       corrects_id, client_completed_at, completed_at
	FROM inspections
`

func scanInspection(row pgx.Row) (*Inspection, error) {
	var (
		in  Inspection
		sum []byte
	)
	err := row.Scan(
		&in.ID, &in.SiteID, &in.Status, &in.TemplateKey, &in.TemplateVersion, &sum, &in.TemplateTitle,
		&in.TemplateSourceRef, &in.ItemCount, &in.StartedBy, &in.ClientStartedAt, &in.StartedAt, &in.DeviceID,
		&in.CorrectsID, &in.ClientCompletedAt, &in.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	in.TemplateSHA256 = hex.EncodeToString(sum)
	in.ClientStartedAt = in.ClientStartedAt.UTC()
	in.StartedAt = in.StartedAt.UTC()
	in.ClientCompletedAt = utc(in.ClientCompletedAt)
	in.CompletedAt = utc(in.CompletedAt)
	in.Items = []InspectionItem{}
	return &in, nil
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// GetInspection looks across the whole organisation, so a start can tell an
// id reused on another site from a new one. Items are not loaded.
func GetInspection(ctx context.Context, db DBTX, orgID, id string) (*Inspection, error) {
	in, err := scanInspection(db.QueryRow(ctx, selectInspection+`WHERE org_id = $1 AND id = $2`, orgID, id))
	if err != nil {
		return nil, fmt.Errorf("get inspection: %w", err)
	}
	return in, nil
}

// GetSiteInspection is one inspection on one site. Items are not loaded.
func GetSiteInspection(ctx context.Context, db DBTX, orgID, siteID, id string) (*Inspection, error) {
	in, err := scanInspection(db.QueryRow(ctx, selectInspection+`WHERE org_id = $1 AND site_id = $2 AND id = $3`, orgID, siteID, id))
	if err != nil {
		return nil, fmt.Errorf("get site inspection: %w", err)
	}
	return in, nil
}

// ListItems returns an inspection's items in template order.
func ListItems(ctx context.Context, db DBTX, orgID, inspectionID string) ([]InspectionItem, error) {
	rows, err := db.Query(ctx, `
		SELECT item_key, position, prompt, item_source_ref, response_type, result, severity, comment,
		       client_answered_at, answered_at
		FROM inspection_responses
		WHERE org_id = $1 AND inspection_id = $2
		ORDER BY position
	`, orgID, inspectionID)
	if err != nil {
		return nil, fmt.Errorf("list inspection items: %w", err)
	}
	defer rows.Close()

	items := []InspectionItem{}
	for rows.Next() {
		var it InspectionItem
		if err := rows.Scan(&it.Key, &it.Position, &it.Prompt, &it.SourceRef, &it.ResponseType, &it.Result, &it.Severity, &it.Comment,
			&it.ClientAnsweredAt, &it.AnsweredAt); err != nil {
			return nil, fmt.Errorf("scan inspection item: %w", err)
		}
		it.ClientAnsweredAt = utc(it.ClientAnsweredAt)
		it.AnsweredAt = utc(it.AnsweredAt)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list inspection items: %w", err)
	}
	return items, nil
}

func InsertInspection(ctx context.Context, db DBTX, n newInspection) error {
	_, err := db.Exec(ctx, `
		INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
		                         template_title, template_source_ref, item_count, started_by, client_started_at,
		                         device_id, corrects_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, n.ID, n.OrgID, n.SiteID, n.RegimeID, n.TemplateKey, n.TemplateVersion, n.TemplateSHA256,
		n.TemplateTitle, n.TemplateSourceRef, n.ItemCount, n.StartedBy, n.ClientStartedAt,
		n.DeviceID, n.CorrectsID)
	if err != nil {
		return fmt.Errorf("insert inspection: %w", err)
	}
	return nil
}

// InsertResponses stores one unanswered response per item in one statement.
// WITH ORDINALITY numbers the rows 1..n in array order, which is the
// template's item order.
func InsertResponses(ctx context.Context, db DBTX, orgID, siteID, inspectionID string, items []newResponse) error {
	keys := make([]string, len(items))
	prompts := make([]string, len(items))
	refs := make([]*string, len(items))
	types := make([]string, len(items))
	for i, it := range items {
		keys[i], prompts[i], refs[i], types[i] = it.Key, it.Prompt, it.SourceRef, it.ResponseType
	}
	tag, err := db.Exec(ctx, `
		INSERT INTO inspection_responses (org_id, site_id, inspection_id, item_key, position, prompt, item_source_ref, response_type)
		SELECT $1, $2, $3, t.key, t.position, t.prompt, t.source_ref, t.response_type
		FROM unnest($4::text[], $5::text[], $6::text[], $7::text[])
		     WITH ORDINALITY AS t(key, prompt, source_ref, response_type, position)
	`, orgID, siteID, inspectionID, keys, prompts, refs, types)
	if err != nil {
		return fmt.Errorf("insert inspection responses: %w", err)
	}
	if int(tag.RowsAffected()) != len(items) {
		return fmt.Errorf("insert inspection responses: stored %d of %d", tag.RowsAffected(), len(items))
	}
	return nil
}
