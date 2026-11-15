package incidents

import (
	"context"
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

// header is an incidents row.
type header struct {
	ID, SiteID, RegimeID, ReportedBy string
	CreatedAt                        time.Time
	LatestVersion                    int
}

func getHeader(ctx context.Context, db DBTX, orgID, siteID, id string) (*header, error) {
	return readHeader(ctx, db, orgID, siteID, id, "")
}

// lockHeader takes the lock a new version's insert takes on its incident
// (000016's UPDATE of latest_version), so an amendment and a satisfaction of
// the same incident queue, and the second reads what the first wrote. NO KEY
// UPDATE doesn't block the key-share locks of foreign key checks (statements,
// photos).
func lockHeader(ctx context.Context, db DBTX, orgID, siteID, id string) (*header, error) {
	return readHeader(ctx, db, orgID, siteID, id, " FOR NO KEY UPDATE")
}

func readHeader(ctx context.Context, db DBTX, orgID, siteID, id, lock string) (*header, error) {
	var h header
	err := db.QueryRow(ctx, `
		SELECT id, site_id, regime_id, reported_by, created_at, latest_version
		FROM incidents WHERE org_id = $1 AND site_id = $2 AND id = $3`+lock, orgID, siteID, id).
		Scan(&h.ID, &h.SiteID, &h.RegimeID, &h.ReportedBy, &h.CreatedAt, &h.LatestVersion)
	if err != nil {
		return nil, fmt.Errorf("get incident: %w", err)
	}
	h.CreatedAt = h.CreatedAt.UTC()
	return &h, nil
}

// incidentExists reports whether id is an incident of this org anywhere, so
// a version 1 that hits the primary key can tell a race from an id another
// site already uses.
func incidentSite(ctx context.Context, db DBTX, orgID, id string) (string, error) {
	var site string
	err := db.QueryRow(ctx, `SELECT site_id FROM incidents WHERE org_id = $1 AND id = $2`, orgID, id).Scan(&site)
	if err != nil {
		return "", fmt.Errorf("incident site: %w", err)
	}
	return site, nil
}

func siteArchivedAt(ctx context.Context, db DBTX, orgID, siteID string) (*time.Time, error) {
	var at *time.Time
	err := db.QueryRow(ctx, `SELECT CASE WHEN status = 'archived' THEN archived_at END FROM sites WHERE org_id = $1 AND id = $2`,
		orgID, siteID).Scan(&at)
	if err != nil {
		return nil, fmt.Errorf("site archived_at: %w", err)
	}
	return at, nil
}

func insertHeader(ctx context.Context, db DBTX, orgID, siteID, regimeID, id, by string) error {
	_, err := db.Exec(ctx, `INSERT INTO incidents (id, org_id, site_id, regime_id, reported_by) VALUES ($1, $2, $3, $4, $5)`,
		id, orgID, siteID, regimeID, by)
	if err != nil {
		return fmt.Errorf("insert incident: %w", err)
	}
	return nil
}

// storedVersion is a version as stored, before its types get labels.
type storedVersion struct {
	Version
	Codes []string
}

func getVersion(ctx context.Context, db DBTX, orgID, incidentID string, n int) (*storedVersion, error) {
	var v storedVersion
	err := db.QueryRow(ctx, `
		SELECT version, taxonomy_codes, occurred_at, employer_aware_at, location_text, description, submitted_by,
		       client_submitted_at, submitted_at, device_id, amend_reason
		FROM incident_versions WHERE org_id = $1 AND incident_id = $2 AND version = $3`, orgID, incidentID, n).
		Scan(&v.Version.Version, &v.Codes, &v.OccurredAt, &v.EmployerAwareAt, &v.Location, &v.Description, &v.SubmittedBy,
			&v.ClientSubmittedAt, &v.SubmittedAt, &v.DeviceID, &v.AmendReason)
	if err != nil {
		return nil, fmt.Errorf("get incident version: %w", err)
	}
	v.OccurredAt, v.ClientSubmittedAt, v.SubmittedAt = v.OccurredAt.UTC(), v.ClientSubmittedAt.UTC(), v.SubmittedAt.UTC()
	v.EmployerAwareAt = utc(v.EmployerAwareAt)

	rows, err := db.Query(ctx, `
		SELECT position, name, occupation, employee_of_org, employer_name, injured, injury_description, body_part, treatment, died_at
		FROM incident_people WHERE org_id = $1 AND incident_id = $2 AND version = $3 ORDER BY position`, orgID, incidentID, n)
	if err != nil {
		return nil, fmt.Errorf("list incident people: %w", err)
	}
	v.People, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Person, error) {
		var p Person
		err := r.Scan(&p.Position, &p.Name, &p.Occupation, &p.EmployeeOfOrg, &p.EmployerName, &p.Injured, &p.InjuryDescription,
			&p.BodyPart, &p.Treatment, &p.DiedAt)
		p.DiedAt = utc(p.DiedAt)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("list incident people: %w", err)
	}
	if v.People == nil {
		v.People = []Person{}
	}

	// Only the id and capture time: not a view of the evidence.
	rows, err = db.Query(ctx, `
		SELECT l.evidence_id, e.client_captured_at
		FROM incident_version_evidence l JOIN evidence e ON e.id = l.evidence_id
		WHERE l.org_id = $1 AND l.incident_id = $2 AND l.version = $3
		ORDER BY e.client_captured_at, l.evidence_id`, orgID, incidentID, n)
	if err != nil {
		return nil, fmt.Errorf("list incident photos: %w", err)
	}
	v.Evidence, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (AttachedEvidence, error) {
		var a AttachedEvidence
		err := r.Scan(&a.EvidenceID, &a.ClientCapturedAt)
		a.ClientCapturedAt = a.ClientCapturedAt.UTC()
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("list incident photos: %w", err)
	}
	if v.Evidence == nil {
		v.Evidence = []AttachedEvidence{}
	}
	return &v, nil
}

