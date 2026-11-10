package database_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/checklists"
	"construction_db/internal/platform/database"
)

// Inspection ids used inside rolled-back transactions; "2b" so they can't
// collide with the RLS fixtures' "2a".
func inspID(n int) string { return fmt.Sprintf("2b000000-0000-0000-0000-%012d", n) }

const (
	startSQL = `
		INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
		                         template_title, template_source_ref, item_count, started_by, client_started_at, corrects_id)
		SELECT $1, $2, $3, id, 'test_check', 1, $4, 'Test check', 'reg 1', $5, $6, now(), $7 FROM regulatory_regimes LIMIT 1`
	responseSQL = `
		INSERT INTO inspection_responses (org_id, site_id, inspection_id, item_key, position, prompt, response_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'pass_fail_na')`
	answerSQL = `
		UPDATE inspection_responses SET result = $3, severity = $4, client_answered_at = now(), answered_at = now()
		WHERE inspection_id = $1 AND item_key = $2`
	attachSQL = `
		INSERT INTO inspection_response_evidence (org_id, site_id, inspection_id, item_key, evidence_id)
		SELECT org_id, site_id, id, $2, $3 FROM inspections WHERE id = $1`
	completeSQL = `
		UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = now() WHERE id = $1`
	evidenceSQL = `
		INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at,
		                      object_key, object_version_id, retain_until, status)
		VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', now(), $6, $7, now() + interval '1 day', $8)`
)

// sqlRunner is a transaction, as the app or as superuser.
type sqlRunner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// must runs one statement in a kept savepoint and fails unless it changed want rows.
func must(t *testing.T, tx pgx.Tx, want int64, sql string, args ...any) {
	t.Helper()
	n, err := inSavepoint(context.Background(), tx, true, sql, args...)
	if err != nil || n != want {
		t.Fatalf("%s: changed %d rows, err %v; want %d", strings.TrimSpace(sql), n, err, want)
	}
}

// refused runs one statement in a discarded savepoint and checks its SQLSTATE
// and, if given, a fragment of the message.
func refused(t *testing.T, tx pgx.Tx, code, msg, sql string, args ...any) {
	t.Helper()
	_, err := inSavepoint(context.Background(), tx, false, sql, args...)
	if pgCode(err) != code || (msg != "" && (err == nil || !strings.Contains(err.Error(), msg))) {
		t.Errorf("%s:\n  got %v\n  want SQLSTATE %s %q", strings.TrimSpace(sql), err, code, msg)
	}
}

// start inserts an in-progress inspection with one response per item, as the
// start endpoint will: item_count is the number of items.
func start(t *testing.T, tx pgx.Tx, org, site, user, id string, corrects any, items ...string) {
	t.Helper()
	must(t, tx, 1, startSQL, id, org, site, hash32(5), len(items), user, corrects)
	for i, it := range items {
		must(t, tx, 1, responseSQL, org, site, id, it, i+1, "Is "+it+" OK?")
	}
}

func answer(t *testing.T, tx pgx.Tx, id, item, result string, severity any) {
	t.Helper()
	must(t, tx, 1, answerSQL, id, item, result, severity)
}

// beginAs starts a transaction rolled back at the end of the test, with
// tenant org (empty for a superuser transaction, which bypasses RLS).
func beginAs(t *testing.T, db sqlRunner, org string) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if org != "" {
		if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: org}); err != nil {
			t.Fatalf("set tenant: %v", err)
		}
	}
	return tx
}

