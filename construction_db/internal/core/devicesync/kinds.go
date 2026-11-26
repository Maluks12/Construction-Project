package devicesync

import (
	"fmt"
	"net/url"
	"strings"
)

type Scope string

const (
	ScopeSite Scope = "site"
	ScopeOrg  Scope = "org"
)

// Kind is one sort of pulled aggregate. It is rendered by its GET (Pattern;
// Path builds the concrete URL) and re-sent whenever a row its GET reads
// changes in the window: each Dep selects the roots a changed row belongs to.
// AsOf names the tables its GET reads that are not deps, each with why: the
// fields they feed are as of server_time (TestSyncDepsCoverWhatGETsRead).
type Kind struct {
	Name    string
	Scope   Scope
	Leads   bool // only owners and SHE officers pull it, as only they may GET it
	Pattern string
	Path    func(scopeID, root string) string
	Deps    []Dep
	AsOf    map[string]string
}

// Dep watches one stamped table. SQL returns one text column, the root; its
// parameters are $1 the scope (site or organisation id), $2 and $3 the
// window (text, cast to xid8), $4 the caller's user id. W writes the window.
type Dep struct {
	Table string
	SQL   string
}

// W is the window on alias's sync_xid ("" for an unqualified column).
func W(alias string) string {
	col := "sync_xid"
	if alias != "" {
		col = alias + ".sync_xid"
	}
	return col + " >= $2::text::xid8 AND " + col + " < $3::text::xid8"
}

// Tables are every stamped table some kind watches.
func Tables(kinds []Kind) map[string]bool {
	out := map[string]bool{}
	for _, k := range kinds {
		for _, d := range k.Deps {
			out[d.Table] = true
		}
	}
	return out
}

// query selects up to $6 roots after $5 that changed in the window.
func (k Kind) query() string {
	parts := make([]string, len(k.Deps))
	for i, d := range k.Deps {
		parts[i] = "(" + d.SQL + ")"
	}
	// p types $1 and $4 for the kinds whose deps don't both use them.
	return `SELECT root FROM (` + strings.Join(parts, " UNION ") + `) u (root), (SELECT $1::uuid, $4::uuid) p
		WHERE root COLLATE "C" > $5 COLLATE "C" ORDER BY root COLLATE "C" LIMIT $6`
}

func sitePath(format string) func(string, string) string {
	return func(site, root string) string { return fmt.Sprintf(format, site, url.PathEscape(root)) }
}

// ByRoot is a site-scoped dep returning column root of table.
func ByRoot(table, root string) Dep {
	return Dep{table, fmt.Sprintf(`SELECT %s::text FROM %s WHERE site_id = $1 AND %s AND %s IS NOT NULL`, root, table, W(""), root)}
}

// ViaDuty maps a deadline-duty table's rows to the duty's subject column.
func ViaDuty(table, subject string) Dep {
	return Dep{table, fmt.Sprintf(`SELECT du.%s::text FROM %s x JOIN deadline_duties du ON du.id = x.duty_id
		WHERE x.site_id = $1 AND %s AND du.%s IS NOT NULL`, subject, table, W("x"), subject)}
}