func insertVersion(ctx context.Context, db DBTX, orgID, siteID, incidentID string, n int, by string, req SubmitVersionRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at, employer_aware_at,
		                               location_text, description, submitted_by, client_submitted_at, device_id, amend_reason,
		                               people_count, evidence_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		orgID, siteID, incidentID, n, req.TaxonomyCodes, req.OccurredAt, req.EmployerAwareAt, req.Location, req.Description, by,
		req.ClientSubmittedAt, req.DeviceID, req.AmendReason, len(req.People), len(req.EvidenceIDs))
	if err != nil {
		return fmt.Errorf("insert incident version: %w", err)
	}
	for i, p := range req.People {
		_, err := db.Exec(ctx, `
			INSERT INTO incident_people (org_id, site_id, incident_id, version, position, name, occupation, employee_of_org,
			                             employer_name, injured, injury_description, body_part, treatment, died_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			orgID, siteID, incidentID, n, i+1, p.Name, p.Occupation, p.EmployeeOfOrg, p.EmployerName, p.Injured,
			p.InjuryDescription, p.BodyPart, p.Treatment, p.DiedAt)
		if err != nil {
			return fmt.Errorf("insert incident person: %w", err)
		}
	}
	for _, id := range req.EvidenceIDs {
		_, err := db.Exec(ctx, `
			INSERT INTO incident_version_evidence (org_id, site_id, incident_id, version, evidence_id) VALUES ($1, $2, $3, $4, $5)`,
			orgID, siteID, incidentID, n, id)
		if err != nil {
			return fmt.Errorf("insert incident photo: %w", err)
		}
	}
	return nil
}

// evidenceStatuses returns the status of each of ids that is evidence on
// siteID. An id missing from the result is unknown or on another site.
func evidenceStatuses(ctx context.Context, db DBTX, orgID, siteID string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT id::text, status FROM evidence WHERE org_id = $1 AND site_id = $2 AND id = ANY($3::uuid[])`,
		orgID, siteID, ids)
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

// summaryRow is a register row before its types get labels.
type summaryRow struct {
	IncidentSummary
	Codes []string
}

// listSummaries returns a site's incidents, latest occurrence first, with
// counts from the latest version and its live deadlines. Filters are applied
// by the caller (types and reportability come from regime data).
func listSummaries(ctx context.Context, db DBTX, orgID, siteID string, now time.Time) ([]summaryRow, error) {
	rows, err := db.Query(ctx, `
		SELECT i.id, i.site_id, i.latest_version, v.taxonomy_codes, v.occurred_at, v.location_text, i.reported_by, i.created_at,
		       v.people_count,
		       (SELECT count(*) FROM incident_people p WHERE p.incident_id = i.id AND p.version = v.version AND p.injured),
		       (SELECT count(*) FROM incident_people p WHERE p.incident_id = i.id AND p.version = v.version AND p.died_at IS NOT NULL),
		       count(d.id) FILTER (WHERE du.satisfaction_id IS NULL),
		       count(d.id) FILTER (WHERE du.satisfaction_id IS NULL AND d.unit <> 'immediately' AND d.due_at <= $3),
		       min(d.due_at) FILTER (WHERE du.satisfaction_id IS NULL)
		FROM incidents i
		JOIN incident_versions v ON v.incident_id = i.id AND v.version = i.latest_version
		LEFT JOIN deadlines d ON d.incident_id = i.id AND d.closed_as IS NULL
		LEFT JOIN deadline_duties du ON du.id = d.duty_id
		WHERE i.org_id = $1 AND i.site_id = $2
		GROUP BY i.id, v.incident_id, v.version
		ORDER BY v.occurred_at DESC, i.id`, orgID, siteID, now)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (summaryRow, error) {
		var s summaryRow
		err := r.Scan(&s.ID, &s.SiteID, &s.LatestVersion, &s.Codes, &s.OccurredAt, &s.Location, &s.ReportedBy, &s.CreatedAt,
			&s.PeopleCount, &s.InjuredCount, &s.DeathCount, &s.OpenDeadlines, &s.OverdueDeadlines, &s.NextDueAt)
		s.OccurredAt, s.CreatedAt, s.NextDueAt = s.OccurredAt.UTC(), s.CreatedAt.UTC(), utc(s.NextDueAt)
		return s, err
	})
}