// The rules the database enforces on inspections, as the app role.
func TestInspectionRules(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)

	const (
		otherSite = "22000000-0000-0000-0000-0000000000a2"
		pendingEv = "2d000000-0000-0000-0000-00000000000a"
		otherEv   = "2d000000-0000-0000-0000-0000000000a2"
	)
	must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Second site' FROM regulatory_regimes LIMIT 1`, otherSite, rlsOrgA)
	must(t, tx, 1, evidenceSQL, pendingEv, rlsOrgA, rlsSiteA, rlsUserA, hash32(6), "insp/pending", nil, "pending")
	must(t, tx, 1, evidenceSQL, otherEv, rlsOrgA, otherSite, rlsUserA, hash32(7), "insp/other-site", "v1", "verified")

	t.Run("a new inspection starts in progress", func(t *testing.T) {
		refused(t, tx, "23514", "starts in progress", `
			INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256, template_title,
			                         template_source_ref, item_count, started_by, client_started_at, status, completed_at, client_completed_at)
			SELECT $1, $2, $3, id, 'k', 1, $4, 't', 'r', 1, $5, now(), 'completed', now(), now() FROM regulatory_regimes LIMIT 1`,
			inspID(1), rlsOrgA, rlsSiteA, hash32(5), rlsUserA)
	})

	start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(2), nil, "a", "b")

	t.Run("answers", func(t *testing.T) {
		refused(t, tx, "23514", "inspection_responses_severity_on_fail", answerSQL, inspID(2), "a", "fail", nil)
		refused(t, tx, "23514", "inspection_responses_severity_on_fail", answerSQL, inspID(2), "a", "pass", "high")
		refused(t, tx, "23514", "inspection_responses_severity_check", answerSQL, inspID(2), "a", "fail", "urgent")
		refused(t, tx, "23514", "inspection_responses_result_check", answerSQL, inspID(2), "a", "maybe", nil)
		refused(t, tx, "23514", "inspection_responses_answered", `UPDATE inspection_responses SET result = 'pass' WHERE inspection_id = $1 AND item_key = 'a'`, inspID(2))
		refused(t, tx, "23514", "inspection_responses_answered", `UPDATE inspection_responses SET comment = 'x' WHERE inspection_id = $1 AND item_key = 'a'`, inspID(2))
		refused(t, tx, "42501", "", `UPDATE inspection_responses SET prompt = 'Changed?' WHERE inspection_id = $1 AND item_key = 'a'`, inspID(2))
		answer(t, tx, inspID(2), "a", "fail", "high")
	})

	t.Run("photos: only verified evidence from the same site", func(t *testing.T) {
		refused(t, tx, "23503", "", attachSQL, inspID(2), "a", pendingEv)
		refused(t, tx, "23503", "", attachSQL, inspID(2), "a", otherEv)
		must(t, tx, 1, attachSQL, inspID(2), "a", rlsEvidenceA)
		// While in progress a photo can be removed (an answer replaces its photos).
		if n, err := inSavepoint(context.Background(), tx, false, `DELETE FROM inspection_response_evidence WHERE inspection_id = $1`, inspID(2)); err != nil || n != 1 {
			t.Errorf("remove a photo while in progress: %d rows, err %v", n, err)
		}
	})

	t.Run("completion needs every item answered", func(t *testing.T) {
		start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(3), nil, "a", "b", "c")
		answer(t, tx, inspID(3), "a", "pass", nil)
		refused(t, tx, "23514", "items not answered: b, c", completeSQL, inspID(3))
	})

	t.Run("completion needs a photo on every fail", func(t *testing.T) {
		start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(4), nil, "a", "b")
		answer(t, tx, inspID(4), "a", "fail", "low")
		answer(t, tx, inspID(4), "b", "pass", nil)
		refused(t, tx, "23514", "a failed item needs a photo: a", completeSQL, inspID(4))
	})

	t.Run("completion needs exactly the template's items", func(t *testing.T) {
		start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(5), nil, "a", "b")
		must(t, tx, 1, responseSQL, rlsOrgA, rlsSiteA, inspID(5), "slipped_in", 3, "Extra?")
		for _, it := range []string{"a", "b", "slipped_in"} {
			answer(t, tx, inspID(5), it, "pass", nil)
		}
		refused(t, tx, "23514", "has 3 responses but its template version has 2 items", completeSQL, inspID(5))
	})

	// Kept for the subtests after it.
	start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(6), nil, "a", "b", "c")
	answer(t, tx, inspID(6), "a", "fail", "high")
	answer(t, tx, inspID(6), "b", "fail", "critical")
	answer(t, tx, inspID(6), "c", "na", nil)
	must(t, tx, 1, attachSQL, inspID(6), "a", rlsEvidenceA)
	must(t, tx, 1, attachSQL, inspID(6), "b", rlsEvidenceA)

	t.Run("completing raises one finding per fail, with its severity", func(t *testing.T) {
		must(t, tx, 1, completeSQL, inspID(6))
		rows, err := tx.Query(context.Background(), `SELECT item_key || ':' || severity FROM inspection_findings WHERE inspection_id = $1 ORDER BY item_key`, inspID(6))
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || !slices.Equal(got, []string{"a:high", "b:critical"}) {
			t.Errorf("findings %v, err %v; want [a:high b:critical]", got, err)
		}
	})

	t.Run("a completed inspection is frozen", func(t *testing.T) {
		const frozen = "is completed; start a new inspection to correct it"
		refused(t, tx, "23514", frozen, answerSQL, inspID(6), "c", "pass", nil)
		refused(t, tx, "23514", frozen, responseSQL, rlsOrgA, rlsSiteA, inspID(6), "d", 4, "Late?")
		refused(t, tx, "23514", frozen, attachSQL, inspID(6), "c", rlsEvidenceA)
		refused(t, tx, "23514", frozen, `DELETE FROM inspection_response_evidence WHERE inspection_id = $1`, inspID(6))
		refused(t, tx, "23514", frozen, `UPDATE inspections SET client_completed_at = now() - interval '1 day' WHERE id = $1`, inspID(6))
		// No privilege at all: the app can't even try.
		refused(t, tx, "42501", "", `DELETE FROM inspection_responses WHERE inspection_id = $1`, inspID(6))
		refused(t, tx, "42501", "", `UPDATE inspections SET template_title = 'X' WHERE id = $1`, inspID(6))
		refused(t, tx, "42501", "", `DELETE FROM inspections WHERE id = $1`, inspID(6))
		refused(t, tx, "42501", "", `UPDATE inspection_findings SET severity = 'low' WHERE inspection_id = $1`, inspID(6))
		refused(t, tx, "42501", "", `DELETE FROM inspection_findings WHERE inspection_id = $1`, inspID(6))
		for _, table := range []string{"inspections", "inspection_responses", "inspection_response_evidence", "inspection_findings"} {
			refused(t, tx, "42501", "", "TRUNCATE "+table)
		}
		// A finding can only point at a fail, and each fail has exactly one.
		refused(t, tx, "23503", "", `INSERT INTO inspection_findings (org_id, site_id, inspection_id, item_key, severity) VALUES ($1, $2, $3, 'c', 'low')`,
			rlsOrgA, rlsSiteA, inspID(6))
		refused(t, tx, "23505", "", `INSERT INTO inspection_findings (org_id, site_id, inspection_id, item_key, severity) VALUES ($1, $2, $3, 'a', 'high')`,
			rlsOrgA, rlsSiteA, inspID(6))
	})

	t.Run("findings only come from completion", func(t *testing.T) {
		start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(7), nil, "a")
		answer(t, tx, inspID(7), "a", "fail", "medium")
		refused(t, tx, "23514", "findings are raised when it is completed",
			`INSERT INTO inspection_findings (org_id, site_id, inspection_id, item_key, severity) VALUES ($1, $2, $3, 'a', 'medium')`,
			rlsOrgA, rlsSiteA, inspID(7))
	})

	t.Run("corrections", func(t *testing.T) {
		refused(t, tx, "23514", "can only correct a completed inspection", startSQL, inspID(8), rlsOrgA, rlsSiteA, hash32(5), 1, rlsUserA, inspID(2))
		refused(t, tx, "23514", "can only correct a completed inspection", startSQL, inspID(8), rlsOrgA, rlsSiteA, hash32(5), 1, rlsUserA, inspID(8))
		refused(t, tx, "23503", "inspections_corrects_fkey", startSQL, inspID(8), rlsOrgA, otherSite, hash32(5), 1, rlsUserA, inspID(6))
		must(t, tx, 1, startSQL, inspID(8), rlsOrgA, rlsSiteA, hash32(5), 1, rlsUserA, inspID(6))
	})
}