// The core kinds, in pull order. The edition adds its own (Edition.SyncSources).
func CoreKinds() []Kind {
	return []Kind{
		{
			Name: "location_list", Scope: ScopeSite, Pattern: "/v1/sites/:id/locations",
			Path: func(site, _ string) string { return "/v1/sites/" + site + "/locations" },
			Deps: []Dep{{"site_locations", `SELECT site_id::text FROM site_locations WHERE site_id = $1 AND ` + W("")}},
		},
		{
			Name: "inspection", Scope: ScopeSite, Pattern: "/v1/sites/:id/inspections/:inspectionId",
			Path: sitePath("/v1/sites/%s/inspections/%s"),
			Deps: []Dep{
				ByRoot("inspections", "id"),
				ByRoot("inspection_responses", "inspection_id"),
				ByRoot("inspection_response_evidence", "inspection_id"),
				ByRoot("inspection_findings", "inspection_id"),
				{"sync_deletions", `SELECT root FROM sync_deletions WHERE site_id = $1 AND kind = 'inspection' AND ` + W("")},
			},
			AsOf: map[string]string{
				"evidence": "an attached photo's capture time: set when it is declared and never changed; only verified photos attach",
			},
		},
		{
			// An inspection's findings with their coverage (finding_coverage,
			// 000019): actions and waivers anywhere in its correction chain,
			// and a completed correction, change them.
			Name: "finding_list", Scope: ScopeSite, Pattern: "/v1/sites/:id/findings",
			Path: func(site, root string) string {
				return "/v1/sites/" + site + "/findings?limit=200&inspection_id=" + url.QueryEscape(root)
			},
			Deps: []Dep{
				ByRoot("inspection_findings", "inspection_id"),
				ByRoot("inspections", "corrects_id"),
				{"corrective_actions", chainDown("corrective_actions")},
				{"finding_waivers", chainDown("finding_waivers")},
			},
			AsOf: map[string]string{
				"inspection_responses": "the item's prompt: copied when the inspection starts and never changed",
			},
		},
		{
			// An action shows its source's correction and review (000019),
			// so a correction completing or an amendment re-sends it.
			Name: "action", Scope: ScopeSite, Pattern: "/v1/sites/:id/actions/:actionId",
			Path: sitePath("/v1/sites/%s/actions/%s"),
			Deps: []Dep{
				ByRoot("corrective_actions", "id"),
				ByRoot("corrective_action_events", "action_id"),
				ByRoot("corrective_action_closeouts", "action_id"),
				{"corrective_action_closeout_evidence", `SELECT c.action_id::text FROM corrective_action_closeout_evidence e
					JOIN corrective_action_closeouts c ON c.id = e.closeout_id WHERE e.site_id = $1 AND ` + W("e")},
				{"inspections", `WITH RECURSIVE up (id) AS (
						SELECT corrects_id FROM inspections WHERE site_id = $1 AND ` + W("") + ` AND corrects_id IS NOT NULL
						UNION SELECT i.corrects_id FROM inspections i JOIN up ON i.id = up.id WHERE i.corrects_id IS NOT NULL)
					SELECT a.id::text FROM corrective_actions a JOIN up ON a.inspection_id = up.id`},
				// An amendment moves the incident's latest_version: the action
				// shows whether it was raised on the latest.
				{"incidents", `SELECT a.id::text FROM incidents i JOIN corrective_actions a ON a.incident_id = i.id
					WHERE i.site_id = $1 AND ` + W("i")},
			},
		},
		{
			Name: "incident", Scope: ScopeSite, Pattern: "/v1/sites/:id/incidents/:incidentId",
			Path: sitePath("/v1/sites/%s/incidents/%s"),
			Deps: []Dep{
				ByRoot("incidents", "id"),
				ByRoot("incident_versions", "incident_id"),
				ByRoot("incident_people", "incident_id"),
				ByRoot("incident_version_evidence", "incident_id"),
				ByRoot("witness_statements", "incident_id"),
				ByRoot("deadlines", "incident_id"),
				ByRoot("deadline_duties", "incident_id"),
				ViaDuty("deadline_satisfactions", "incident_id"),
				ViaDuty("deadline_satisfaction_retractions", "incident_id"),
			},
		},
		{
			// A satisfaction belongs to the duty, so every row of the duty.
			// The GET reads no subject: an amendment that moves a deadline
			// closes or adds deadline rows, which re-sends them (for site
			// work too).
			Name: "deadline", Scope: ScopeSite, Pattern: "/v1/sites/:id/deadlines/:deadlineId",
			Path: sitePath("/v1/sites/%s/deadlines/%s"),
			Deps: []Dep{
				ByRoot("deadlines", "id"),
				dutyRows("deadline_duties", "id"),
				dutyRows("deadline_satisfactions", "duty_id"),
				dutyRows("deadline_satisfaction_retractions", "duty_id"),
			},
		},
		{
			Name: "toolbox_talk", Scope: ScopeSite, Pattern: "/v1/sites/:id/toolbox-talks/:talkId",
			Path: sitePath("/v1/sites/%s/toolbox-talks/%s"),
			Deps: []Dep{
				ByRoot("toolbox_talks", "id"),
				ByRoot("toolbox_talk_evidence", "talk_id"),
				ByRoot("toolbox_attendance", "talk_id"),
				{"toolbox_attendance_retractions", `SELECT a.talk_id::text FROM toolbox_attendance_retractions r
					JOIN toolbox_attendance a ON a.id = r.attendance_id WHERE r.site_id = $1 AND ` + W("r")},
				{"toolbox_attendance_workers", `SELECT a.talk_id::text FROM toolbox_attendance_workers l
					JOIN toolbox_attendance a ON a.id = l.attendance_id WHERE l.site_id = $1 AND ` + W("l")},
				{"workers", `SELECT a.talk_id::text FROM workers w JOIN toolbox_attendance_workers l ON l.worker_id = w.id
					JOIN toolbox_attendance a ON a.id = l.attendance_id WHERE l.site_id = $1 AND ` + W("w")},
				// A signature is matched to the register by identity key too, so
				// a worker added or renamed later changes it.
				{"workers", `SELECT a.talk_id::text FROM workers w JOIN toolbox_attendance a ON a.org_id = w.org_id AND a.identity_key = w.identity_key
					WHERE a.site_id = $1 AND ` + W("w")},
				// not_signed: who signed in that day and didn't sign.
				{"site_sign_ins", `SELECT t.id::text FROM site_sign_ins s JOIN toolbox_talks t ON t.site_id = s.site_id AND t.talk_date = s.sign_in_date
					WHERE s.site_id = $1 AND ` + W("s")},
			},
		},
		{
			// One day's register: its sign-ins, sheets, warnings (from each
			// worker's records), overlaps (their sign-ins on other sites) and
			// headcount (the day's talk signatures). absent is computed from the
			// roster as of server_time.
			Name: "sign_in_day", Scope: ScopeSite, Pattern: "/v1/sites/:id/sign-ins",
			Path: func(site, root string) string { return "/v1/sites/" + site + "/sign-ins?date=" + url.QueryEscape(root) },
			Deps: []Dep{
				ByRoot("site_sign_ins", "sign_in_date"),
				ByRoot("site_sign_in_sheets", "sheet_date"),
				{"site_sign_ins", `SELECT s.sign_in_date::text FROM site_sign_ins o
					JOIN site_sign_ins s ON s.org_id = o.org_id AND s.worker_id = o.worker_id AND s.sign_in_date = o.sign_in_date AND s.site_id = $1
					WHERE o.org_id = (SELECT org_id FROM sites WHERE id = $1) AND o.site_id <> $1 AND ` + W("o")},
				WorkerDays("workers", "id"),
				WorkerDays("worker_competencies", "worker_id"),
				WorkerDays("worker_medicals", "worker_id"),
				ByRoot("toolbox_talks", "talk_date"),
				{"toolbox_attendance", TalkDate("toolbox_attendance", "x.talk_id")},
				{"toolbox_attendance_retractions", TalkDate("toolbox_attendance_retractions",
					"(SELECT talk_id FROM toolbox_attendance a WHERE a.id = x.attendance_id)")},
				{"toolbox_attendance_workers", TalkDate("toolbox_attendance_workers",
					"(SELECT talk_id FROM toolbox_attendance a WHERE a.id = x.attendance_id)")},
			},
			AsOf: map[string]string{
				"site_roster":    "absent: who was expected and didn't sign in; today's moves with the clock, a past day's never changes",
				"worker_history": "absent: whether each expected worker was active on that day",
			},
		},
		{
			Name: "roster", Scope: ScopeSite, Pattern: "/v1/sites/:id/roster",
			Path: func(site, _ string) string { return "/v1/sites/" + site + "/roster?all=true" },
			Deps: []Dep{
				ByRoot("site_roster", "site_id"),
				{"workers", `SELECT r.site_id::text FROM workers w JOIN site_roster r ON r.worker_id = w.id
					WHERE r.site_id = $1 AND ` + W("w")},
				// last_sign_in_on and expected.
				ByRoot("site_sign_ins", "site_id"),
			},
		},
		{
			Name: "worker", Scope: ScopeOrg, Pattern: "/v1/workers/:workerId",
			Path: func(_, root string) string { return "/v1/workers/" + url.PathEscape(root) },
			Deps: []Dep{
				{"workers", `SELECT id::text FROM workers WHERE org_id = $1 AND ` + W("")},
				{"worker_competencies", `SELECT worker_id::text FROM worker_competencies WHERE org_id = $1 AND ` + W("")},
				{"worker_medicals", `SELECT worker_id::text FROM worker_medicals WHERE org_id = $1 AND ` + W("")},
			},
		},
		{
			Name: "vault_document", Scope: ScopeOrg, Leads: true, Pattern: "/v1/vault/documents/:documentId",
			Path: func(_, root string) string { return "/v1/vault/documents/" + url.PathEscape(root) },
			Deps: []Dep{
				{"documents", `SELECT id::text FROM documents WHERE org_id = $1 AND scope = 'org' AND ` + W("")},
				{"document_versions", `SELECT document_id::text FROM document_versions WHERE org_id = $1 AND scope = 'org' AND ` + W("")},
			},
		},
		{
			// The caller's inbox as GET /v1/notifications shows it. Its
			// subject_state is notification_subject_current (000048), read
			// from each subject: a change to a subject re-sends the inbox.
			Name: "notification_list", Scope: ScopeOrg, Pattern: "/v1/notifications",
			Path: func(_, _ string) string { return "/v1/notifications?limit=200" },
			Deps: []Dep{
				{"notifications", `SELECT user_id::text FROM notifications WHERE user_id = $4 AND ` + W("")},
				told("deadlines", "(n.subject_type = 'deadline' AND x.id = n.subject_id) OR (n.subject_type = 'deadline_duty' AND x.duty_id = n.subject_id)"),
				told("deadline_duties", "(n.subject_type = 'deadline_duty' AND x.id = n.subject_id) OR "+
					"(n.subject_type = 'deadline' AND x.id = (SELECT duty_id FROM deadlines d WHERE d.id = n.subject_id))"),
				told("corrective_actions", "(n.subject_type = 'action' AND x.id = n.subject_id) OR "+
					"(n.subject_type = 'closeout' AND x.id = (SELECT action_id FROM corrective_action_closeouts c WHERE c.id = n.subject_id))"),
				told("worker_competencies", "n.subject_type = 'competency' AND (x.id = n.subject_id OR x.supersedes_id = n.subject_id)"),
				told("worker_medicals", "n.subject_type = 'medical' AND (x.id = n.subject_id OR x.supersedes_id = n.subject_id)"),
				told("site_appointments", "n.subject_type = 'appointment' AND (x.id = n.subject_id OR x.supersedes_id = n.subject_id)"),
				told("documents", "n.subject_type = 'document' AND x.id = n.subject_id"),
				told("document_versions", "n.subject_type = 'document' AND x.document_id = n.subject_id"),
			},
		},
	}
}

