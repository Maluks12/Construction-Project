package database_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/platform/database"
)

// Two orgs with exactly one row in every RLS table, seeded as superuser.
const (
	rlsOrgA      = "20000000-0000-0000-0000-00000000000a"
	rlsOrgB      = "20000000-0000-0000-0000-00000000000b"
	rlsUserA     = "21000000-0000-0000-0000-00000000000a"
	rlsUserB     = "21000000-0000-0000-0000-00000000000b"
	rlsSiteA     = "22000000-0000-0000-0000-00000000000a"
	rlsSiteB     = "22000000-0000-0000-0000-00000000000b"
	rlsSessB     = "24000000-0000-0000-0000-00000000000b"
	rlsEvidenceA = "28000000-0000-0000-0000-00000000000a"
	rlsEvidenceB = "28000000-0000-0000-0000-00000000000b"
	rlsInspA     = "2a000000-0000-0000-0000-00000000000a" // completed, one failed item (medium) with a photo
	rlsInspB     = "2a000000-0000-0000-0000-00000000000b"
	rlsIncA      = "2e000000-0000-0000-0000-00000000000a" // version 1: one injured employee, one photo, one statement
	rlsIncB      = "2e000000-0000-0000-0000-00000000000b"
	rlsDutyA     = "30000000-0000-0000-0000-00000000000a" // GAR 8(1)(b) on rlsIncA: one live deadline, satisfied once then retracted
	rlsDutyB     = "30000000-0000-0000-0000-00000000000b"
	rlsDeadA     = "31000000-0000-0000-0000-00000000000a"
	rlsDeadB     = "31000000-0000-0000-0000-00000000000b"
	rlsActionA   = "33000000-0000-0000-0000-00000000000a" // on rlsInspA's finding, owned by its user, due tomorrow; closed out, awaiting verification
	rlsActionB   = "33000000-0000-0000-0000-00000000000b"
	rlsTalkA     = "3c000000-0000-0000-0000-00000000000a" // one photo, one tap signature (retracted)
	rlsTalkB     = "3c000000-0000-0000-0000-00000000000b"
	rlsAttA      = "3d000000-0000-0000-0000-00000000000a"
	rlsAttB      = "3d000000-0000-0000-0000-00000000000b"
	rlsRevA      = "3e000000-0000-0000-0000-00000000000a" // the late diary of 15 Jan, one photo, one addendum with a photo
	rlsRevB      = "3e000000-0000-0000-0000-00000000000b"
	rlsAddA      = "3f000000-0000-0000-0000-00000000000a"
	rlsAddB      = "3f000000-0000-0000-0000-00000000000b"
	rlsFileA     = "40000000-0000-0000-0000-00000000000a" // a verified competency certificate, viewed once
	rlsExportA   = "4b000000-0000-0000-0000-00000000000a" // a built audit export (M12)
	rlsFileB     = "40000000-0000-0000-0000-00000000000b"
	rlsWorkA     = "50000000-0000-0000-0000-00000000000a" // site work, version 1: excavation from 1 Dec 2026
	rlsWorkB     = "50000000-0000-0000-0000-00000000000b"
	rlsDiaryDate = "2026-01-15"
)

// Inserts an open action on a finding; its raise must follow. $1 id, $2 org, $3 site,
// $4 inspection, $5 item, $6 severity, $7 raiser and owner, $8 days from today (local).
const actionOnFindingSQL = `
	INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, severity, title, raised_by, client_raised_at,
	                                time_zone, owner_id, due_on)
	VALUES ($1, $2, $3, $4, $5, $6, 'Fix it', $7, now(), 'Africa/Johannesburg', $7,
	        (now() AT TIME ZONE 'Africa/Johannesburg')::date + $8::int)`

// The raise of an action inserted by actionOnFindingSQL: $1 event id, $2 org, $3 site,
// $4 action, $5 raiser and owner, $6 days from today (local).
const raiseOwnSQL = `
	INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, status, owner_id, due_on)
	VALUES ($1, $2, $3, $4, 1, 'raised', $5, now(), 'open', $5, (now() AT TIME ZONE 'Africa/Johannesburg')::date + $6::int)`

// Inserts a live deadline: $1 id, $2 org, $3 site, $4 duty, $5 incident, $6
// rule, $7 kind, $8 period, $9 unit, $10 basis_at, $11 computed_in_version.
// due_at and excluded_days are placeholders; the trigger computes them.
const deadlineSQL = `
	INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, regime_id, rule_key, rule_label, kind, period, unit,
	                       direction, basis, source_ref, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version)
	SELECT $1, $2, $3, $4, $5, id, $6, 'A rule', $7, $8, $9, 'after', 'incident_occurred_at', 'Test ref', $10,
	       'Africa/Johannesburg', 'ZA', '2000-01-01', '{}', $11
	FROM regulatory_regimes LIMIT 1`

// What an unfiltered DELETE as tenant A does to a table.
type deleteRule int

const (
	deleteOK     deleteRule = iota // removes A's row only
	deleteDenied                   // the app has no DELETE privilege (42501)
	deletePinned                   // A's row is referenced by A's evidence (23503)
	deleteFrozen                   // the app may delete, but A's row belongs to a completed inspection (23514)
)

type rlsTable struct {
	name, key    string // key identifies a row's org; refresh_tokens has no org_id
	wantA, wantB string
	update       string // a column the app may set to itself; "" means no UPDATE privilege
	del          deleteRule
}

