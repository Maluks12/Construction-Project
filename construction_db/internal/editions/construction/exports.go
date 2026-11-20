package construction

import (
	"context"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/exports"
)

// ExportsContributor gives the audit pack the edition's own sections: the
// H&S file, appointments and the site diary (M12), read in the pack's
// snapshot as of its as_of.
func (Edition) ExportsContributor() exports.Contributor { return packSections{} }

type packSections struct{}

func (packSections) PackSections(ctx context.Context, tx pgx.Tx, q exports.Query) ([]exports.Section, error) {
	specs := []struct{ key, title, sql string }{
		{"hs_file", "Health and safety file (CR 2014 reg 7(1)(b))", exports.DocumentsSQL(`d.scope = 'site' AND d.site_id = $2`)},
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
