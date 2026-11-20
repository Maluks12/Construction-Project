package exports

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Query: every value is read as of AsOf (D10), from history or set-once times.
type Query struct {
	OrgID, SiteID  string
	AsOf           time.Time
	From, To       string
	TimeZone       string
	IncludesHealth bool
}

type Section struct {
	Key         string          `json:"key"`
	Title       string          `json:"title"`
	Records     json.RawMessage `json:"records"`
	EvidenceIDs []string        `json:"-"`
	FileIDs     []string        `json:"-"`
}

// Contributor: an edition's own sections, read in the pack's snapshot.
type Contributor interface {
	PackSections(ctx context.Context, tx pgx.Tx, q Query) ([]Section, error)
}

// RunSection: (records, evidence ids, file ids) from $1 org, $2 site, $3 as_of,
// $4 from, $5 to, $6 zone, $7 includes health.
func RunSection(ctx context.Context, tx pgx.Tx, q Query, key, title, sql string) (Section, error) {
	s := Section{Key: key, Title: title}
	var records []byte
	// Every parameter typed once, so a section may leave some unused.
	wrapped := `SELECT s.* FROM (` + sql + `) s, (SELECT $1::uuid, $2::uuid, $3::timestamptz, $4::date, $5::date, $6::text, $7::boolean) p`
	err := tx.QueryRow(ctx, wrapped, q.OrgID, q.SiteID, q.AsOf, q.From, q.To, q.TimeZone, q.IncludesHealth).
		Scan(&records, &s.EvidenceIDs, &s.FileIDs)
	if err != nil {
		return s, fmt.Errorf("section %s: %w", key, err)
	}
	s.Records = records
	if s.EvidenceIDs == nil {
		s.EvidenceIDs = []string{}
	}
	if s.FileIDs == nil {
		s.FileIDs = []string{}
	}
	return s, nil
}

func asOfRetraction(alias, at, by, reason string) string {
	return fmt.Sprintf(`CASE WHEN %[1]s.%[2]s <= $3 THEN jsonb_build_object('at', %[1]s.%[2]s, 'by', %[1]s.%[3]s, 'reason', %[1]s.%[4]s) END`,
		alias, at, by, reason)
}

func inRange(expr string) string {
	return fmt.Sprintf(`((%s) AT TIME ZONE $6)::date BETWEEN $4::date AND $5::date`, expr)
}

var vaultSQL = DocumentsSQL(`d.scope = 'org'`)

// DocumentsSQL: the vault here, the H&S file in the edition.
func DocumentsSQL(scope string) string {
	return `
	WITH docs AS (
		SELECT d.id, d.type_key, d.title, d.regime_code, d.created_by, d.created_at,
		       ` + asOfRetraction("d", "retracted_at", "retracted_by", "retract_reason") + ` AS retraction,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object(
		                 'version', v.version, 'file_id', v.file_id, 'issuer', v.issuer, 'reference', v.reference,
		                 'issued_on', v.issued_on, 'expires_on', v.expires_on, 'uploaded_by', v.uploaded_by,
		                 'client_uploaded_at', v.client_uploaded_at, 'received_at', v.received_at,
		                 'retraction', ` + asOfRetraction("v", "retracted_at", "retracted_by", "retract_reason") + `)
		               ORDER BY v.version), '[]')
		        FROM document_versions v WHERE v.document_id = d.id AND v.received_at <= $3) AS versions,
		       ARRAY(SELECT v.file_id::text FROM document_versions v WHERE v.document_id = d.id AND v.received_at <= $3) AS files
		FROM documents d
		WHERE d.org_id = $1 AND ` + scope + ` AND d.created_at <= $3
	)
	SELECT coalesce(jsonb_agg(to_jsonb(docs) - 'files' ORDER BY docs.type_key, docs.created_at, docs.id), '[]'),
	       ARRAY[]::text[],
	       coalesce((SELECT array_agg(f ORDER BY f) FROM docs, unnest(docs.files) f), ARRAY[]::text[])
	FROM docs`
}