// In foreign-key-safe delete order.
var rlsTables = []rlsTable{
	{"audit_export_downloads", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"audit_export_files", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"audit_export_evidence", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"audit_export_results", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"audit_export_attempts", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"audit_exports", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"toolbox_talk_history", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"member_history", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_history", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"organisation_history", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"worker_history", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"holiday_recomputations", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"notification_emails", "org_id", rlsOrgA, rlsOrgB, "status", deleteDenied},
	{"notifications", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_appointments", "org_id", rlsOrgA, rlsOrgB, "retracted_at", deleteDenied},
	{"document_versions", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"documents", "org_id", rlsOrgA, rlsOrgB, "latest_version", deleteDenied},
	{"site_roster", "org_id", rlsOrgA, rlsOrgB, "removed_at", deleteDenied},
	{"site_sign_in_sheets", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_sign_ins", "org_id", rlsOrgA, rlsOrgB, "retracted_at", deleteDenied},
	{"toolbox_attendance_workers", "org_id", rlsOrgA, rlsOrgB, "unlinked_at", deleteDenied},
	{"worker_medicals", "org_id", rlsOrgA, rlsOrgB, "retracted_at", deleteDenied},
	{"worker_competencies", "org_id", rlsOrgA, rlsOrgB, "retracted_at", deleteDenied},
	{"workers", "org_id", rlsOrgA, rlsOrgB, "trade", deleteDenied},
	{"file_access_log", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"files", "org_id", rlsOrgA, rlsOrgB, "status", deleteDenied},
	{"site_diary_addendum_evidence", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_diary_addenda", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_diary_revision_evidence", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_diary_revisions", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_diaries", "org_id", rlsOrgA, rlsOrgB, "late", deleteDenied},
	{"toolbox_attendance_retractions", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"toolbox_attendance", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"toolbox_talk_evidence", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"toolbox_talks", "org_id", rlsOrgA, rlsOrgB, "notes", deleteDenied},
	{"finding_waivers", "org_id", rlsOrgA, rlsOrgB, "retract_reason", deleteDenied},
	{"corrective_action_closeout_evidence", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"corrective_action_closeouts", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"corrective_action_events", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"corrective_actions", "org_id", rlsOrgA, rlsOrgB, "status", deleteDenied},
	{"deadline_satisfaction_retractions", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"deadline_satisfactions", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"deadlines", "org_id", rlsOrgA, rlsOrgB, "closed_as", deleteDenied},
	{"deadline_duties", "org_id", rlsOrgA, rlsOrgB, "satisfaction_id", deleteDenied},
	{"site_work_versions", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_work", "org_id", rlsOrgA, rlsOrgB, "latest_version", deleteDenied},
	{"witness_statements", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"incident_version_evidence", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"incident_people", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"incident_versions", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"incidents", "org_id", rlsOrgA, rlsOrgB, "latest_version", deleteDenied},
	{"inspection_findings", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"inspection_response_evidence", "org_id", rlsOrgA, rlsOrgB, "", deleteFrozen},
	{"inspection_responses", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"inspections", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"evidence_access_log", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"evidence_chain", "org_id", rlsOrgA, rlsOrgB, "", deleteDenied},
	{"site_chain_heads", "org_id", rlsOrgA, rlsOrgB, "org_id", deleteDenied},
	{"evidence", "org_id", rlsOrgA, rlsOrgB, "status", deleteDenied},
	{"refresh_tokens", "token_hash", "rls-refresh-a", "rls-refresh-b", "token_hash", deleteOK},
	{"sessions", "org_id", rlsOrgA, rlsOrgB, "org_id", deleteOK},
	{"invitations", "org_id", rlsOrgA, rlsOrgB, "org_id", deleteOK},
	{"site_locations", "org_id", rlsOrgA, rlsOrgB, "org_id", deleteOK},
	{"site_members", "org_id", rlsOrgA, rlsOrgB, "org_id", deleteOK},
	{"sites", "org_id", rlsOrgA, rlsOrgB, "org_id", deletePinned},
	{"org_members", "org_id", rlsOrgA, rlsOrgB, "org_id", deletePinned},
	{"organisations", "id", rlsOrgA, rlsOrgB, "id", deletePinned},
}

// rlsRows: tables with more than one seeded row per org. An action with a
// close-out has two events (its raise and the close-out); the worker has a
// certificate, a medical and an appointment letter, and the vault a letter:
// four files.
var rlsRows = map[string]int{"corrective_action_events": 2, "files": 4}

func rowsPerOrg(table string) int {
	if n, ok := rlsRows[table]; ok {
		return n
	}
	return 1
}

func hash32(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// inSavepoint runs one statement in a savepoint of tx, so an expected error
// leaves tx usable. keep=false rolls the statement back either way.
func inSavepoint(ctx context.Context, tx pgx.Tx, keep bool, sql string, args ...any) (int64, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return 0, err
	}
	tag, err := sp.Exec(ctx, sql, args...)
	if err != nil || !keep {
		_ = sp.Rollback(ctx)
		return tag.RowsAffected(), err
	}
	return tag.RowsAffected(), sp.Commit(ctx)
}

var seedOnce = sync.OnceValue(func() error {
	ctx := context.Background()
	tx, err := admin.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, x := range []string{"a", "b"} {
		id := func(prefix string) string { return prefix + "000000-0000-0000-0000-00000000000" + x }
		org, user, site, loc, sess, inv, ev, insp := id("20"), id("21"), id("22"), id("23"), id("24"), id("26"), id("28"), id("2a")
		inc, stmt, duty, dl, sat, act, ev1, co, wv, ev2 := id("2e"), id("2f"), id("30"), id("31"), id("32"), id("33"), id("34"), id("37"), id("38"), id("3a")
		talk, att := id("3c"), id("3d")
		rev, add := id("3e"), id("3f")

		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'RLS', 'construction', ARRAY['construction'])`, []any{org}},
			{`INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'RLS user')`, []any{user, "rls-" + x + "@rls.test"}},
			{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{org, user}},
			{`INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'RLS site' FROM regulatory_regimes LIMIT 1`, []any{site, org}},
			{`INSERT INTO site_members (org_id, site_id, user_id) VALUES ($1, $2, $3)`, []any{org, site, user}},
			{`INSERT INTO site_locations (id, org_id, site_id, kind, name) VALUES ($1, $2, $3, 'zone', $4)`, []any{loc, org, site, "Zone " + x}},
			{`INSERT INTO sessions (id, org_id, user_id, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, []any{sess, org, user}},
			{`INSERT INTO refresh_tokens (session_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, []any{sess, "rls-refresh-" + x}},
			{`INSERT INTO invitations (id, org_id, user_id, token_hash, invited_by, expires_at) VALUES ($1, $2, $3, $4, $3, now() + interval '1 day')`, []any{inv, org, user, "rls-invite-" + x}},
			{`INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at, object_key, object_version_id, retain_until, status)
			  VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', now(), $6, 'v1', now() + interval '1 day', 'verified')`, []any{ev, org, site, user, hash32(1), "rls/" + x}},
			{`INSERT INTO site_chain_heads (site_id, org_id, seq, head_hash) VALUES ($1, $2, 1, $3)`, []any{site, org, hash32(3)}},
			{`INSERT INTO evidence_chain (evidence_id, org_id, site_id, seq, content_sha256, server_received_at, prev_chain_hash, metadata_hash, chain_hash)
			  VALUES ($1, $2, $3, 1, $4, now(), $5, $6, $7)`, []any{ev, org, site, hash32(1), hash32(0), hash32(2), hash32(3)}},
			{`INSERT INTO evidence_access_log (org_id, site_id, evidence_id, user_id, action) VALUES ($1, $2, $3, $4, 'view_record')`, []any{org, site, ev, user}},
			// One completed inspection: completing it raises its one finding.
			{`INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
			                           template_title, template_source_ref, item_count, started_by, client_started_at)
			  SELECT $1, $2, $3, id, 'rls_check', 1, $5, 'RLS check', 'reg 1', 1, $4, now() FROM regulatory_regimes LIMIT 1`,
				[]any{insp, org, site, user, hash32(4)}},
			{`INSERT INTO inspection_responses (org_id, site_id, inspection_id, item_key, position, prompt, response_type,
			                                    result, severity, client_answered_at, answered_at)
			  VALUES ($1, $2, $3, 'item', 1, 'OK?', 'pass_fail_na', 'fail', 'medium', now(), now())`, []any{org, site, insp}},
			{`INSERT INTO inspection_response_evidence (org_id, site_id, inspection_id, item_key, evidence_id) VALUES ($1, $2, $3, 'item', $4)`,
				[]any{org, site, insp, ev}},
			{`UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = now() WHERE id = $1`, []any{insp}},
			// One incident, version 1, with its person, photo and a statement.
			{`INSERT INTO incidents (id, org_id, site_id, regime_id, reported_by) SELECT $1, $2, $3, id, $4 FROM regulatory_regimes LIMIT 1`,
				[]any{inc, org, site, user}},
			{`INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at, employer_aware_at,
			                                 submitted_by, client_submitted_at, people_count, evidence_count)
			  VALUES ($1, $2, $3, 1, ARRAY['loss_of_limb'], now() - interval '1 hour', now() - interval '1 hour', $4, now(), 1, 1)`,
				[]any{org, site, inc, user}},
			{`INSERT INTO incident_people (org_id, site_id, incident_id, version, position, employee_of_org, injured) VALUES ($1, $2, $3, 1, 1, true, true)`,
				[]any{org, site, inc}},
			{`INSERT INTO incident_version_evidence (org_id, site_id, incident_id, version, evidence_id) VALUES ($1, $2, $3, 1, $4)`,
				[]any{org, site, inc, ev}},
			{`INSERT INTO witness_statements (id, org_id, site_id, incident_id, witness_name, statement, taken_by, taken_at)
			  VALUES ($1, $2, $3, $4, 'RLS witness', 'Saw it.', $5, now())`, []any{stmt, org, site, inc, user}},
			// A duty with its live deadline, satisfied and then retracted.
			{`INSERT INTO deadline_duties (id, org_id, site_id, incident_id, rule_key) VALUES ($1, $2, $3, $4, 'ohsa_gar8_1b_immediate_notice')`,
				[]any{duty, org, site, inc}},
			{deadlineSQL, []any{dl, org, site, duty, inc, "ohsa_gar8_1b_immediate_notice", "report", 0, "immediately", "2026-11-10T10:00:00Z", 1}},
			{`INSERT INTO deadline_satisfactions (id, org_id, site_id, duty_id, deadline_id, satisfied_at, method, recorded_by)
			  VALUES ($1, $2, $3, $4, $5, '2026-11-10T10:05:00Z', 'phone', $6)`, []any{sat, org, site, duty, dl, user}},
			{`INSERT INTO deadline_satisfaction_retractions (satisfaction_id, org_id, site_id, duty_id, retracted_by, reason)
			  VALUES ($1, $2, $3, $4, $5, 'Recorded on the wrong incident')`, []any{sat, org, site, duty, user}},
			// An action on the inspection's finding, with its raise.
			{actionOnFindingSQL, []any{act, org, site, insp, "item", "medium", user, 1}},
			{raiseOwnSQL, []any{ev1, org, site, act, user, 1}},
			// Its close-out, awaiting verification. The seed has one photo per
			// org, the finding's own, which a real close-out can't use; the
			// check is off for this insert only (rolled into the seed's
			// transaction).
			{`ALTER TABLE corrective_action_closeout_evidence DISABLE TRIGGER corrective_action_closeout_evidence_insert`, nil},
			{`INSERT INTO corrective_action_closeouts (id, org_id, site_id, action_id, submitted_by, client_submitted_at, evidence_count)
			  VALUES ($1, $2, $3, $4, $5, now(), 1)`, []any{co, org, site, act, user}},
			{`INSERT INTO corrective_action_closeout_evidence (org_id, site_id, closeout_id, evidence_id) VALUES ($1, $2, $3, $4)`, []any{org, site, co, ev}},
			{`INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, status, owner_id, due_on, closeout_id)
			  VALUES ($1, $2, $3, $4, 2, 'closeout_submitted', $5, now(), 'awaiting_verification', $5,
			          (now() AT TIME ZONE 'Africa/Johannesburg')::date + 1, $6)`, []any{ev2, org, site, act, user, co}},
			// The trigger can't be switched back on while the close-out's
			// deferred checks are pending, so they run now (and pass).
			{`SET CONSTRAINTS ALL IMMEDIATE`, nil},
			{`SET CONSTRAINTS ALL DEFERRED`, nil},
			{`ALTER TABLE corrective_action_closeout_evidence ENABLE TRIGGER corrective_action_closeout_evidence_insert`, nil},
			// A waiver of the same finding.
			{`INSERT INTO finding_waivers (id, org_id, site_id, inspection_id, item_key, severity, reason, waived_by, client_waived_at)
			  VALUES ($1, $2, $3, $4, 'item', 'medium', 'Paperwork only', $5, now())`, []any{wv, org, site, insp, user}},
			// A toolbox talk with a photo and one signature, retracted.
			{talkSQL, []any{talk, org, site, "scaffolding", "Scaffolding", talkStart, nil, sast, user}},
			{talkPhotoSQL, []any{org, site, talk, ev, "photo", user}},
			{signSQL, []any{att, org, site, talk, "RLS worker", nil, "tap", user, signTime}},
			{unsignSQL, []any{att, org, site, "Signed by mistake", user}},
			// A diary written late, with a photo, and an addendum with a photo.
			{diarySQL, []any{org, site, rlsDiaryDate, user}},
			{revisionSQL, []any{rev, org, site, rlsDiaryDate, nil, 5, user, lateSaveAt, "Written up later", 1}},
			{revisionPhotoSQL, []any{org, site, rlsDiaryDate, rev, ev}},
			{addendumSQL, []any{add, org, site, rlsDiaryDate, "Forgot the plant", "Crane idle", user, 1}},
			{addendumPhotoSQL, []any{org, site, rlsDiaryDate, add, ev}},
			// M11: a verified certificate, viewed once.
			{fileSQL, []any{id("40"), org, nil, "competency", user, hash32(6), "verified"}},
			{`INSERT INTO file_access_log (org_id, file_id, user_id, action, object_version_id) VALUES ($1, $2, $3, 'view_bytes', 'v1')`,
				[]any{org, id("40"), user}},
			// The worker who signed the talk, with that certificate, a medical
			// and the signature linked to them.
			{workerSQL, []any{id("41"), org, "RLS worker", nil, nil, user}},
			{compSQL, []any{id("42"), org, id("41"), "scaffold_erector", id("40"), nil, user}},
			{fileSQL, []any{id("44"), org, nil, "medical", user, hash32(8), "verified"}},
			{medSQL, []any{id("43"), org, id("41"), "fit", id("44"), nil, user}},
			{`INSERT INTO toolbox_attendance_workers (org_id, site_id, attendance_id, worker_id, method, linked_by) VALUES ($1, $2, $3, $4, 'auto', $5)`,
				[]any{org, site, att, id("41"), user}},
			// The worker signs in on paper (a sheet photo first), which puts
			// them on the site's roster.
			{`INSERT INTO site_sign_in_sheets (org_id, site_id, sheet_date, evidence_id, added_by) VALUES ($1, $2, '2026-01-15', $3, $4)`,
				[]any{org, site, ev, user}},
			{signInSQL, []any{id("45"), org, site, id("41"), "paper", user, time.Date(2026, 1, 15, 5, 0, 0, 0, time.UTC)}},
			// A Letter of Good Standing in the vault.
			{fileSQL, []any{id("47"), org, nil, "org_document", user, hash32(9), "verified"}},
			{docSQL, []any{id("46"), org, nil, "org", "coida_letter_of_good_standing", user}},
			{docVersionSQL, []any{org, nil, id("46"), 1, "org", id("47"), user}},
			// The worker appointed scaffolding supervisor on the site.
			{fileSQL, []any{id("49"), org, site, "appointment", user, hash32(10), "verified"}},
			{appointmentSQL, []any{id("48"), org, site, id("41"), "scaffolding_supervisor", id("49"), nil, user}},
			// A reminder of the action, its email still pending, and a holiday
			// already recomputed.
			{`INSERT INTO notifications (id, org_id, user_id, kind, subject_type, subject_id, threshold, title)
			  VALUES ($1, $2, $3, 'action_overdue', 'action', $4, 'escalation_1', 'Fix it')`, []any{id("4a"), org, user, act}},
			{`INSERT INTO notification_emails (notification_id, org_id, to_address) VALUES ($1, $2, 'rls@rls.test')`, []any{id("4a"), org}},
			{`INSERT INTO holiday_recomputations (org_id, jurisdiction, holiday_date, recomputed) VALUES ($1, 'ZA', '2026-12-25', 0)`, []any{org}},
			// An audit export, built (its attempt, result, contents) and downloaded
			// once. The settle window is waived for the seed only.
			{`SELECT set_config('siteproof.export_settle', '0 seconds', true)`, nil},
			{exportSQL, []any{id("4b"), org, site, user}},
			{`INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
			  VALUES ($1, $2, $3, 1, 'rls', now(), clock_timestamp() + interval '1 hour')`, []any{id("4c"), org, id("4b")}},
			{exportResultSQL, []any{id("4b"), org, id("4c")}},
			{`INSERT INTO audit_export_evidence (org_id, export_id, site_id, evidence_id) VALUES ($1, $2, $3, $4)`, []any{org, id("4b"), site, ev}},
			{`INSERT INTO audit_export_files (org_id, export_id, file_id) VALUES ($1, $2, $3)`, []any{org, id("4b"), id("40")}},
			{`INSERT INTO audit_export_downloads (org_id, export_id, user_id) VALUES ($1, $2, $3)`, []any{org, id("4b"), user}},
			// REG4: planned excavation.
			{siteWorkSQL, []any{id("50"), org, site, user}},
			{workVersionSQL, []any{org, site, id("50"), 1, "2026-12-01", []string{"excavation"}, "planned", user, "0 minutes", nil}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return fmt.Errorf("seed %q: %w", s.sql, err)
			}
		}
	}
	return tx.Commit(ctx)
})

func seedRLS(t *testing.T) {
	t.Helper()
	if err := seedOnce(); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestRLSReads(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()

	for _, tb := range rlsTables {
		t.Run(tb.name, func(t *testing.T) {
			var keys []string
			err := database.WithTenant(ctx, pool, database.Tenant{OrgID: rlsOrgA}, func(tx pgx.Tx) error {
				rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT %s::text FROM %s`, tb.key, tb.name))
				if err != nil {
					return err
				}
				keys, err = pgx.CollectRows(rows, pgx.RowTo[string])
				return err
			})
			if err != nil {
				t.Fatalf("select as A: %v", err)
			}
			if want := slices.Repeat([]string{tb.wantA}, rowsPerOrg(tb.name)); !slices.Equal(keys, want) {
				t.Errorf("unfiltered SELECT as A returned %v, want only %v", keys, want)
			}

			var n int
			err = database.WithTenant(ctx, pool, database.Tenant{}, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, tb.name)).Scan(&n)
			})
			if err != nil {
				t.Fatalf("count with no tenant: %v", err)
			}
			if n != 0 {
				t.Errorf("no tenant sees %d rows, want 0", n)
			}
		})
	}
}

func TestRLSWrites(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tenantA := database.Tenant{OrgID: rlsOrgA}

	t.Run("insert into B fails the policy", func(t *testing.T) {
		inserts := map[string]struct {
			sql  string
			args []any
		}{
			"organisations":  {`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'X', 'construction', ARRAY['construction'])`, []any{"27000000-0000-0000-0000-00000000000b"}},
			"org_members":    {`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{rlsOrgB, rlsUserA}},
			"sites":          {`INSERT INTO sites (org_id, regime_id, name) SELECT $1, id, 'X' FROM regulatory_regimes LIMIT 1`, []any{rlsOrgB}},
			"site_members":   {`INSERT INTO site_members (org_id, site_id, user_id) VALUES ($1, $2, $3)`, []any{rlsOrgB, rlsSiteB, rlsUserA}},
			"site_locations": {`INSERT INTO site_locations (org_id, site_id, kind, name) VALUES ($1, $2, 'zone', 'Smuggled')`, []any{rlsOrgB, rlsSiteB}},
			"sessions":       {`INSERT INTO sessions (org_id, user_id, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, []any{rlsOrgB, rlsUserB}},
			"invitations":    {`INSERT INTO invitations (org_id, user_id, token_hash, invited_by, expires_at) VALUES ($1, $2, 'rls-smuggled', $2, now() + interval '1 day')`, []any{rlsOrgB, rlsUserB}},
			"refresh_tokens": {`INSERT INTO refresh_tokens (session_id, token_hash, expires_at) VALUES ($1, 'rls-smuggled', now() + interval '1 day')`, []any{rlsSessB}},
			"evidence": {`INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at, object_key)
			  VALUES ('29000000-0000-0000-0000-00000000000b', $1, $2, $3, $4, 1, 'image/jpeg', now(), 'rls-smuggled')`, []any{rlsOrgB, rlsSiteB, rlsUserB, hash32(9)}},
			// These collide with B's seeded rows too; the policy check must come first.
			"site_chain_heads": {`INSERT INTO site_chain_heads (site_id, org_id) VALUES ($1, $2)`, []any{rlsSiteB, rlsOrgB}},
			"evidence_chain": {`INSERT INTO evidence_chain (evidence_id, org_id, site_id, seq, content_sha256, server_received_at, prev_chain_hash, metadata_hash, chain_hash)
			  VALUES ($1, $2, $3, 2, $4, now(), $5, $6, $7)`, []any{rlsEvidenceB, rlsOrgB, rlsSiteB, hash32(1), hash32(3), hash32(4), hash32(5)}},
			"evidence_access_log": {`INSERT INTO evidence_access_log (org_id, site_id, evidence_id, user_id, action) VALUES ($1, $2, $3, $4, 'view_record')`,
				[]any{rlsOrgB, rlsSiteB, rlsEvidenceB, rlsUserB}},
			"inspections": {`INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
			                                          template_title, template_source_ref, item_count, started_by, client_started_at)
			  SELECT '2c000000-0000-0000-0000-00000000000b', $1, $2, id, 'k', 1, $4, 't', 'r', 1, $3, now() FROM regulatory_regimes LIMIT 1`,
				[]any{rlsOrgB, rlsSiteB, rlsUserB, hash32(4)}},
			"inspection_responses": {`INSERT INTO inspection_responses (org_id, site_id, inspection_id, item_key, position, prompt, response_type)
			  VALUES ($1, $2, $3, 'smuggled', 2, 'P?', 'pass_fail_na')`, []any{rlsOrgB, rlsSiteB, rlsInspB}},
			"inspection_response_evidence": {`INSERT INTO inspection_response_evidence (org_id, site_id, inspection_id, item_key, evidence_id)
			  VALUES ($1, $2, $3, 'item', $4)`, []any{rlsOrgB, rlsSiteB, rlsInspB, rlsEvidenceB}},
			"incidents": {`INSERT INTO incidents (id, org_id, site_id, regime_id, reported_by)
			  SELECT '2e000000-0000-0000-0000-0000000000bb', $1, $2, id, $3 FROM regulatory_regimes LIMIT 1`, []any{rlsOrgB, rlsSiteB, rlsUserB}},
			"incident_versions": {`INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at,
			                                                      submitted_by, client_submitted_at, amend_reason, people_count, evidence_count)
			  VALUES ($1, $2, $3, 2, ARRAY['near_miss'], now(), $4, now(), 'Smuggled', 0, 0)`, []any{rlsOrgB, rlsSiteB, rlsIncB, rlsUserB}},
			"incident_people": {`INSERT INTO incident_people (org_id, site_id, incident_id, version, position, employee_of_org, injured)
			  VALUES ($1, $2, $3, 1, 2, false, false)`, []any{rlsOrgB, rlsSiteB, rlsIncB}},
			"incident_version_evidence": {`INSERT INTO incident_version_evidence (org_id, site_id, incident_id, version, evidence_id)
			  VALUES ($1, $2, $3, 1, $4)`, []any{rlsOrgB, rlsSiteB, rlsIncB, rlsEvidenceA}},
			"witness_statements": {`INSERT INTO witness_statements (id, org_id, site_id, incident_id, witness_name, statement, taken_by, taken_at)
			  VALUES ('2f000000-0000-0000-0000-0000000000bb', $1, $2, $3, 'W', 'Smuggled.', $4, now())`, []any{rlsOrgB, rlsSiteB, rlsIncB, rlsUserB}},
			"deadline_duties": {`INSERT INTO deadline_duties (org_id, site_id, incident_id, rule_key) VALUES ($1, $2, $3, 'smuggled')`,
				[]any{rlsOrgB, rlsSiteB, rlsIncB}},
			"deadlines": {deadlineSQL, []any{"31000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, rlsDutyB, rlsIncB,
				"ohsa_gar8_1b_immediate_notice", "report", 0, "immediately", "2026-11-10T10:00:00Z", 1}},
			"deadline_satisfactions": {`INSERT INTO deadline_satisfactions (id, org_id, site_id, duty_id, deadline_id, satisfied_at, method, recorded_by)
			  VALUES ('32000000-0000-0000-0000-0000000000bb', $1, $2, $3, $4, '2026-11-10T10:05:00Z', 'phone', $5)`,
				[]any{rlsOrgB, rlsSiteB, rlsDutyB, rlsDeadB, rlsUserB}},
			"deadline_satisfaction_retractions": {`INSERT INTO deadline_satisfaction_retractions (satisfaction_id, org_id, site_id, duty_id, retracted_by, reason)
			  VALUES ('32000000-0000-0000-0000-00000000000b', $1, $2, $3, $4, 'Smuggled')`, []any{rlsOrgB, rlsSiteB, rlsDutyB, rlsUserB}},
			"inspection_findings": {`INSERT INTO inspection_findings (org_id, site_id, inspection_id, item_key, severity)
			  VALUES ($1, $2, $3, 'item', 'high')`, []any{rlsOrgB, rlsSiteB, rlsInspB}},
			"corrective_action_closeouts": {`INSERT INTO corrective_action_closeouts (id, org_id, site_id, action_id, submitted_by, client_submitted_at, evidence_count)
			  VALUES ('37000000-0000-0000-0000-0000000000bb', $1, $2, $3, $4, now(), 1)`, []any{rlsOrgB, rlsSiteB, rlsActionB, rlsUserB}},
			"corrective_action_closeout_evidence": {`INSERT INTO corrective_action_closeout_evidence (org_id, site_id, closeout_id, evidence_id)
			  VALUES ($1, $2, '37000000-0000-0000-0000-00000000000b', $3)`, []any{rlsOrgB, rlsSiteB, rlsEvidenceB}},
			"finding_waivers": {`INSERT INTO finding_waivers (id, org_id, site_id, inspection_id, item_key, severity, reason, waived_by, client_waived_at)
			  VALUES ('38000000-0000-0000-0000-0000000000bb', $1, $2, $3, 'item', 'medium', 'Smuggled', $4, now())`, []any{rlsOrgB, rlsSiteB, rlsInspB, rlsUserB}},
			"corrective_actions": {actionOnFindingSQL, []any{"33000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, rlsInspB, "item", "high", rlsUserB, 1}},
			"site_diaries": {`INSERT INTO site_diaries (org_id, site_id, diary_date, time_zone, created_by) VALUES ($1, $2, '2026-01-16', 'Africa/Johannesburg', $3)`,
				[]any{rlsOrgB, rlsSiteB, rlsUserB}},
			"site_diary_revisions":           {revisionSQL, []any{"3e000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, rlsDiaryDate, nil, 1, rlsUserB, lateSaveAt, "Smuggled", 0}},
			"site_diary_revision_evidence":   {revisionPhotoSQL, []any{rlsOrgB, rlsSiteB, rlsDiaryDate, rlsRevB, rlsEvidenceB}},
			"site_diary_addenda":             {addendumSQL, []any{"3f000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, rlsDiaryDate, "Smuggled", "x", rlsUserB, 0}},
			"site_diary_addendum_evidence":   {addendumPhotoSQL, []any{rlsOrgB, rlsSiteB, rlsDiaryDate, rlsAddB, rlsEvidenceB}},
			"toolbox_talks":                  {talkSQL, []any{"3c000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, nil, "Smuggled", talkStart, nil, sast, rlsUserB}},
			"toolbox_talk_evidence":          {talkPhotoSQL, []any{rlsOrgB, rlsSiteB, rlsTalkB, rlsEvidenceB, "photo", rlsUserB}},
			"toolbox_attendance":             {signSQL, []any{"3d000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, rlsTalkB, "Smuggled", nil, "tap", rlsUserB, signTime}},
			"toolbox_attendance_retractions": {unsignSQL, []any{rlsAttB, rlsOrgB, rlsSiteB, "Smuggled", rlsUserB}},
			"files":                          {fileSQL, []any{"40000000-0000-0000-0000-0000000000bb", rlsOrgB, nil, "competency", rlsUserB, hash32(7), "pending"}},
			"file_access_log": {`INSERT INTO file_access_log (org_id, file_id, user_id, action, object_version_id) VALUES ($1, $2, $3, 'view_bytes', 'v1')`,
				[]any{rlsOrgB, rlsFileB, rlsUserB}},
			"notifications": {`INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, title)
			  VALUES ($1, $2, 'action_overdue', 'action', $3, 'escalation_2', 'Smuggled')`, []any{rlsOrgB, rlsUserB, rlsActionB}},
			"notification_emails": {`INSERT INTO notification_emails (notification_id, org_id, to_address) VALUES ($1, $2, 'x@x.test')`,
				[]any{"4a000000-0000-0000-0000-00000000000b", rlsOrgB}},
			"holiday_recomputations": {`INSERT INTO holiday_recomputations (org_id, jurisdiction, holiday_date, recomputed) VALUES ($1, 'ZA', '2026-12-26', 0)`,
				[]any{rlsOrgB}},
			"worker_history": {`INSERT INTO worker_history (org_id, worker_id, name, active, changed_at, changed_by) VALUES ($1, $2, 'Smuggled', true, now(), $3)`,
				[]any{rlsOrgB, "41000000-0000-0000-0000-00000000000b", rlsUserB}},
			"site_appointments": {appointmentSQL, []any{"48000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, "41000000-0000-0000-0000-00000000000b",
				"risk_assessor", "49000000-0000-0000-0000-00000000000b", nil, rlsUserB}},
			"documents":         {docSQL, []any{"46000000-0000-0000-0000-0000000000bb", rlsOrgB, nil, "org", "cidb_registration", rlsUserB}},
			"document_versions": {docVersionSQL, []any{rlsOrgB, nil, "46000000-0000-0000-0000-00000000000b", 2, "org", "47000000-0000-0000-0000-00000000000b", rlsUserB}},
			"site_sign_ins":     {signInSQL, []any{"45000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, "41000000-0000-0000-0000-00000000000b", "tap", rlsUserB, localNov10(15, 0)}},
			"site_sign_in_sheets": {`INSERT INTO site_sign_in_sheets (org_id, site_id, sheet_date, evidence_id, added_by) VALUES ($1, $2, '2026-11-11', $3, $4)`,
				[]any{rlsOrgB, rlsSiteB, rlsEvidenceB, rlsUserB}},
			"site_roster": {`INSERT INTO site_roster (org_id, site_id, worker_id, added_via, added_by) VALUES ($1, $2, $3, 'manual', $4)`,
				[]any{rlsOrgB, rlsSiteB, "41000000-0000-0000-0000-00000000000b", rlsUserB}},
			"workers":             {workerSQL, []any{"41000000-0000-0000-0000-0000000000bb", rlsOrgB, "Smuggled", nil, nil, rlsUserB}},
			"worker_competencies": {compSQL, []any{"42000000-0000-0000-0000-0000000000bb", rlsOrgB, "41000000-0000-0000-0000-00000000000b", "k", rlsFileB, nil, rlsUserB}},
			"worker_medicals":     {medSQL, []any{"43000000-0000-0000-0000-0000000000bb", rlsOrgB, "41000000-0000-0000-0000-00000000000b", "fit", "44000000-0000-0000-0000-00000000000b", nil, rlsUserB}},
			"toolbox_attendance_workers": {`INSERT INTO toolbox_attendance_workers (org_id, site_id, attendance_id, worker_id, method, linked_by) VALUES ($1, $2, $3, $4, 'manual', $5)`,
				[]any{rlsOrgB, rlsSiteB, rlsAttB, "41000000-0000-0000-0000-00000000000b", rlsUserB}},
			"audit_exports": {exportSQL, []any{"4b000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, rlsUserB}},
			"audit_export_attempts": {`INSERT INTO audit_export_attempts (id, org_id, export_id, attempt, instance, claimed_at, lease_until)
			  VALUES (gen_random_uuid(), $1, $2, 1, 'x', now(), now() + interval '1 hour')`, []any{rlsOrgB, "4b000000-0000-0000-0000-00000000000b"}},
			"audit_export_results": {exportResultSQL, []any{"4b000000-0000-0000-0000-00000000000b", rlsOrgB, "4c000000-0000-0000-0000-00000000000b"}},
			"audit_export_evidence": {`INSERT INTO audit_export_evidence (org_id, export_id, site_id, evidence_id) VALUES ($1, $2, $3, $4)`,
				[]any{rlsOrgB, "4b000000-0000-0000-0000-00000000000b", rlsSiteB, rlsEvidenceB}},
			"audit_export_files": {`INSERT INTO audit_export_files (org_id, export_id, file_id) VALUES ($1, $2, $3)`,
				[]any{rlsOrgB, "4b000000-0000-0000-0000-00000000000b", rlsFileB}},
			"audit_export_downloads": {`INSERT INTO audit_export_downloads (org_id, export_id, user_id) VALUES ($1, $2, $3)`,
				[]any{rlsOrgB, "4b000000-0000-0000-0000-00000000000b", rlsUserB}},
			// History is written only by triggers: the app has no INSERT at all.
			"organisation_history": {`INSERT INTO organisation_history (org_id, trading_name, changed_at) VALUES ($1, 'Smuggled', now())`, []any{rlsOrgB}},
			"site_history":         {`INSERT INTO site_history (org_id, site_id, regime_id, name, status, changed_at) SELECT $1, $2, id, 'Smuggled', 'active', now() FROM regulatory_regimes LIMIT 1`, []any{rlsOrgB, rlsSiteB}},
			"member_history":       {`INSERT INTO member_history (org_id, user_id, role, status, full_name, changed_at) VALUES ($1, $2, 'owner', 'active', 'Smuggled', now())`, []any{rlsOrgB, rlsUserB}},
			"toolbox_talk_history": {`INSERT INTO toolbox_talk_history (org_id, site_id, talk_id, topic, presenter_name, started_at, changed_at) VALUES ($1, $2, $3, 'Smuggled', 'P', now(), now())`, []any{rlsOrgB, rlsSiteB, rlsTalkB}},
			"site_work":            {siteWorkSQL, []any{"50000000-0000-0000-0000-0000000000bb", rlsOrgB, rlsSiteB, rlsUserB}},
			"site_work_versions":   {workVersionSQL, []any{rlsOrgB, rlsSiteB, rlsWorkB, 2, "2026-12-08", []string{}, "cancelled", rlsUserB, "0 minutes", "Smuggled"}},
			"corrective_action_events": {`INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, reason, status, owner_id, due_on)
			  VALUES ('34000000-0000-0000-0000-0000000000bb', $1, $2, $3, 2, 'cancelled', $4, now(), 'Smuggled', 'cancelled', $4, action_local_date(now(), 'Africa/Johannesburg') + 1)`,
				[]any{rlsOrgB, rlsSiteB, rlsActionB, rlsUserB}},
		}
		for _, tb := range rlsTables {
			t.Run(tb.name, func(t *testing.T) {
				in := inserts[tb.name]
				err := database.WithTenant(ctx, pool, tenantA, func(tx pgx.Tx) error {
					_, err := tx.Exec(ctx, in.sql, in.args...)
					return err
				})
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Fatalf("got %v, want SQLSTATE 42501", err)
				}
			})
		}
	})

	// Rolled back so the fixtures survive. RowsAffected counts every row
	// changed, so 1 means only A's row: B's was never reached.
	t.Run("unfiltered update and delete reach only A", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := database.SetTenant(ctx, tx, tenantA); err != nil {
			t.Fatalf("set tenant: %v", err)
		}

		for _, tb := range rlsTables {
			col := tb.update
			if col == "" {
				col = tb.key
			}
			n, err := inSavepoint(ctx, tx, true, fmt.Sprintf(`UPDATE %s SET %s = %s`, tb.name, col, col))
			switch {
			case tb.update == "":
				if pgCode(err) != "42501" {
					t.Errorf("unfiltered UPDATE %s: got %v, want SQLSTATE 42501", tb.name, err)
				}
			case err != nil:
				t.Errorf("unfiltered UPDATE %s: %v", tb.name, err)
			case n != int64(rowsPerOrg(tb.name)):
				t.Errorf("unfiltered UPDATE %s changed %d rows, want %d", tb.name, n, rowsPerOrg(tb.name))
			}
		}

		for _, tb := range rlsTables {
			n, err := inSavepoint(ctx, tx, true, fmt.Sprintf(`DELETE FROM %s`, tb.name))
			switch tb.del {
			case deleteOK:
				if err != nil || n != 1 {
					t.Errorf("unfiltered DELETE %s: removed %d rows, err %v; want 1", tb.name, n, err)
				}
			case deleteDenied:
				if pgCode(err) != "42501" {
					t.Errorf("unfiltered DELETE %s: got %v, want SQLSTATE 42501", tb.name, err)
				}
			case deleteFrozen:
				if pgCode(err) != "23514" {
					t.Errorf("unfiltered DELETE %s: got %v, want SQLSTATE 23514 (A's row belongs to a completed inspection)", tb.name, err)
				}
			case deletePinned:
				if pgCode(err) != "23503" {
					t.Errorf("unfiltered DELETE %s: got %v, want SQLSTATE 23503 (A's row is pinned by A's evidence)", tb.name, err)
				}
				q := fmt.Sprintf(`DELETE FROM %s WHERE %s::text = $1`, tb.name, tb.key)
				if n, err := inSavepoint(ctx, tx, true, q, tb.wantB); err != nil || n != 0 {
					t.Errorf("DELETE %s of B's row: removed %d rows, err %v; want 0", tb.name, n, err)
				}
			}
		}
	})

	t.Run("B's rows are intact", func(t *testing.T) {
		for _, tb := range rlsTables {
			var n int
			q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s::text = $1`, tb.name, tb.key)
			if err := admin.QueryRow(ctx, q, tb.wantB).Scan(&n); err != nil {
				t.Fatalf("count %s: %v", tb.name, err)
			}
			if n != rowsPerOrg(tb.name) {
				t.Errorf("%s: B has %d rows, want %d", tb.name, n, rowsPerOrg(tb.name))
			}
		}
	})
}