// told maps a change to table to the caller's inbox, when they were told
// about the changed row (on: x the row, n the notification).
func told(table, on string) Dep {
	return Dep{table, `SELECT n.user_id::text FROM notifications n JOIN ` + table + ` x ON ` + on + `
		WHERE n.user_id = $4 AND ` + W("x")}
}

// WorkerDays maps a change to a worker's row in table to the days that
// worker signed in on the site.
func WorkerDays(table, workerCol string) Dep {
	return Dep{table, `SELECT s.sign_in_date::text FROM ` + table + ` x JOIN site_sign_ins s ON s.worker_id = x.` + workerCol + `
		WHERE s.site_id = $1 AND ` + W("x")}
}

// TalkDate maps a change in table (alias x) to the day of the talk named by
// talk, an expression on x.
func TalkDate(table, talk string) string {
	return `SELECT t.talk_date::text FROM ` + table + ` x JOIN toolbox_talks t ON t.id = ` + talk + `
		WHERE x.site_id = $1 AND ` + W("x")
}

// chainDown maps rows of table (on an inspection) to that inspection and
// every later correction of it: finding_coverage reads the whole chain.
func chainDown(table string) string {
	return `WITH RECURSIVE down (id) AS (
			SELECT inspection_id FROM ` + table + ` WHERE site_id = $1 AND ` + W("") + ` AND inspection_id IS NOT NULL
			UNION SELECT i.id FROM inspections i JOIN down ON i.corrects_id = down.id)
		SELECT id::text FROM down`
}

// dutyRows maps a duty table's rows to every deadline row of the duty.
func dutyRows(table, dutyCol string) Dep {
	return Dep{table, fmt.Sprintf(`SELECT d.id::text FROM %s x JOIN deadlines d ON d.duty_id = x.%s
		WHERE x.site_id = $1 AND %s`, table, dutyCol, W("x"))}
}