// Workers: the roster during the range, plus anyone signed in or appointed.
var workersSQL = `
	WITH pw AS (
		SELECT r.worker_id FROM site_roster r
		WHERE r.org_id = $1 AND r.site_id = $2 AND r.added_at <= $3
		  AND NOT coalesce(r.removed_at <= $3 AND (r.removed_at AT TIME ZONE $6)::date < $4::date, false)
		UNION SELECT s.worker_id FROM site_sign_ins s
		WHERE s.org_id = $1 AND s.site_id = $2 AND s.received_at <= $3 AND s.sign_in_date BETWEEN $4::date AND $5::date
		UNION SELECT a.worker_id FROM site_appointments a WHERE a.org_id = $1 AND a.site_id = $2 AND a.recorded_at <= $3
	),
	ws AS (
		SELECT w.id, h.name, h.firm, h.clock_number, h.trade, h.active,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('added_via', r.added_via, 'added_by', r.added_by, 'added_at', r.added_at,
		                 'removal', CASE WHEN r.removed_at <= $3 THEN jsonb_build_object('at', r.removed_at, 'by', r.removed_by, 'reason', r.remove_reason) END)
		                 ORDER BY r.added_at, r.id), '[]')
		        FROM site_roster r WHERE r.site_id = $2 AND r.worker_id = w.id AND r.added_at <= $3) AS roster,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', c.id, 'regime_code', c.regime_code, 'type_key', c.type_key,
		                 'number', c.number, 'issuer', c.issuer, 'issued_on', c.issued_on, 'expires_on', c.expires_on, 'file_id', c.file_id,
		                 'supersedes_id', c.supersedes_id,
		                 'superseded_by', (SELECT x.id FROM worker_competencies x WHERE x.supersedes_id = c.id AND x.recorded_at <= $3
		                                   AND NOT coalesce(x.retracted_at <= $3, false) ORDER BY x.recorded_at, x.id LIMIT 1),
		                 'recorded_by', c.recorded_by, 'client_recorded_at', c.client_recorded_at, 'recorded_at', c.recorded_at,
		                 'retraction', ` + asOfRetraction("c", "retracted_at", "retracted_by", "retract_reason") + `)
		                 ORDER BY c.recorded_at, c.id), '[]')
		        FROM worker_competencies c WHERE c.worker_id = w.id AND c.recorded_at <= $3) AS competencies,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', m.id, 'regime_code', m.regime_code, 'outcome', m.outcome,
		                 'examined_on', m.examined_on, 'expires_on', m.expires_on, 'practice_number', m.practice_number,
		                 'file_id', CASE WHEN $7 THEN m.file_id END, 'supersedes_id', m.supersedes_id,
		                 'superseded_by', (SELECT x.id FROM worker_medicals x WHERE x.supersedes_id = m.id AND x.recorded_at <= $3
		                                   AND NOT coalesce(x.retracted_at <= $3, false) ORDER BY x.recorded_at, x.id LIMIT 1),
		                 'recorded_by', m.recorded_by, 'client_recorded_at', m.client_recorded_at, 'recorded_at', m.recorded_at,
		                 'retraction', ` + asOfRetraction("m", "retracted_at", "retracted_by", "retract_reason") + `)
		                 ORDER BY m.recorded_at, m.id), '[]')
		        FROM worker_medicals m WHERE m.worker_id = w.id AND m.recorded_at <= $3) AS medicals,
		       ARRAY(SELECT c.file_id::text FROM worker_competencies c WHERE c.worker_id = w.id AND c.recorded_at <= $3
		             UNION SELECT m.file_id::text FROM worker_medicals m WHERE $7 AND m.worker_id = w.id AND m.recorded_at <= $3) AS files
		FROM workers w
		JOIN LATERAL (SELECT * FROM worker_history h WHERE h.worker_id = w.id AND h.changed_at <= $3
		              ORDER BY h.changed_at DESC, h.id DESC LIMIT 1) h ON true
		WHERE w.org_id = $1 AND w.id IN (SELECT worker_id FROM pw)
	)
	SELECT coalesce(jsonb_agg(to_jsonb(ws) - 'files' ORDER BY ws.name, ws.id), '[]'),
	       ARRAY[]::text[],
	       coalesce((SELECT array_agg(DISTINCT f ORDER BY f) FROM ws, unnest(ws.files) f), ARRAY[]::text[])
	FROM ws`