func listStatements(ctx context.Context, db DBTX, orgID, incidentID string) ([]WitnessStatement, error) {
	rows, err := db.Query(ctx, `
		SELECT w.id, w.witness_name, w.witness_contact, w.statement, w.taken_by, w.taken_at, w.recorded_at, w.device_id,
		       w.evidence_id, e.client_captured_at, w.supersedes_id, c.id
		FROM witness_statements w
		LEFT JOIN evidence e ON e.id = w.evidence_id
		LEFT JOIN witness_statements c ON c.supersedes_id = w.id
		WHERE w.org_id = $1 AND w.incident_id = $2
		ORDER BY w.recorded_at, w.id`, orgID, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list statements: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (WitnessStatement, error) { return scanStatement(r) })
	if err != nil {
		return nil, fmt.Errorf("list statements: %w", err)
	}
	if out == nil {
		out = []WitnessStatement{}
	}
	return out, nil
}

func getStatement(ctx context.Context, db DBTX, orgID, id string) (WitnessStatement, string, error) {
	var incident string
	rows, err := db.Query(ctx, `
		SELECT w.id, w.witness_name, w.witness_contact, w.statement, w.taken_by, w.taken_at, w.recorded_at, w.device_id,
		       w.evidence_id, e.client_captured_at, w.supersedes_id, c.id, w.incident_id
		FROM witness_statements w
		LEFT JOIN evidence e ON e.id = w.evidence_id
		LEFT JOIN witness_statements c ON c.supersedes_id = w.id
		WHERE w.org_id = $1 AND w.id = $2`, orgID, id)
	if err != nil {
		return WitnessStatement{}, "", fmt.Errorf("get statement: %w", err)
	}
	st, err := pgx.CollectExactlyOneRow(rows, func(r pgx.CollectableRow) (WitnessStatement, error) {
		return scanStatement(r, &incident)
	})
	if err != nil {
		return WitnessStatement{}, "", fmt.Errorf("get statement: %w", err)
	}
	return st, incident, nil
}

func scanStatement(r pgx.CollectableRow, extra ...any) (WitnessStatement, error) {
	var (
		w          WitnessStatement
		evidenceID *string
		captured   *time.Time
	)
	dest := append([]any{&w.ID, &w.WitnessName, &w.WitnessContact, &w.Statement, &w.TakenBy, &w.TakenAt, &w.RecordedAt,
		&w.DeviceID, &evidenceID, &captured, &w.SupersedesID, &w.SupersededBy}, extra...)
	if err := r.Scan(dest...); err != nil {
		return WitnessStatement{}, err
	}
	w.TakenAt, w.RecordedAt = w.TakenAt.UTC(), w.RecordedAt.UTC()
	if evidenceID != nil {
		w.Evidence = &AttachedEvidence{EvidenceID: *evidenceID, ClientCapturedAt: captured.UTC()}
	}
	return w, nil
}

func insertStatement(ctx context.Context, db DBTX, orgID, siteID, incidentID, id, by string, req StatementRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO witness_statements (id, org_id, site_id, incident_id, witness_name, witness_contact, statement, taken_by,
		                                taken_at, device_id, evidence_id, supersedes_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		id, orgID, siteID, incidentID, req.WitnessName, req.WitnessContact, req.Statement, by, req.TakenAt, req.DeviceID,
		req.EvidenceID, req.SupersedesID)
	if err != nil {
		return fmt.Errorf("insert statement: %w", err)
	}
	return nil
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
