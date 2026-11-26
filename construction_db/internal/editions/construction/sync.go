package construction

import (
	"net/url"

	"construction_db/internal/core/devicesync"
)

// SyncSources: the edition's records in pull and push (SYNC; REG4 D13 for
// site work). Deadlines are pulled through core's deadline kind.
func (e Edition) SyncSources() devicesync.Sources {
	base := "/v1/" + e.Name() + "/sites/"
	at := func(format string) func(string, string) string {
		return func(site, root string) string { return base + site + format + url.PathEscape(root) }
	}
	w := devicesync.W
	return devicesync.Sources{
		Kinds: []devicesync.Kind{
			{
				// The day as GET shows it: its current revision, conflicts and
				// addenda, the day's talks and incidents, and the headcount
				// suggested from that day's sign-ins.
				Name: "diary_day", Scope: devicesync.ScopeSite, Pattern: base + ":id/diary/:date",
				Path: at("/diary/"),
				Deps: []devicesync.Dep{
					devicesync.ByRoot("site_diaries", "diary_date"),
					devicesync.ByRoot("site_diary_revisions", "diary_date"),
					devicesync.ByRoot("site_diary_revision_evidence", "diary_date"),
					devicesync.ByRoot("site_diary_addenda", "diary_date"),
					devicesync.ByRoot("site_diary_addendum_evidence", "diary_date"),
					devicesync.ByRoot("toolbox_talks", "talk_date"),
					devicesync.ByRoot("site_sign_ins", "sign_in_date"),
					{Table: "incident_versions", SQL: `SELECT d.diary_date::text FROM incident_versions v
						JOIN site_diaries d ON d.site_id = v.site_id AND d.diary_date = (v.occurred_at AT TIME ZONE d.time_zone)::date
						WHERE v.site_id = $1 AND ` + w("v")},
					// The day's incidents are by their latest version.
					{Table: "incidents", SQL: `SELECT d.diary_date::text FROM incidents i
						JOIN incident_versions v ON v.incident_id = i.id AND v.version = i.latest_version
						JOIN site_diaries d ON d.site_id = i.site_id AND d.diary_date = (v.occurred_at AT TIME ZONE d.time_zone)::date
						WHERE i.site_id = $1 AND ` + w("i")},
					// The headcount suggested counts the day's talk signatures
					// and sign-ins, one per person.
					{Table: "toolbox_attendance", SQL: devicesync.TalkDate("toolbox_attendance", "x.talk_id")},
					{Table: "toolbox_attendance_retractions", SQL: devicesync.TalkDate("toolbox_attendance_retractions",
						"(SELECT talk_id FROM toolbox_attendance a WHERE a.id = x.attendance_id)")},
					{Table: "toolbox_attendance_workers", SQL: devicesync.TalkDate("toolbox_attendance_workers",
						"(SELECT talk_id FROM toolbox_attendance a WHERE a.id = x.attendance_id)")},
					devicesync.WorkerDays("workers", "id"),
					{Table: "workers", SQL: `SELECT t.talk_date::text FROM workers x
						JOIN toolbox_attendance a ON a.org_id = x.org_id AND a.identity_key = x.identity_key
						JOIN toolbox_talks t ON t.id = a.talk_id WHERE a.site_id = $1 AND ` + w("x")},
				},
			},
			{
				Name: "site_work", Scope: devicesync.ScopeSite, Pattern: base + ":id/site-work/:workId",
				Path: at("/site-work/"),
				Deps: []devicesync.Dep{
					devicesync.ByRoot("site_work", "id"),
					devicesync.ByRoot("site_work_versions", "site_work_id"),
					devicesync.ByRoot("deadlines", "site_work_id"),
					devicesync.ByRoot("deadline_duties", "site_work_id"),
					devicesync.ViaDuty("deadline_satisfactions", "site_work_id"),
					devicesync.ViaDuty("deadline_satisfaction_retractions", "site_work_id"),
				},
			},
			{
				Name: "appointment_list", Scope: devicesync.ScopeSite, Pattern: base + ":id/appointments",
				Path: func(site, _ string) string { return base + site + "/appointments" },
				Deps: []devicesync.Dep{
					devicesync.ByRoot("site_appointments", "site_id"),
					{Table: "workers", SQL: `SELECT a.site_id::text FROM workers w JOIN site_appointments a ON a.worker_id = w.id
						WHERE a.site_id = $1 AND ` + w("w")},
				},
			},
			{
				Name: "safety_file_document", Scope: devicesync.ScopeSite, Pattern: base + ":id/safety-file/:documentId",
				Path: at("/safety-file/"),
				Deps: []devicesync.Dep{
					devicesync.ByRoot("documents", "id"),
					devicesync.ByRoot("document_versions", "document_id"),
				},
			},
		},
		Reference: []devicesync.RefPart{
			{Name: "site_work_facts", Pattern: base + ":id/site-work-facts",
				Path: func(site, _ string) string { return base + site + "/site-work-facts" }},
		},
		Push: []string{
			"PUT " + base + ":id/diary/:date/revisions/:revisionId",
			"PUT " + base + ":id/diary/:date/addenda/:addendumId",
			"PUT " + base + ":id/site-work/:workId/versions/:version",
			"PUT " + base + ":id/appointments/:appointmentId",
			"POST " + base + ":id/appointments/:appointmentId/retract",
			"PUT " + base + ":id/safety-file/:documentId",
			"PUT " + base + ":id/safety-file/:documentId/versions/:version",
			"POST " + base + ":id/safety-file/:documentId/retract",
			"POST " + base + ":id/safety-file/:documentId/versions/:version/retract",
		},
	}
}
