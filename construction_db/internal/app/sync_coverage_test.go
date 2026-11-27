package app

import (
	"context"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/devicesync"
)

// syncServerOnly are the tables no pull reads, each with why (D1: one design
// for every table, so a new table must choose).
var syncServerOnly = map[string]string{
	"organisations":             "the caller's organisation is /v1/me's, sent whole on every org pull",
	"users":                     "no org; names reach a phone inside the records that show them",
	"sites":                     "the site list is sent whole on every org pull (D4, D5)",
	"sessions":                  "auth, online only",
	"refresh_tokens":            "auth, online only",
	"invitations":               "team management, online only",
	"evidence":                  "the phone's own upload queue; complete is the status call; records name photos (D3)",
	"site_chain_heads":          "the chain is the server's proof",
	"evidence_chain":            "the chain is the server's proof",
	"evidence_access_log":       "server-only log",
	"files":                     "as evidence: the phone's own upload queue",
	"file_access_log":           "server-only log",
	"public_holidays":           "deadline arithmetic is the server's; due dates arrive in deadlines",
	"holiday_calendar_coverage": "deadline arithmetic is the server's",
	"regulatory_regimes":        "the reference bundle (GET /v1/sync/sites/:id/reference)",
	"notification_emails":       "the worker's outbox",
	"job_runs":                  "the worker's bookkeeping",
	"holiday_recomputations":    "the worker's bookkeeping",
	"worker_history":            "history tables are server-only (decided 20 Nov)",
	"organisation_history":      "history tables are server-only (decided 20 Nov)",
	"site_history":              "history tables are server-only (decided 20 Nov)",
	"member_history":            "history tables are server-only (decided 20 Nov)",
	"toolbox_talk_history":      "history tables are server-only (decided 20 Nov)",
	"site_member_history":       "history tables are server-only (decided 20 Nov; ONBOARD D7b)",
	"audit_exports":             "exports are server-only (decided 20 Nov)",
	"audit_export_attempts":     "exports are server-only",
	"audit_export_results":      "exports are server-only",
	"audit_export_evidence":     "exports are server-only",
	"audit_export_files":        "exports are server-only",
	"audit_export_downloads":    "exports are server-only",
	"audit_export_backoff":      "exports are server-only",
	"sync_generation":           "the sync epoch itself (D2)",
	"schema_migrations":         "migrate's bookkeeping",
}

func syncKinds() []devicesync.Kind {
	return append(devicesync.CoreKinds(), testEdition.SyncSources().Kinds...)
}

type syncTable struct {
	name                       string
	column, stamped, indexed   bool
	stampLast                  bool
	deletionTrigger, hasPublic bool
}

func loadSyncCatalog(t *testing.T, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) map[string]syncTable {
	t.Helper()
	rows, err := db.Query(context.Background(), `
		SELECT c.relname,
		       EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'sync_xid'
		               AND a.atttypid = 'xid8'::regtype AND a.attnotnull AND NOT a.attisdropped),
		       EXISTS (SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid = g.tgfoid
		               WHERE g.tgrelid = c.oid AND g.tgname = 'zz_sync_stamp' AND p.proname = 'sync_stamp'
		               AND g.tgenabled = 'O' AND g.tgtype & 2 = 2 AND g.tgtype & 4 = 4 AND g.tgtype & 16 = 16),
		       EXISTS (SELECT 1 FROM pg_index i JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[1]
		               WHERE i.indrelid = c.oid AND a.attname = 'sync_xid'),
		       -- sync_deleted, or a table's own <table>_sync_deleted that fans a
		       -- deletion out to other roots (site_members: the member's actions).
		       EXISTS (SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid = g.tgfoid
		               WHERE g.tgrelid = c.oid AND (p.proname = 'sync_deleted' OR p.proname = c.relname || '_sync_deleted') AND g.tgenabled = 'O' AND g.tgtype & 8 = 8),
		       -- Triggers on one event fire in name order (bytewise): no BEFORE
		       -- row trigger on insert or update may come after the stamp.
		       NOT EXISTS (SELECT 1 FROM pg_trigger g WHERE g.tgrelid = c.oid AND NOT g.tgisinternal
		                   AND g.tgtype & 1 = 1 AND g.tgtype & 2 = 2 AND (g.tgtype & 4 = 4 OR g.tgtype & 16 = 16)
		                   AND g.tgname COLLATE "C" > 'zz_sync_stamp'),
		       true
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'`)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]syncTable{}
	for rows.Next() {
		var x syncTable
		if err := rows.Scan(&x.name, &x.column, &x.stamped, &x.indexed, &x.deletionTrigger, &x.stampLast, &x.hasPublic); err != nil {
			t.Fatal(err)
		}
		out[x.name] = x
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// syncProblems: every table is watched by a kind (and then stamped and
// indexed on sync_xid) or server-only; a watched table the code deletes
// from records its deletions.
func syncProblems(tables map[string]syncTable, watched map[string]bool, deletedFrom []string) []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(tables)) {
		x := tables[name]
		_, serverOnly := syncServerOnly[name]
		switch {
		case watched[name] && serverOnly:
			out = append(out, name+": watched by a kind and listed server-only")
		case watched[name]:
			if !x.column {
				out = append(out, name+": no sync_xid xid8 NOT NULL")
			}
			if !x.stamped {
				out = append(out, name+": no zz_sync_stamp trigger")
			}
			if !x.stampLast {
				out = append(out, name+": a BEFORE trigger fires after zz_sync_stamp")
			}
			if !x.indexed {
				out = append(out, name+": no index on (scope, sync_xid)")
			}
			if slices.Contains(deletedFrom, name) && !x.deletionTrigger {
				out = append(out, name+": deleted from with no sync_deleted trigger")
			}
		case !serverOnly && name != "schema_migrations":
			out = append(out, name+": neither watched by a sync kind nor server-only")
		}
	}
	for name := range watched {
		if _, ok := tables[name]; !ok {
			out = append(out, name+": a kind watches a table that doesn't exist")
		}
	}
	return out
}

