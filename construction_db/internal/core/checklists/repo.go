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

// headerColumns are InspectionHeader's columns, in scanHeader's order. The
// "i." prefix lets a query join inspections to its responses.
const headerColumns = `
	i.id, i.site_id, i.status, i.template_key, i.template_version, i.template_sha256, i.template_title,
	i.template_source_ref, i.item_count, i.started_by, i.client_started_at, i.started_at, i.device_id,
	i.corrects_id, i.client_completed_at, i.completed_at, i.abandoned_at, i.abandoned_by, i.abandon_reason`

const selectInspection = `SELECT` + headerColumns + `
	FROM inspections i
`

// scanHeader scans headerColumns into h, then any extra columns the query
// selects after them.
func scanHeader(row pgx.Row, h *InspectionHeader, extra ...any) error {
	var sum []byte
	dest := append([]any{
		&h.ID, &h.SiteID, &h.Status, &h.TemplateKey, &h.TemplateVersion, &sum, &h.TemplateTitle,
		&h.TemplateSourceRef, &h.ItemCount, &h.StartedBy, &h.ClientStartedAt, &h.StartedAt, &h.DeviceID,
		&h.CorrectsID, &h.ClientCompletedAt, &h.CompletedAt, &h.AbandonedAt, &h.AbandonedBy, &h.AbandonReason,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return err
	}
	h.TemplateSHA256 = hex.EncodeToString(sum)
	h.ClientStartedAt = h.ClientStartedAt.UTC()
	h.StartedAt = h.StartedAt.UTC()
	h.ClientCompletedAt = utc(h.ClientCompletedAt)
	h.CompletedAt = utc(h.CompletedAt)
	h.AbandonedAt = utc(h.AbandonedAt)
	return nil
}

func scanInspection(row pgx.Row) (*Inspection, error) {
	in := Inspection{Items: []InspectionItem{}, Findings: []InspectionFinding{}}
	if err := scanHeader(row, &in.InspectionHeader); err != nil {
		return nil, err
	}
	return &in, nil
}

// ListInspections returns a site's inspections, newest first, with how many
// items each has answered and failed. status "" means any.
//
// LEFT JOIN and GROUP BY i.id: one row per inspection, counting its
// responses. count(r.result) skips NULLs, so it counts answered items only.
// Grouping by the primary key lets the other inspection columns be selected.
func ListInspections(ctx context.Context, db DBTX, orgID, siteID, status string, limit int) ([]InspectionSummary, error) {
	rows, err := db.Query(ctx, `SELECT`+headerColumns+`,
		       count(r.result), count(*) FILTER (WHERE r.result = 'fail')
		FROM inspections i
		LEFT JOIN inspection_responses r ON r.inspection_id = i.id
		WHERE i.org_id = $1 AND i.site_id = $2 AND ($3 = '' OR i.status = $3)
		GROUP BY i.id
		ORDER BY i.started_at DESC, i.id
		LIMIT $4
	`, orgID, siteID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list inspections: %w", err)
	}
	defer rows.Close()

	items := []InspectionSummary{}
	for rows.Next() {
		var s InspectionSummary
		if err := scanHeader(rows, &s.InspectionHeader, &s.AnsweredCount, &s.FailedCount); err != nil {
			return nil, fmt.Errorf("scan inspection summary: %w", err)
		}
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list inspections: %w", err)
	}
	return items, nil
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
	in, err := scanInspection(db.QueryRow(ctx, selectInspection+`WHERE i.org_id = $1 AND i.id = $2`, orgID, id))
	if err != nil {
		return nil, fmt.Errorf("get inspection: %w", err)
	}
	return in, nil
}

// GetSiteInspection is one inspection on one site. Items are not loaded.
func GetSiteInspection(ctx context.Context, db DBTX, orgID, siteID, id string) (*Inspection, error) {
	in, err := scanInspection(db.QueryRow(ctx, selectInspection+`WHERE i.org_id = $1 AND i.site_id = $2 AND i.id = $3`, orgID, siteID, id))
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
		it.Evidence = []AttachedEvidence{}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list inspection items: %w", err)
	}

	attached, err := ListAttached(ctx, db, orgID, inspectionID, "")
	if err != nil {
		return nil, err
	}
	for i := range items {
		if a, ok := attached[items[i].Key]; ok {
			items[i].Evidence = a
		}
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

// GetCorrection returns the id of the inspection that corrects correctedID,
// or pgx.ErrNoRows if there is none. An abandoned correction doesn't count:
// abandoning frees the slot (000014).
func GetCorrection(ctx context.Context, db DBTX, orgID, correctedID string) (string, error) {
	var id string
	if err := db.QueryRow(ctx, `SELECT id FROM inspections WHERE org_id = $1 AND corrects_id = $2 AND status <> 'abandoned'`, orgID, correctedID).Scan(&id); err != nil {
		return "", fmt.Errorf("get correction: %w", err)
	}
	return id, nil
}

// ListAttached returns the photos on an inspection's answers, by item key,
// oldest attachment first; itemKey "" means every item. It reads only the
// photo's id and capture time from evidence: not a view of the evidence (see
// CLAUDE.md, "Evidence reads are logged").
func ListAttached(ctx context.Context, db DBTX, orgID, inspectionID, itemKey string) (map[string][]AttachedEvidence, error) {
	rows, err := db.Query(ctx, `
		SELECT l.item_key, l.evidence_id, e.client_captured_at, e.client_captured_at < i.client_started_at, l.attached_at
		FROM inspection_response_evidence l
		JOIN evidence e ON e.id = l.evidence_id
		JOIN inspections i ON i.id = l.inspection_id
		WHERE l.org_id = $1 AND l.inspection_id = $2 AND ($3 = '' OR l.item_key = $3)
		ORDER BY l.attached_at, l.evidence_id
	`, orgID, inspectionID, itemKey)
	if err != nil {
		return nil, fmt.Errorf("list attached evidence: %w", err)
	}
	defer rows.Close()

	out := map[string][]AttachedEvidence{}
	for rows.Next() {
		var (
			key string
			a   AttachedEvidence
		)
		if err := rows.Scan(&key, &a.EvidenceID, &a.ClientCapturedAt, &a.CapturedBeforeStart, &a.AttachedAt); err != nil {
			return nil, fmt.Errorf("scan attached evidence: %w", err)
		}
		a.ClientCapturedAt = a.ClientCapturedAt.UTC()
		a.AttachedAt = a.AttachedAt.UTC()
		out[key] = append(out[key], a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list attached evidence: %w", err)
	}
	return out, nil
}

// GetItem is one item of an inspection with its photos.
func GetItem(ctx context.Context, db DBTX, orgID, inspectionID, itemKey string) (*InspectionItem, error) {
	var it InspectionItem
	err := db.QueryRow(ctx, `
		SELECT item_key, position, prompt, item_source_ref, response_type, result, severity, comment,
		       client_answered_at, answered_at
		FROM inspection_responses
		WHERE org_id = $1 AND inspection_id = $2 AND item_key = $3
	`, orgID, inspectionID, itemKey).Scan(&it.Key, &it.Position, &it.Prompt, &it.SourceRef, &it.ResponseType, &it.Result, &it.Severity,
		&it.Comment, &it.ClientAnsweredAt, &it.AnsweredAt)
	if err != nil {
		return nil, fmt.Errorf("get inspection item: %w", err)
	}
	it.ClientAnsweredAt = utc(it.ClientAnsweredAt)
	it.AnsweredAt = utc(it.AnsweredAt)
	attached, err := ListAttached(ctx, db, orgID, inspectionID, itemKey)
	if err != nil {
		return nil, err
	}
	it.Evidence = attached[itemKey]
	if it.Evidence == nil {
		it.Evidence = []AttachedEvidence{}
	}
	return &it, nil
}

// GetSiteInspectionForUpdate locks the inspection row until the transaction
// ends. Answering, completing and abandoning all take this lock first, so
// they run one at a time per inspection.
func GetSiteInspectionForUpdate(ctx context.Context, db DBTX, orgID, siteID, id string) (*Inspection, error) {
	in, err := scanInspection(db.QueryRow(ctx, selectInspection+`WHERE i.org_id = $1 AND i.site_id = $2 AND i.id = $3 FOR UPDATE`, orgID, siteID, id))
	if err != nil {
		return nil, fmt.Errorf("get inspection for update: %w", err)
	}
	return in, nil
}

// EvidenceStatuses returns the status of each of ids that is evidence on
// siteID. An id missing from the result is unknown or on another site.
func EvidenceStatuses(ctx context.Context, db DBTX, orgID, siteID string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT id::text, status FROM evidence WHERE org_id = $1 AND site_id = $2 AND id = ANY($3::uuid[])`, orgID, siteID, ids)
	if err != nil {
		return nil, fmt.Errorf("evidence statuses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("scan evidence status: %w", err)
		}
		out[id] = status
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidence statuses: %w", err)
	}
	return out, nil
}

// SaveAnswer replaces an item's answer and its photos. answered_at is the
// server's clock.
func SaveAnswer(ctx context.Context, db DBTX, orgID, siteID, inspectionID, itemKey string, req AnswerRequest) error {
	tag, err := db.Exec(ctx, `
		UPDATE inspection_responses
		SET result = $4, severity = $5, comment = $6, client_answered_at = $7, answered_at = now()
		WHERE org_id = $1 AND inspection_id = $2 AND item_key = $3
	`, orgID, inspectionID, itemKey, req.Result, req.Severity, req.Comment, req.ClientAnsweredAt)
	if err != nil {
		return fmt.Errorf("save answer: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("save answer: changed %d rows", tag.RowsAffected())
	}
	if _, err := db.Exec(ctx, `DELETE FROM inspection_response_evidence WHERE org_id = $1 AND inspection_id = $2 AND item_key = $3`,
		orgID, inspectionID, itemKey); err != nil {
		return fmt.Errorf("save answer: remove photos: %w", err)
	}
	if len(req.EvidenceIDs) == 0 {
		return nil
	}
	tag, err = db.Exec(ctx, `
		INSERT INTO inspection_response_evidence (org_id, site_id, inspection_id, item_key, evidence_id)
		SELECT $1, $2, $3, $4, unnest($5::uuid[])
	`, orgID, siteID, inspectionID, itemKey, req.EvidenceIDs)
	if err != nil {
		return fmt.Errorf("save answer: attach photos: %w", err)
	}
	if int(tag.RowsAffected()) != len(req.EvidenceIDs) {
		return fmt.Errorf("save answer: attached %d of %d photos", tag.RowsAffected(), len(req.EvidenceIDs))
	}
	return nil
}

// CompleteInspection marks it completed. The database checks it is complete
// and raises one finding per fail (inspections_guard, inspections_raise_findings).
func CompleteInspection(ctx context.Context, db DBTX, orgID, id string, clientCompletedAt time.Time) error {
	tag, err := db.Exec(ctx, `
		UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = $3
		WHERE org_id = $1 AND id = $2
	`, orgID, id, clientCompletedAt)
	if err != nil {
		return fmt.Errorf("complete inspection: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("complete inspection: changed %d rows", tag.RowsAffected())
	}
	return nil
}

// ListInspectionFindings returns an inspection's findings in template order.
func ListInspectionFindings(ctx context.Context, db DBTX, orgID, inspectionID string) ([]InspectionFinding, error) {
	rows, err := db.Query(ctx, `
		SELECT f.item_key, f.severity, f.raised_at
		FROM inspection_findings f
		JOIN inspection_responses r ON r.inspection_id = f.inspection_id AND r.item_key = f.item_key
		WHERE f.org_id = $1 AND f.inspection_id = $2
		ORDER BY r.position
	`, orgID, inspectionID)
	if err != nil {
		return nil, fmt.Errorf("list inspection findings: %w", err)
	}
	defer rows.Close()
	out := []InspectionFinding{}
	for rows.Next() {
		var f InspectionFinding
		if err := rows.Scan(&f.ItemKey, &f.Severity, &f.RaisedAt); err != nil {
			return nil, fmt.Errorf("scan inspection finding: %w", err)
		}
		f.RaisedAt = f.RaisedAt.UTC()
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list inspection findings: %w", err)
	}
	return out, nil
}

// ListSiteFindings returns a site's findings, newest first, then by
// inspection and template order. severity "" means any.
func ListSiteFindings(ctx context.Context, db DBTX, orgID, siteID, severity, coverage string, limit int) ([]Finding, error) {
	rows, err := db.Query(ctx, `
		SELECT f.inspection_id, f.site_id, f.item_key, f.severity, f.raised_at,
		       i.template_key, i.template_version, i.template_title, r.prompt, c.id,
		       cov.coverage, cov.action_ids::text[], cov.waiver_id
		FROM inspection_findings f
		CROSS JOIN LATERAL finding_coverage(f.inspection_id, f.item_key) cov
		JOIN inspections i ON i.id = f.inspection_id
		JOIN inspection_responses r ON r.inspection_id = f.inspection_id AND r.item_key = f.item_key
		-- At most one: inspections_corrects_once allows one correction that
		-- isn't abandoned, and only a completed one supersedes.
		LEFT JOIN inspections c ON c.corrects_id = f.inspection_id AND c.status = 'completed'
		WHERE f.org_id = $1 AND f.site_id = $2 AND ($3 = '' OR f.severity = $3) AND ($5 = '' OR cov.coverage = $5)
		ORDER BY f.raised_at DESC, f.inspection_id, r.position
		LIMIT $4
	`, orgID, siteID, severity, limit, coverage)
	if err != nil {
		return nil, fmt.Errorf("list site findings: %w", err)
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		var f Finding
		if err := rows.Scan(&f.InspectionID, &f.SiteID, &f.ItemKey, &f.Severity, &f.RaisedAt,
			&f.TemplateKey, &f.TemplateVersion, &f.TemplateTitle, &f.Prompt, &f.CorrectedBy, &f.Coverage, &f.CoveredBy, &f.WaiverID); err != nil {
			return nil, fmt.Errorf("scan site finding: %w", err)
		}
		f.RaisedAt = f.RaisedAt.UTC()
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list site findings: %w", err)
	}
	return out, nil
}

// HasFail reports whether any item of the inspection is answered "fail".
func HasFail(ctx context.Context, db DBTX, orgID, inspectionID string) (bool, error) {
	var has bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM inspection_responses WHERE org_id = $1 AND inspection_id = $2 AND result = 'fail')`,
		orgID, inspectionID).Scan(&has)
	if err != nil {
		return false, fmt.Errorf("has fail: %w", err)
	}
	return has, nil
}

// AbandonInspection marks it abandoned, by whom and why. The database keeps
// its answers and refuses it if a fail would be abandoned by anyone but an
// owner or a SHE officer (inspections_guard, 000014).
func AbandonInspection(ctx context.Context, db DBTX, orgID, id, by, reason string) error {
	tag, err := db.Exec(ctx, `
		UPDATE inspections SET status = 'abandoned', abandoned_at = now(), abandoned_by = $3, abandon_reason = $4
		WHERE org_id = $1 AND id = $2
	`, orgID, id, by, reason)
	if err != nil {
		return fmt.Errorf("abandon inspection: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("abandon inspection: changed %d rows", tag.RowsAffected())
	}
	return nil
}