// Inspections in progress at as_of are headers only: their answers were still
// being overwritten.
var inspectionsSQL = `
	WITH ins AS (
		SELECT i.id, i.template_key, i.template_version, encode(i.template_sha256, 'hex') AS template_sha256, i.template_title,
		       i.template_source_ref, i.item_count, i.started_by, i.client_started_at, i.started_at, i.corrects_id,
		       CASE WHEN i.completed_at <= $3 THEN 'completed' WHEN i.abandoned_at <= $3 THEN 'abandoned'
		            ELSE 'in_progress_at_cut_off' END AS status,
		       CASE WHEN i.completed_at <= $3 THEN i.completed_at END AS completed_at,
		       CASE WHEN i.completed_at <= $3 THEN i.client_completed_at END AS client_completed_at,
		       CASE WHEN i.abandoned_at <= $3 THEN jsonb_build_object('at', i.abandoned_at, 'by', i.abandoned_by, 'reason', i.abandon_reason) END AS abandoned,
		       CASE WHEN i.completed_at <= $3 OR i.abandoned_at <= $3 THEN
		           (SELECT coalesce(jsonb_agg(jsonb_build_object('item_key', r.item_key, 'position', r.position, 'prompt', r.prompt,
		                     'source_ref', r.item_source_ref, 'result', r.result, 'severity', r.severity, 'comment', r.comment,
		                     'client_answered_at', r.client_answered_at, 'answered_at', r.answered_at,
		                     'evidence_ids', (SELECT coalesce(jsonb_agg(e.evidence_id ORDER BY e.evidence_id), '[]')
		                                      FROM inspection_response_evidence e WHERE e.inspection_id = r.inspection_id AND e.item_key = r.item_key))
		                     ORDER BY r.position), '[]')
		            FROM inspection_responses r WHERE r.inspection_id = i.id) END AS items,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('item_key', f.item_key, 'severity', f.severity, 'raised_at', f.raised_at)
		                 ORDER BY f.item_key), '[]')
		        FROM inspection_findings f WHERE f.inspection_id = i.id AND f.raised_at <= $3) AS findings,
		       CASE WHEN i.completed_at <= $3 OR i.abandoned_at <= $3 THEN
		           ARRAY(SELECT e.evidence_id::text FROM inspection_response_evidence e WHERE e.inspection_id = i.id) END AS photos
		FROM inspections i
		WHERE i.org_id = $1 AND i.site_id = $2 AND i.started_at <= $3 AND ` + inRange("i.started_at") + `
	)
	SELECT coalesce(jsonb_agg(to_jsonb(ins) - 'photos' ORDER BY ins.started_at, ins.id), '[]'),
	       coalesce((SELECT array_agg(DISTINCT p ORDER BY p) FROM ins, unnest(ins.photos) p), ARRAY[]::text[]),
	       ARRAY[]::text[]
	FROM ins`

var packSources = `
	sources AS (
		SELECT i.id FROM inspections i WHERE i.org_id = $1 AND i.site_id = $2 AND i.started_at <= $3 AND ` + inRange("i.started_at") + `
		UNION SELECT x.id FROM incidents x WHERE x.org_id = $1 AND x.site_id = $2 AND x.created_at <= $3 AND ` + inRange(
	`SELECT v.occurred_at FROM incident_versions v WHERE v.incident_id = x.id AND v.submitted_at <= $3 ORDER BY v.version DESC LIMIT 1`) + `
	)`

