package construction

import (
	"context"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/exports"
)

// ExportsContributor gives the audit pack the edition's own sections: the
// H&S file, site work (REG4), appointments and the site diary (M12), read in
// the pack's snapshot as of its as_of.
func (Edition) ExportsContributor() exports.Contributor { return packSections{} }

type packSections struct{}

func (packSections) PackSections(ctx context.Context, tx pgx.Tx, q exports.Query) ([]exports.Section, error) {
	specs := []struct{ key, title, sql string }{
		{"hs_file", "Health and safety file (CR 2014 reg 7(1)(b))", exports.DocumentsSQL(`d.scope = 'site' AND d.site_id = $2`)},
		{"site_work", "Notifications and permits (CR 2014 reg 3 and 4)", siteWorkSQL},
		{"appointments", "Appointments (CR 2014 reg 8 and others)", appointmentsSQL},
		{"diary", "Site diary", diarySQL},
	}
	out := make([]exports.Section, 0, len(specs))
	for _, sp := range specs {
		s, err := exports.RunSection(ctx, tx, q, sp.key, sp.title, sp.sql)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Site work (REG4) recorded by as_of with a version whose span, as of as_of,
// overlaps the range (R5): from its starts_on or the record's earliest due
// date, whichever is earlier, to its ends_on; a cancelled version ends on
// the day it was submitted, an open-ended one when the next version was, the
// latest open-ended planned one never. Linked documents' files are packed by
// the H&S file section: every version received by as_of, retracted or not.
const siteWorkSQL = `
	WITH sw AS (
		SELECT w.id, w.created_by, w.created_at, lv.starts_on, lv.ends_on,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('version', v.version, 'description', v.description,
		                 'starts_on', v.starts_on, 'ends_on', v.ends_on, 'facts', v.facts, 'status', v.status,
		                 'submitted_by', v.submitted_by, 'client_submitted_at', v.client_submitted_at,
		                 'submitted_at', v.submitted_at, 'amend_reason', v.amend_reason) ORDER BY v.version), '[]')
		        FROM site_work_versions v WHERE v.site_work_id = w.id AND v.submitted_at <= $3) AS versions,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', d.id, 'duty_id', d.duty_id, 'rule_key', d.rule_key, 'label', d.rule_label,
		                 'kind', d.kind, 'period', d.period, 'unit', d.unit, 'direction', d.direction, 'basis', d.basis,
		                 'source_ref', d.source_ref, 'caveat', d.caveat, 'proof_document_types', d.proof_document_types,
		                 'basis_at', d.basis_at, 'time_zone', d.time_zone, 'due_at', d.due_at, 'due_on', d.due_on,
		                 'computed_in_version', d.computed_in_version, 'computed_at', d.computed_at,
		                 'closed', CASE WHEN d.closed_at <= $3 THEN jsonb_build_object('as', d.closed_as, 'by', d.closed_by, 'at', d.closed_at,
		                                'reason', d.closed_reason, 'superseded_by', d.superseded_by, 'in_version', d.closed_in_version,
		                                'recomputed_for', d.recomputed_for) END,
		                 'satisfactions', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', s.id, 'satisfied_at', s.satisfied_at,
		                                       'method', s.method, 'reference', s.reference, 'note', s.note, 'recorded_by', s.recorded_by,
		                                       'recorded_at', s.recorded_at, 'evidence_id', s.evidence_id, 'document_id', s.document_id,
		                                       'document_version', s.document_version,
		                                       'file_id', (SELECT dv.file_id FROM document_versions dv
		                                                   WHERE dv.document_id = s.document_id AND dv.version = s.document_version),
		                                       'retraction', (SELECT jsonb_build_object('at', r.retracted_at, 'by', r.retracted_by, 'reason', r.reason)
		                                                      FROM deadline_satisfaction_retractions r WHERE r.satisfaction_id = s.id AND r.retracted_at <= $3))
		                                       ORDER BY s.recorded_at, s.id), '[]')
		                                   FROM deadline_satisfactions s WHERE s.duty_id = d.duty_id AND s.recorded_at <= $3))
		                 ORDER BY d.computed_at, d.id), '[]')
		        FROM deadlines d WHERE d.site_work_id = w.id AND d.computed_at <= $3) AS deadlines,
		       ARRAY(SELECT s.evidence_id::text FROM deadline_satisfactions s JOIN deadline_duties du ON du.id = s.duty_id
		             WHERE du.site_work_id = w.id AND s.recorded_at <= $3 AND s.evidence_id IS NOT NULL) AS photos
		FROM site_work w
		JOIN LATERAL (SELECT v.starts_on, v.ends_on FROM site_work_versions v
		              WHERE v.site_work_id = w.id AND v.submitted_at <= $3 ORDER BY v.version DESC LIMIT 1) lv ON true
		WHERE w.org_id = $1 AND w.site_id = $2 AND w.created_at <= $3
		  AND EXISTS (
		      SELECT 1 FROM (
		          SELECT least(v.starts_on, (SELECT min(d.due_on) FROM deadlines d
		                                     WHERE d.site_work_id = w.id AND d.computed_at <= $3)) AS span_from,
		                 CASE WHEN v.status = 'cancelled'
		                      THEN least(v.ends_on, (v.submitted_at AT TIME ZONE $6)::date)
		                      ELSE coalesce(v.ends_on, (lead(v.submitted_at) OVER (ORDER BY v.version) AT TIME ZONE $6)::date,
		                                    'infinity'::date) END AS span_to
		          FROM site_work_versions v WHERE v.site_work_id = w.id AND v.submitted_at <= $3) spans
		      WHERE spans.span_from <= $5::date AND spans.span_to >= $4::date)
	)
	SELECT coalesce(jsonb_agg(to_jsonb(sw) - 'photos' ORDER BY sw.starts_on, sw.id), '[]'),
	       coalesce((SELECT array_agg(DISTINCT p ORDER BY p) FROM sw x, unnest(x.photos) p), ARRAY[]::text[]),
	       ARRAY[]::text[]
	FROM sw`

// Appointments recorded by as_of, superseded and retracted as of as_of,
// with their letters.
const appointmentsSQL = `
	WITH ap AS (
		SELECT a.id, a.worker_id, a.regime_code, a.type_key, a.appointed_on, a.expires_on, a.file_id, a.supersedes_id,
		       (SELECT x.id FROM site_appointments x WHERE x.supersedes_id = a.id AND x.recorded_at <= $3
		          AND NOT coalesce(x.retracted_at <= $3, false) ORDER BY x.recorded_at, x.id LIMIT 1) AS superseded_by,
		       a.recorded_by, a.client_recorded_at, a.recorded_at,
		       CASE WHEN a.retracted_at <= $3 THEN jsonb_build_object('at', a.retracted_at, 'by', a.retracted_by, 'reason', a.retract_reason) END AS retraction
		FROM site_appointments a WHERE a.org_id = $1 AND a.site_id = $2 AND a.recorded_at <= $3
	)
	SELECT coalesce(jsonb_agg(to_jsonb(ap) ORDER BY ap.type_key, ap.recorded_at, ap.id), '[]'),
	       ARRAY[]::text[],
	       coalesce((SELECT array_agg(DISTINCT x.file_id::text ORDER BY x.file_id::text) FROM ap x), ARRAY[]::text[])
	FROM ap`

// Diaries dated in the range: every revision and addendum received by
// as_of with its photos, and the current revision, conflict and late flag
// as they stood at as_of (site_diary_replay_as_of, 000038).
const diarySQL = `
	WITH days AS (
		SELECT d.diary_date, d.time_zone, d.opens_at, d.closes_at, d.created_by, d.created_at,
		       (SELECT to_jsonb(r) FROM site_diary_replay_as_of(d.site_id, d.diary_date, $3) r) AS as_of_state,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', v.id, 'seq', v.seq, 'base_revision_id', v.base_revision_id,
		                 'weather_conditions', v.weather_conditions, 'weather_note', v.weather_note, 'headcount', v.headcount,
		                 'headcount_suggested', v.headcount_suggested, 'work_done', v.work_done, 'sections', v.sections,
		                 'saved_by', v.saved_by, 'client_saved_at', v.client_saved_at, 'server_received_at', v.server_received_at,
		                 'saved_after_close', v.saved_after_close, 'received_after_close', v.received_after_close,
		                 'late_reason', v.late_reason,
		                 'evidence_ids', (SELECT coalesce(jsonb_agg(e.evidence_id ORDER BY e.evidence_id), '[]')
		                                  FROM site_diary_revision_evidence e WHERE e.revision_id = v.id)) ORDER BY v.seq), '[]')
		        FROM site_diary_revisions v
		        WHERE v.site_id = d.site_id AND v.diary_date = d.diary_date AND v.server_received_at <= $3) AS revisions,
		       (SELECT coalesce(jsonb_agg(jsonb_build_object('id', a.id, 'reason', a.reason, 'note', a.note, 'sections', a.sections,
		                 'added_by', a.added_by, 'client_added_at', a.client_added_at, 'server_received_at', a.server_received_at,
		                 'evidence_ids', (SELECT coalesce(jsonb_agg(e.evidence_id ORDER BY e.evidence_id), '[]')
		                                  FROM site_diary_addendum_evidence e WHERE e.addendum_id = a.id))
		                 ORDER BY a.server_received_at, a.id), '[]')
		        FROM site_diary_addenda a
		        WHERE a.site_id = d.site_id AND a.diary_date = d.diary_date AND a.server_received_at <= $3) AS addenda,
		       ARRAY(SELECT e.evidence_id::text FROM site_diary_revisions v JOIN site_diary_revision_evidence e ON e.revision_id = v.id
		             WHERE v.site_id = d.site_id AND v.diary_date = d.diary_date AND v.server_received_at <= $3
		             UNION SELECT e.evidence_id::text FROM site_diary_addenda a JOIN site_diary_addendum_evidence e ON e.addendum_id = a.id
		             WHERE a.site_id = d.site_id AND a.diary_date = d.diary_date AND a.server_received_at <= $3) AS photos
		FROM site_diaries d
		WHERE d.org_id = $1 AND d.site_id = $2 AND d.created_at <= $3 AND d.diary_date BETWEEN $4::date AND $5::date
	)
	SELECT coalesce(jsonb_agg(to_jsonb(days) - 'photos' ORDER BY days.diary_date), '[]'),
	       coalesce((SELECT array_agg(DISTINCT p ORDER BY p) FROM days x, unnest(x.photos) p), ARRAY[]::text[]),
	       ARRAY[]::text[]
	FROM days`