var deleteFrom = regexp.MustCompile(`(?i)DELETE\s+FROM\s+(\w+)`)

// deletedFrom is every table a DELETE in the non-test Go code names.
func deletedFrom(t *testing.T) []string {
	t.Helper()
	var out []string
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range deleteFrom.FindAllStringSubmatch(string(b), -1) {
			out = append(out, m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSyncCoverage(t *testing.T) {
	tables := loadSyncCatalog(t, testAdmin)
	watched := devicesync.Tables(syncKinds())
	deleted := deletedFrom(t)
	if !slices.Contains(deleted, "inspection_response_evidence") {
		t.Fatalf("the delete scan found %v; it would pass vacuously", deleted)
	}
	if len(watched) < 40 {
		t.Fatalf("only %d tables watched", len(watched))
	}
	for _, p := range syncProblems(tables, watched, deleted) {
		t.Error(p)
	}

	probe := func(t *testing.T, ddl, want string) {
		t.Helper()
		ctx := context.Background()
		tx, err := testAdmin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, ddl); err != nil {
			t.Fatalf("probe: %v", err)
		}
		if got := syncProblems(loadSyncCatalog(t, tx), watched, deleted); !slices.Contains(got, want) {
			t.Errorf("problems %q lack %q", got, want)
		}
	}
	t.Run("a new table must choose", func(t *testing.T) {
		probe(t, `CREATE TABLE sync_probe (org_id uuid)`, "sync_probe: neither watched by a sync kind nor server-only")
	})
	t.Run("a stamp switched off", func(t *testing.T) {
		probe(t, `ALTER TABLE incidents DISABLE TRIGGER zz_sync_stamp`, "incidents: no zz_sync_stamp trigger")
	})
	t.Run("a stamp dropped", func(t *testing.T) {
		probe(t, `DROP TRIGGER zz_sync_stamp ON workers`, "workers: no zz_sync_stamp trigger")
	})
	t.Run("a trigger after the stamp", func(t *testing.T) {
		probe(t, `CREATE TRIGGER zzz_after_stamp BEFORE UPDATE ON incidents FOR EACH ROW EXECUTE FUNCTION sync_stamp()`,
			"incidents: a BEFORE trigger fires after zz_sync_stamp")
	})
	t.Run("an index dropped", func(t *testing.T) {
		probe(t, `DROP INDEX deadlines_sync_idx`, "deadlines: no index on (scope, sync_xid)")
	})
	t.Run("the deletion trigger dropped", func(t *testing.T) {
		probe(t, `DROP TRIGGER zz_sync_deleted ON inspection_response_evidence`,
			"inspection_response_evidence: deleted from with no sync_deleted trigger")
		probe(t, `DROP TRIGGER zz_sync_deleted ON site_members`, "site_members: deleted from with no sync_deleted trigger")
	})
	t.Run("a nullable column", func(t *testing.T) {
		probe(t, `ALTER TABLE site_roster ALTER COLUMN sync_xid DROP NOT NULL`, "site_roster: no sync_xid xid8 NOT NULL")
	})
}

// syncOnlineOnly are the write routes a batch never carries, each with why
// (D6, D7). A new write route must be on this list or a push list.
var syncOnlineOnly = map[string]string{
	"POST /v1/auth/signup":                                                    "auth: before any session",
	"POST /v1/auth/login":                                                     "auth: before any session",
	"POST /v1/auth/refresh":                                                   "auth: its own retry rule (the refresh grace)",
	"POST /v1/auth/logout":                                                    "session management",
	"DELETE /v1/auth/sessions/:id":                                            "session management",
	"POST /v1/invites/preview":                                                "team management, online",
	"POST /v1/invites/accept":                                                 "team management, online",
	"POST /v1/invites":                                                        "team management, online",
	"DELETE /v1/invites/:id":                                                  "team management, online",
	"PATCH /v1/members/:userId":                                               "team management (ONBOARD D13): a lead at the dashboard; the rules need the live membership",
	"POST /v1/sites":                                                          "site set-up by leads, online (server ids)",
	"PATCH /v1/sites/:id":                                                     "site set-up by leads, online; last write wins",
	"DELETE /v1/sites/:id":                                                    "site set-up by leads, online",
	"PUT /v1/sites/:id/members/:userId":                                       "who sees a site, online",
	"DELETE /v1/sites/:id/members/:userId":                                    "who sees a site, online",
	"POST /v1/sites/:id/locations":                                            "site set-up by leads, online (server ids)",
	"PATCH /v1/sites/:id/locations/:locationId":                               "site set-up by leads, online; last write wins",
	"DELETE /v1/sites/:id/locations/:locationId":                              "site set-up by leads, online",
	"POST /v1/sites/:id/evidence/:evidenceId/complete":                        "one request per file (decided 20 Nov)",
	"POST /v1/sites/:id/evidence/:evidenceId/view":                            "a logged view, online",
	"POST /v1/files/:fileId/complete":                                         "one request per file (decided 20 Nov)",
	"POST /v1/workers/:workerId/competencies/:recordId/view":                  "a logged view, online",
	"POST /v1/workers/:workerId/medicals/:recordId/view":                      "a logged view, online",
	"PUT /v1/sites/:id/toolbox-talks/:talkId/attendance/:attendanceId/worker": "a lead's register task at the dashboard (D7)",
	"POST /v1/sites/:id/toolbox-talks/:talkId/attendance/:attendanceId/worker/unlink": "a lead's register task at the dashboard (D7)",
	"POST /v1/vault/documents/:documentId/versions/:version/view":                     "a logged view, online",
	"POST /v1/construction/sites/:id/safety-file/:documentId/versions/:version/view":  "a logged view, online",
	"POST /v1/construction/sites/:id/appointments/:appointmentId/view":                "a logged view, online",
	"PUT /v1/sites/:id/exports/:exportId":                                             "exports are server-only (decided 20 Nov)",
	"POST /v1/sites/:id/exports/:exportId/download":                                   "exports are server-only (decided 20 Nov)",
	"POST /v1/sync/push": "the batch itself",
}

func TestPushRoutes(t *testing.T) {
	router := newRouter(t)
	push := append(devicesync.CorePushRoutes(), testEdition.SyncSources().Push...)
	routes := map[string]bool{}
	for _, r := range router.Routes() {
		routes[r.Method+" "+r.Path] = true
		if r.Method == http.MethodGet {
			continue
		}
		key := r.Method + " " + r.Path
		_, online := syncOnlineOnly[key]
		inPush := slices.Contains(push, key)
		switch {
		case online && inPush:
			t.Errorf("%s is both online-only and pushable", key)
		case !online && !inPush:
			t.Errorf("%s: a write route neither pushable nor online-only; choose (devicesync.CorePushRoutes, Edition.SyncSources, or syncOnlineOnly)", key)
		}
	}
	for _, p := range push {
		if !routes[p] {
			t.Errorf("push route %s doesn't exist", p)
		}
	}
	for k := range syncOnlineOnly {
		if !routes[k] {
			t.Errorf("online-only route %s doesn't exist", k)
		}
	}
	for _, k := range syncKinds() {
		if !routes["GET "+k.Pattern] {
			t.Errorf("kind %s renders through %s, which isn't a GET route", k.Name, k.Pattern)
		}
	}
}