// Actions: the state as of as_of is the last event's (events carry the state after them).
var actionsSQL = `
	WITH ` + packSources + `,
	acts AS (
		SELECT a.id, a.inspection_id, a.item_key, a.incident_id, a.incident_version, a.severity, a.title, a.description,
		       a.raised_by, a.client_raised_at, a.raised_at, a.time_zone, a.default_due_on,
		       (SELECT jsonb_build_object('status', e.status, 'owner_id', e.owner_id, 'responsible_party', e.responsible_party, 'due_on', e.due_on)
		        FROM corrective_action_events e WHERE e.action_id = a.id AND e.recorded_at <= $3 ORDER BY e.seq DESC LIMIT 1) AS state,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', e.id, 'seq', e.seq, 'kind', e.kind, 'actor_id', e.actor_id,
		                 'client_at', e.client_at, 'recorded_at', e.recorded_at, 'reason', e.reason, 'status', e.status,
		                 'owner_id', e.owner_id, 'responsible_party', e.responsible_party, 'due_on', e.due_on,
		                 'closeout_id', e.closeout_id, 'self_verified', e.self_verified) ORDER BY e.seq), '[]')
		        FROM corrective_action_events e WHERE e.action_id = a.id AND e.recorded_at <= $3) AS events,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', c.id, 'note', c.note, 'submitted_by', c.submitted_by,
		                 'client_submitted_at', c.client_submitted_at, 'submitted_at', c.submitted_at,
		                 'evidence_ids', (SELECT coalesce(jsonb_agg(x.evidence_id ORDER BY x.evidence_id), '[]')
		                                  FROM corrective_action_closeout_evidence x WHERE x.closeout_id = c.id)) ORDER BY c.submitted_at, c.id), '[]')
		        FROM corrective_action_closeouts c WHERE c.action_id = a.id AND c.submitted_at <= $3) AS closeouts,
		       ARRAY(SELECT x.evidence_id::text FROM corrective_action_closeouts c JOIN corrective_action_closeout_evidence x ON x.closeout_id = c.id
		             WHERE c.action_id = a.id AND c.submitted_at <= $3) AS photos
		FROM corrective_actions a
		WHERE a.org_id = $1 AND a.site_id = $2 AND a.raised_at <= $3
		  AND (` + inRange("a.raised_at") + ` OR a.inspection_id IN (SELECT id FROM sources) OR a.incident_id IN (SELECT id FROM sources))
	),
	waivers AS (
		SELECT w.id, w.inspection_id, w.item_key, w.severity, w.reason, w.waived_by, w.client_waived_at, w.waived_at,
		       ` + asOfRetraction("w", "retracted_at", "retracted_by", "retract_reason") + ` AS retraction
		FROM finding_waivers w
		WHERE w.org_id = $1 AND w.site_id = $2 AND w.waived_at <= $3 AND w.inspection_id IN (SELECT id FROM sources)
	)
	SELECT jsonb_build_object(
	           'actions', (SELECT coalesce(jsonb_agg(to_jsonb(acts) - 'photos' ORDER BY acts.raised_at, acts.id), '[]') FROM acts),
	           'waivers', (SELECT coalesce(jsonb_agg(to_jsonb(waivers) ORDER BY waivers.waived_at, waivers.id), '[]') FROM waivers)),
	       coalesce((SELECT array_agg(DISTINCT p ORDER BY p) FROM acts, unnest(acts.photos) p), ARRAY[]::text[]),
	       ARRAY[]::text[]`