// The guards hold for every role: a superuser bypasses privileges and RLS,
// so only the triggers stop it.
func TestInspectionsFrozenForEveryRole(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")

	start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(20), nil, "a")
	t.Run("in progress, only the answer and the completion change", func(t *testing.T) {
		refused(t, tx, "23514", "only the answer can change", `UPDATE inspection_responses SET prompt = 'Changed?' WHERE inspection_id = $1`, inspID(20))
		refused(t, tx, "23514", "the only change allowed is completing it", `UPDATE inspections SET template_title = 'X' WHERE id = $1`, inspID(20))
		refused(t, tx, "23514", "the only change allowed is completing it", `UPDATE inspections SET status = 'in_progress' WHERE id = $1`, inspID(20))
	})

	answer(t, tx, inspID(20), "a", "fail", "critical")
	must(t, tx, 1, attachSQL, inspID(20), "a", rlsEvidenceA)
	must(t, tx, 1, completeSQL, inspID(20))

	t.Run("completed, nothing changes", func(t *testing.T) {
		const frozen = "is completed; start a new inspection to correct it"
		refused(t, tx, "23514", frozen, answerSQL, inspID(20), "a", "pass", nil)
		refused(t, tx, "23514", frozen, `DELETE FROM inspection_responses WHERE inspection_id = $1`, inspID(20))
		refused(t, tx, "23514", frozen, responseSQL, rlsOrgA, rlsSiteA, inspID(20), "b", 2, "Late?")
		refused(t, tx, "23514", frozen, `DELETE FROM inspection_response_evidence WHERE inspection_id = $1`, inspID(20))
		refused(t, tx, "23514", "a photo link never changes", `UPDATE inspection_response_evidence SET attached_at = now() WHERE inspection_id = $1`, inspID(20))
		refused(t, tx, "23514", frozen, `UPDATE inspections SET template_title = 'X' WHERE id = $1`, inspID(20))
		refused(t, tx, "23514", frozen, `DELETE FROM inspections WHERE id = $1`, inspID(20))
		refused(t, tx, "23514", "findings never change", `UPDATE inspection_findings SET severity = 'low' WHERE inspection_id = $1`, inspID(20))
		refused(t, tx, "23514", "findings never change", `DELETE FROM inspection_findings WHERE inspection_id = $1`, inspID(20))
		for _, table := range []string{"inspections", "inspection_responses", "inspection_response_evidence", "inspection_findings"} {
			refused(t, tx, "23514", "cannot be truncated", "TRUNCATE "+table+" CASCADE")
		}
	})
}