// Tables that are deliberately outside RLS.
var rlsAllowlist = []string{
	"users",                     // no org_id; accepted risk, see BUILD_PLAN DEPLOY
	"regulatory_regimes",        // public system data, read-only to siteproof_app
	"public_holidays",           // the holiday calendar (000015): public data, read-only to siteproof_app
	"holiday_calendar_coverage", // the calendar's date range (000015): same
	"job_runs",                  // worker job runs (000029): system data, no org_id; written by cmd/worker
	"sync_generation",           // the sync epoch's generation (000049): one row, no app privileges
	"schema_migrations",         // golang-migrate's bookkeeping (absent in tests)
}

type tableRLS struct {
	name                    string
	enabled, forced, orgCol bool
	policies                int
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadRLSCatalog(t *testing.T, db querier) []tableRLS {
	t.Helper()
	rows, err := db.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_attribute a
		               WHERE a.attrelid = c.oid AND a.attname = 'org_id' AND a.attnum > 0 AND NOT a.attisdropped),
		       (SELECT count(*)::int FROM pg_policy p WHERE p.polrelid = c.oid)
		FROM pg_class c
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
		ORDER BY c.relname
	`)
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	tables, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (tableRLS, error) {
		var x tableRLS
		err := r.Scan(&x.name, &x.enabled, &x.forced, &x.orgCol, &x.policies)
		return x, err
	})
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	return tables
}

// coverageProblems: every table with org_id, and every public table not on the
// allowlist, must have RLS enabled and forced with at least one policy.
func coverageProblems(tables []tableRLS) []string {
	var out []string
	for _, x := range tables {
		allowed := slices.Contains(rlsAllowlist, x.name)
		if allowed && !x.orgCol {
			continue
		}
		if allowed {
			out = append(out, x.name+": has org_id but is on the RLS allowlist")
		}
		if !x.enabled {
			out = append(out, x.name+": RLS not enabled")
		}
		if !x.forced {
			out = append(out, x.name+": RLS not forced")
		}
		if x.policies == 0 {
			out = append(out, x.name+": no policy")
		}
	}
	return out
}

func TestRLSCoverage(t *testing.T) {
	ctx := context.Background()
	tables := loadRLSCatalog(t, pool)

	var names, orgTables []string
	for _, x := range tables {
		names = append(names, x.name)
		if x.orgCol {
			orgTables = append(orgTables, x.name)
		}
	}
	if !slices.Contains(orgTables, "sites") {
		t.Fatalf("catalog query found no org_id on sites (got %v); it would pass vacuously", orgTables)
	}
	for _, tb := range rlsTables {
		if !slices.Contains(names, tb.name) {
			t.Fatalf("catalog query is missing %s (got %v)", tb.name, names)
		}
	}

	for _, p := range coverageProblems(tables) {
		t.Error(p)
	}

	// DDL is transactional, so these probes are rolled back and never seen by other tests.
	probe := func(t *testing.T, ddl, want string) {
		t.Helper()
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, ddl); err != nil {
			t.Fatalf("probe ddl: %v", err)
		}
		if got := coverageProblems(loadRLSCatalog(t, tx)); !slices.Contains(got, want) {
			t.Errorf("coverage problems %q do not include %q", got, want)
		}
	}
	t.Run("catches a new table with org_id and no RLS", func(t *testing.T) {
		probe(t, `CREATE TABLE rls_probe (org_id uuid)`, "rls_probe: RLS not enabled")
	})
	t.Run("catches a new table without org_id and not allowlisted", func(t *testing.T) {
		probe(t, `CREATE TABLE rls_probe_indirect (session_id uuid)`, "rls_probe_indirect: no policy")
	})
	t.Run("catches RLS switched off on a tenant table", func(t *testing.T) {
		probe(t, `ALTER TABLE sites NO FORCE ROW LEVEL SECURITY`, "sites: RLS not forced")
	})
}

func TestTokenLookups(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM refresh_token_tenant('unknown')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("refresh_token_tenant(unknown): %d rows, err %v; want 0", n, err)
	}
	var org *string
	if err := pool.QueryRow(ctx, `SELECT invitation_org('unknown')::text`).Scan(&org); err != nil || org != nil {
		t.Errorf("invitation_org(unknown) = %v, err %v; want NULL", org, err)
	}

	var gotOrg, gotUser string
	if err := pool.QueryRow(ctx, `SELECT org_id::text, user_id::text FROM refresh_token_tenant('rls-refresh-b')`).Scan(&gotOrg, &gotUser); err != nil {
		t.Fatalf("refresh_token_tenant(b): %v", err)
	}
	if gotOrg != rlsOrgB || gotUser != rlsUserB {
		t.Errorf("refresh_token_tenant(b) = (%s, %s), want (%s, %s)", gotOrg, gotUser, rlsOrgB, rlsUserB)
	}
	if err := pool.QueryRow(ctx, `SELECT invitation_org('rls-invite-b')::text`).Scan(&gotOrg); err != nil || gotOrg != rlsOrgB {
		t.Errorf("invitation_org(b) = %s, err %v; want %s", gotOrg, err, rlsOrgB)
	}

	// No tenant set and no WithTenant: the app role sees none of the rows the lookups found.
	for _, table := range []string{"sessions", "invitations"} {
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, table)).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s with no tenant: %d rows, err %v; want 0", table, n, err)
		}
	}
}

// An export of a site by its owner: $1 id, $2 org, $3 site, $4 requester.
const exportSQL = `
	INSERT INTO audit_exports (id, org_id, site_id, requested_by, requester_role, from_date, to_date, time_zone, as_of, pack_format,
	                           regime_code, regime_version, regime_data_sha256, includes_health, client_requested_at)
	VALUES ($1, $2, $3, $4, 'owner', '2026-01-01', '2026-01-31', 'Africa/Johannesburg', now(), 1,
	        'za_construction_2014', '2014', decode(repeat('0b', 32), 'hex'), true, now())`

// The result of export $1 (org $2) by attempt $3: one photo, one file.
const exportResultSQL = `
	INSERT INTO audit_export_results (export_id, org_id, attempt_id, sha256, size_bytes, object_key, object_version_id,
	                                  evidence_count, file_count, chain_length, chain_head, chain_intact)
	VALUES ($1, $2, $3, decode(repeat('0c', 32), 'hex'), 1, 'rls/export.zip', 'v1', 1, 1, 1, decode(repeat('0d', 32), 'hex'), true)`