// Incidents are in range by their latest version's occurred_at as of as_of.
var incidentsSQL = `
	WITH inc AS (
		SELECT x.id, x.reported_by, x.created_at,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('version', v.version, 'taxonomy_codes', v.taxonomy_codes,
		                 'occurred_at', v.occurred_at, 'employer_aware_at', v.employer_aware_at, 'location_text', v.location_text,
		                 'description', v.description, 'submitted_by', v.submitted_by, 'client_submitted_at', v.client_submitted_at,
		                 'submitted_at', v.submitted_at, 'amend_reason', v.amend_reason,
		                 'people', (SELECT coalesce(jsonb_agg(jsonb_build_object('position', p.position, 'name', p.name,
		                                'occupation', p.occupation, 'employee_of_org', p.employee_of_org, 'employer_name', p.employer_name,
		                                'injured', CASE WHEN $7 THEN p.injured END,
		                                'injury_description', CASE WHEN $7 THEN p.injury_description END,
		                                'body_part', CASE WHEN $7 THEN p.body_part END,
		                                'treatment', CASE WHEN $7 THEN p.treatment END,
		                                'died_at', CASE WHEN $7 THEN p.died_at END) ORDER BY p.position), '[]')
		                            FROM incident_people p WHERE p.incident_id = v.incident_id AND p.version = v.version),
		                 'evidence_ids', (SELECT coalesce(jsonb_agg(e.evidence_id ORDER BY e.evidence_id), '[]')
		                                  FROM incident_version_evidence e WHERE e.incident_id = v.incident_id AND e.version = v.version))
		                 ORDER BY v.version), '[]')
		        FROM incident_versions v WHERE v.incident_id = x.id AND v.submitted_at <= $3) AS versions,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', w.id, 'witness_name', w.witness_name, 'witness_contact', w.witness_contact,
		                 'statement', w.statement, 'taken_by', w.taken_by, 'taken_at', w.taken_at, 'recorded_at', w.recorded_at,
		                 'evidence_id', w.evidence_id, 'supersedes_id', w.supersedes_id) ORDER BY w.recorded_at, w.id), '[]')
		        FROM witness_statements w WHERE w.incident_id = x.id AND w.recorded_at <= $3) AS witness_statements,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', d.id, 'duty_id', d.duty_id, 'rule_key', d.rule_key, 'label', d.rule_label,
		                 'kind', d.kind, 'period', d.period, 'unit', d.unit, 'basis', d.basis, 'source_ref', d.source_ref, 'caveat', d.caveat,
		                 'basis_at', d.basis_at, 'time_zone', d.time_zone, 'due_at', d.due_at, 'due_on', d.due_on,
		                 'excluded_days', d.excluded_days, 'legal_latest_on', d.legal_latest_on, 'legal_latest_at', d.legal_latest_at,
		                 'legal_latest_source_ref', d.legal_latest_source_ref, 'computed_in_version', d.computed_in_version,
		                 'computed_at', d.computed_at, 'added_by_satisfaction_id', d.added_by_satisfaction_id,
		                 'closed', CASE WHEN d.closed_at <= $3 THEN jsonb_build_object('as', d.closed_as, 'by', d.closed_by, 'at', d.closed_at,
		                                'reason', d.closed_reason, 'superseded_by', d.superseded_by, 'in_version', d.closed_in_version,
		                                'recomputed_for', d.recomputed_for) END,
		                 'satisfactions', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', s.id, 'satisfied_at', s.satisfied_at,
		                                       'method', s.method, 'reference', s.reference, 'note', s.note, 'recorded_by', s.recorded_by,
		                                       'recorded_at', s.recorded_at, 'evidence_id', s.evidence_id,
		                                       'retraction', (SELECT jsonb_build_object('at', r.retracted_at, 'by', r.retracted_by, 'reason', r.reason)
		                                                      FROM deadline_satisfaction_retractions r WHERE r.satisfaction_id = s.id AND r.retracted_at <= $3))
		                                       ORDER BY s.recorded_at, s.id), '[]')
		                                   FROM deadline_satisfactions s WHERE s.duty_id = d.duty_id AND s.recorded_at <= $3))
		                 ORDER BY d.computed_at, d.id), '[]')
		        FROM deadlines d WHERE d.incident_id = x.id AND d.computed_at <= $3) AS deadlines,
		       ARRAY(SELECT e.evidence_id::text FROM incident_versions v JOIN incident_version_evidence e
		                 ON e.incident_id = v.incident_id AND e.version = v.version WHERE v.incident_id = x.id AND v.submitted_at <= $3
		             UNION SELECT w.evidence_id::text FROM witness_statements w WHERE w.incident_id = x.id AND w.recorded_at <= $3 AND w.evidence_id IS NOT NULL
		             UNION SELECT s.evidence_id::text FROM deadline_satisfactions s JOIN deadline_duties du ON du.id = s.duty_id
		                 WHERE du.incident_id = x.id AND s.recorded_at <= $3 AND s.evidence_id IS NOT NULL) AS photos,
		       (SELECT v.occurred_at FROM incident_versions v WHERE v.incident_id = x.id AND v.submitted_at <= $3
		        ORDER BY v.version DESC LIMIT 1) AS occurred_at
		FROM incidents x
		WHERE x.org_id = $1 AND x.site_id = $2 AND x.created_at <= $3
	)
	SELECT coalesce(jsonb_agg(to_jsonb(inc) - 'photos' ORDER BY inc.occurred_at, inc.id) FILTER (WHERE ` + inRange("inc.occurred_at") + `), '[]'),
	       coalesce((SELECT array_agg(DISTINCT p ORDER BY p) FROM inc, unnest(inc.photos) p WHERE ` + inRange("inc.occurred_at") + `), ARRAY[]::text[]),
	       ARRAY[]::text[]
	FROM inc`