// The database's CHECK lists and the Go lists can't drift apart.
func TestInspectionChecksMatchGo(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	start(t, tx, rlsOrgA, rlsSiteA, rlsUserA, inspID(30), nil, "a")

	for _, s := range checklists.Severities {
		if _, err := inSavepoint(context.Background(), tx, false, answerSQL, inspID(30), "a", "fail", s.Key); err != nil {
			t.Errorf("severity %q (checklists.Severities) refused by the database: %v", s.Key, err)
		}
	}
	refused(t, tx, "23514", "", answerSQL, inspID(30), "a", "fail", "severe")

	insertType := `INSERT INTO inspection_responses (org_id, site_id, inspection_id, item_key, position, prompt, response_type)
		VALUES ($1, $2, $3, 'typed', 9, 'Typed?', $4)`
	for _, rt := range checklists.SupportedResponseTypes {
		if _, err := inSavepoint(context.Background(), tx, false, insertType, rlsOrgA, rlsSiteA, inspID(30), rt); err != nil {
			t.Errorf("response type %q (checklists.SupportedResponseTypes) refused by the database: %v", rt, err)
		}
	}
	refused(t, tx, "23514", "", insertType, rlsOrgA, rlsSiteA, inspID(30), "text")
}

// Org C holds committed fixtures for the tests that need two transactions at
// once. Committed rows can't be rolled back, so they live in their own org
// and never touch A's or B's one-row-per-table fixtures.
const (
	orgC  = "20000000-0000-0000-0000-00000000000c"
	userC = "21000000-0000-0000-0000-00000000000c"
	siteC = "22000000-0000-0000-0000-00000000000c"
)

var seedOrgC = sync.OnceValue(func() error {
	ctx := context.Background()
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Locks', 'construction', ARRAY['construction'])`, []any{orgC}},
		{`INSERT INTO users (id, email, full_name) VALUES ($1, 'locks@rls.test', 'Locks user')`, []any{userC}},
		{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{orgC, userC}},
		{`INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Locks site' FROM regulatory_regimes LIMIT 1`, []any{siteC, orgC}},
	} {
		if _, err := admin.Exec(ctx, s.sql, s.args...); err != nil {
			return fmt.Errorf("seed org C %q: %w", s.sql, err)
		}
	}
	return nil
})

// committedInspection starts an inspection in org C and commits it, with
// every item answered as given ("" leaves it unanswered).
func committedInspection(t *testing.T, id string, answers map[string]string, items ...string) {
	t.Helper()
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, startSQL, id, orgC, siteC, hash32(5), len(items), userC, nil); err != nil {
			return err
		}
		for i, it := range items {
			if _, err := tx.Exec(ctx, responseSQL, orgC, siteC, id, it, i+1, "Is "+it+" OK?"); err != nil {
				return err
			}
			if r := answers[it]; r != "" {
				if _, err := tx.Exec(ctx, answerSQL, id, it, r, nil); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("committed inspection: %v", err)
	}
}