var toolboxSQL = `
	WITH talks AS (
		SELECT t.id, t.talk_date, t.time_zone, t.recorded_by, t.created_at,
		       h.topic_key, h.topic, h.presenter_name, h.notes, h.started_at, h.ended_at,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('evidence_id', e.evidence_id, 'kind', e.kind, 'added_by', e.added_by,
		                 'added_at', e.added_at) ORDER BY e.added_at, e.evidence_id), '[]')
		        FROM toolbox_talk_evidence e WHERE e.talk_id = t.id AND e.added_at <= $3) AS photos_added,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', a.id, 'name', a.name, 'firm', a.firm, 'clock_number', a.clock_number,
		                 'method', a.method, 'recorded_by', a.recorded_by, 'client_signed_at', a.client_signed_at, 'signed_at', a.signed_at,
		                 'retraction', (SELECT jsonb_build_object('at', r.retracted_at, 'by', r.retracted_by, 'reason', r.reason,
		                                    'client_at', r.client_retracted_at)
		                                FROM toolbox_attendance_retractions r WHERE r.attendance_id = a.id AND r.retracted_at <= $3),
		                 'worker_links', (SELECT coalesce(jsonb_agg(jsonb_build_object('worker_id', l.worker_id, 'method', l.method,
		                                       'linked_by', l.linked_by, 'linked_at', l.linked_at,
		                                       'unlinked', CASE WHEN l.unlinked_at <= $3 THEN jsonb_build_object('at', l.unlinked_at,
		                                                       'by', l.unlinked_by, 'reason', l.unlink_reason) END)
		                                       ORDER BY l.linked_at, l.id), '[]')
		                                  FROM toolbox_attendance_workers l WHERE l.attendance_id = a.id AND l.linked_at <= $3))
		                 ORDER BY a.signed_at, a.id), '[]')
		        FROM toolbox_attendance a WHERE a.talk_id = t.id AND a.signed_at <= $3) AS attendance,
		       ARRAY(SELECT e.evidence_id::text FROM toolbox_talk_evidence e WHERE e.talk_id = t.id AND e.added_at <= $3) AS photos
		FROM toolbox_talks t
		JOIN LATERAL (SELECT * FROM toolbox_talk_history h WHERE h.talk_id = t.id AND h.changed_at <= $3
		              ORDER BY h.changed_at DESC, h.id DESC LIMIT 1) h ON true
		WHERE t.org_id = $1 AND t.site_id = $2 AND t.created_at <= $3 AND t.talk_date BETWEEN $4::date AND $5::date
	)
	SELECT coalesce(jsonb_agg(to_jsonb(talks) - 'photos' ORDER BY talks.started_at, talks.id), '[]'),
	       coalesce((SELECT array_agg(DISTINCT p ORDER BY p) FROM talks, unnest(talks.photos) p), ARRAY[]::text[]),
	       ARRAY[]::text[]
	FROM talks`