// waitingOn waits until another session is blocked on a lock while running a
// statement containing fragment.
func waitingOn(t *testing.T, fragment string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := admin.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE '%' || $1 || '%' AND pid <> pg_backend_pid()`, fragment).Scan(&n)
		if err != nil {
			t.Fatalf("pg_stat_activity: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no session waited on a lock running %q; the guard isn't taking it", fragment)
}

func tenantTx(t *testing.T) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := database.SetTenant(context.Background(), tx, database.Tenant{OrgID: orgC}); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	return tx
}

// An answer that arrives while a completion is open waits for it, and then
// fails: it can't land after completed_at.
func TestAnswerWaitsForCompletion(t *testing.T) {
	ctx := context.Background()
	id := inspID(40)
	committedInspection(t, id, map[string]string{"a": "pass"}, "a")

	completing := tenantTx(t)
	defer completing.Rollback(ctx)
	if _, err := completing.Exec(ctx, completeSQL, id); err != nil {
		t.Fatalf("complete: %v", err)
	}

	answered := make(chan error, 1)
	go func() {
		answered <- database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, answerSQL, id, "a", "na", nil)
			return err
		})
	}()
	waitingOn(t, "UPDATE inspection_responses")

	if err := completing.Commit(ctx); err != nil {
		t.Fatalf("commit completion: %v", err)
	}
	if err := <-answered; pgCode(err) != "23514" || !strings.Contains(err.Error(), "is completed") {
		t.Fatalf("late answer: %v, want SQLSTATE 23514 (inspection is completed)", err)
	}
	var result string
	if err := admin.QueryRow(ctx, `SELECT result FROM inspection_responses WHERE inspection_id = $1`, id).Scan(&result); err != nil || result != "pass" {
		t.Errorf("result after the late answer: %q, err %v; want pass", result, err)
	}
}

// A completion that arrives while an answer is open waits for it, and its
// checks see that answer: here the open answer is the one that makes the
// inspection complete.
func TestCompletionWaitsForAnswer(t *testing.T) {
	ctx := context.Background()
	id := inspID(41)
	committedInspection(t, id, nil, "a")

	answering := tenantTx(t)
	defer answering.Rollback(ctx)
	if _, err := answering.Exec(ctx, answerSQL, id, "a", "pass", nil); err != nil {
		t.Fatalf("answer: %v", err)
	}

	completed := make(chan error, 1)
	go func() {
		completed <- database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, completeSQL, id)
			return err
		})
	}()
	waitingOn(t, "UPDATE inspections SET status")

	if err := answering.Commit(ctx); err != nil {
		t.Fatalf("commit answer: %v", err)
	}
	if err := <-completed; err != nil {
		t.Fatalf("completion after the answer committed: %v, want success (it must have waited and seen the answer)", err)
	}
}

// Many answers racing one completion: no deadlock, every answer either lands
// before the completion or fails cleanly, and only the ones that succeeded
// are stored.
func TestAnswersRacingCompletion(t *testing.T) {
	ctx := context.Background()
	id := inspID(42)
	const n = 20
	items := make([]string, n)
	answers := map[string]string{}
	for i := range items {
		items[i] = fmt.Sprintf("item_%02d", i)
		answers[items[i]] = "pass"
	}
	committedInspection(t, id, answers, items...)

	go1 := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	var completeErr error
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-go1
			errs[i] = database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, answerSQL, id, items[i], "na", nil)
				return err
			})
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-go1
		completeErr = database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, completeSQL, id)
			return err
		})
	}()
	close(go1)
	wg.Wait()

	if completeErr != nil {
		t.Fatalf("completion: %v (every item was already answered, so it must succeed)", completeErr)
	}
	succeeded := 0
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case pgCode(err) == "23514" && strings.Contains(err.Error(), "is completed"):
		default:
			t.Errorf("answer %d: %v, want success or SQLSTATE 23514 (inspection is completed)", i, err)
		}
	}
	var stored int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM inspection_responses WHERE inspection_id = $1 AND result = 'na'`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != succeeded {
		t.Errorf("%d answers stored, %d succeeded; a refused answer must leave nothing behind", stored, succeeded)
	}
	t.Logf("%d of %d answers landed before the completion", succeeded, n)
}