var signInsSQL = `
	WITH si AS (
		SELECT s.id, s.worker_id, s.name, s.firm, s.clock_number, s.method, s.matched_existing, s.recorded_by,
		       s.client_signed_in_at, s.received_at, s.time_zone, s.sign_in_date, s.received_after_close,
		       CASE WHEN s.sign_out_received_at <= $3 THEN jsonb_build_object('client_at', s.client_signed_out_at,
		            'method', s.sign_out_method, 'by', s.signed_out_by, 'received_at', s.sign_out_received_at) END AS sign_out,
		       ` + asOfRetraction("s", "retracted_at", "retracted_by", "retract_reason") + ` AS retraction
		FROM site_sign_ins s
		WHERE s.org_id = $1 AND s.site_id = $2 AND s.received_at <= $3 AND s.sign_in_date BETWEEN $4::date AND $5::date
	),
	sheets AS (
		SELECT h.sheet_date, h.evidence_id, h.added_by, h.added_at FROM site_sign_in_sheets h
		WHERE h.org_id = $1 AND h.site_id = $2 AND h.added_at <= $3 AND h.sheet_date BETWEEN $4::date AND $5::date
	)
	SELECT jsonb_build_object(
	           'sign_ins', (SELECT coalesce(jsonb_agg(to_jsonb(si) ORDER BY si.client_signed_in_at, si.id), '[]') FROM si),
	           'sheets', (SELECT coalesce(jsonb_agg(to_jsonb(sheets) ORDER BY sheets.sheet_date, sheets.evidence_id), '[]') FROM sheets)),
	       coalesce((SELECT array_agg(DISTINCT evidence_id::text ORDER BY evidence_id::text) FROM sheets), ARRAY[]::text[]),
	       ARRAY[]::text[]`

var receivedPhotosSQL = `
	SELECT '[]'::jsonb,
	       coalesce((SELECT array_agg(c.evidence_id::text ORDER BY c.evidence_id::text) FROM evidence_chain c
	                 WHERE c.org_id = $1 AND c.site_id = $2 AND c.server_received_at <= $3 AND ` + inRange("c.server_received_at") + `), ARRAY[]::text[]),
	       ARRAY[]::text[]`

// membersSQL lets the pack's actor ids be read.
const membersSQL = `
	SELECT coalesce(jsonb_agg(jsonb_build_object('user_id', m.user_id, 'full_name', m.full_name, 'role', m.role, 'status', m.status)
	           ORDER BY m.user_id), '[]')
	FROM (SELECT DISTINCT ON (h.user_id) h.user_id, h.full_name, h.role, h.status FROM member_history h
	      WHERE h.org_id = $1 AND h.changed_at <= $2 ORDER BY h.user_id, h.changed_at DESC, h.id DESC) m`

const organisationSQL = `
	SELECT to_jsonb(o) FROM (
		SELECT h.trading_name, h.registered_name, h.reg_no, h.vat_no, h.coida_insurer, h.archived_at
		FROM organisation_history h WHERE h.org_id = $1 AND h.changed_at <= $2 ORDER BY h.changed_at DESC, h.id DESC LIMIT 1) o`

const siteSQL = `
	SELECT to_jsonb(s) FROM (
		SELECT h.site_id AS id, h.name, h.address, h.lat, h.lng, h.status, h.archived_at,
		       r.code AS regime_code, r.name AS regime_name, r.version AS regime_version, r.edition, r.jurisdiction
		FROM site_history h JOIN regulatory_regimes r ON r.id = h.regime_id
		WHERE h.site_id = $1 AND h.changed_at <= $2 ORDER BY h.changed_at DESC, h.id DESC LIMIT 1) s`

func coreSections(ctx context.Context, tx pgx.Tx, q Query) ([]Section, error) {
	specs := []struct{ key, title, sql string }{
		{"vault", "Statutory documents (the organisation's vault)", vaultSQL},
		{"workers", "Workers on site, with competencies and medical certificates of fitness", workersSQL},
		{"inspections", "Inspections", inspectionsSQL},
		{"actions", "Corrective actions and waivers", actionsSQL},
		{"incidents", "Incident register, with reporting deadlines", incidentsSQL},
		{"toolbox_talks", "Toolbox talks", toolboxSQL},
		{"sign_ins", "Daily sign-in register", signInsSQL},
		{"received_photos", "Photos received in the period", receivedPhotosSQL},
	}
	out := make([]Section, 0, len(specs))
	for _, sp := range specs {
		s, err := RunSection(ctx, tx, q, sp.key, sp.title, sp.sql)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
