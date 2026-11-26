package database_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/database"
)

// xid reads an xid8 expression as a number (pgx has no xid8 type).
func xid(t *testing.T, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, sql string, args ...any) uint64 {
	t.Helper()
	var s string
	if err := db.QueryRow(context.Background(), "SELECT ("+sql+")::text", args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		t.Fatalf("%s: %q: %v", sql, s, err)
	}
	return n
}

const locationSQL = `INSERT INTO site_locations (id, org_id, site_id, kind, name) VALUES ($1, $2, $3, 'zone', $4)`

func asOrgC(t *testing.T, fn func(ctx context.Context, tx pgx.Tx) uint64) uint64 {
	t.Helper()
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var got uint64
	if err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
		got = fn(ctx, tx)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSyncStamp(t *testing.T) {
	insp, loc := fresh(), fresh()
	var startXid uint64
	startXid = asOrgC(t, func(ctx context.Context, tx pgx.Tx) uint64 {
		if _, err := tx.Exec(ctx, startSQL, insp, orgC, siteC, hash32(5), 1, userC, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, responseSQL, orgC, siteC, insp, "a", 1, "Is a OK?"); err != nil {
			t.Fatal(err)
		}
		return xid(t, tx, "pg_current_xact_id()")
	})
	if got := xid(t, admin, "SELECT sync_xid FROM inspections WHERE id = $1", insp); got != startXid {
		t.Fatalf("insert: sync_xid %d, want the writing transaction %d", got, startXid)
	}

	answerXid := asOrgC(t, func(ctx context.Context, tx pgx.Tx) uint64 {
		if _, err := tx.Exec(ctx, answerSQL, insp, "a", "pass", nil); err != nil {
			t.Fatal(err)
		}
		return xid(t, tx, "pg_current_xact_id()")
	})
	if got := xid(t, admin, "SELECT sync_xid FROM inspection_responses WHERE inspection_id = $1", insp); got != answerXid {
		t.Errorf("update: sync_xid %d, want %d", got, answerXid)
	}
	if got := xid(t, admin, "SELECT sync_xid FROM inspections WHERE id = $1", insp); got != startXid {
		t.Errorf("untouched row moved to %d", got)
	}

	asOrgC(t, func(ctx context.Context, tx pgx.Tx) uint64 {
		if _, err := tx.Exec(ctx, `UPDATE inspection_responses SET comment = comment WHERE inspection_id = $1`, insp); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if got := xid(t, admin, "SELECT sync_xid FROM inspection_responses WHERE inspection_id = $1", insp); got != answerXid {
		t.Errorf("a no-op update moved sync_xid to %d", got)
	}

	locXid := asOrgC(t, func(ctx context.Context, tx pgx.Tx) uint64 {
		if _, err := tx.Exec(ctx, `INSERT INTO site_locations (id, org_id, site_id, kind, name, sync_xid) VALUES ($1, $2, $3, 'zone', $4, '1')`,
			loc, orgC, siteC, "stamp "+loc); err != nil {
			t.Fatal(err)
		}
		return xid(t, tx, "pg_current_xact_id()")
	})
	if got := xid(t, admin, "SELECT sync_xid FROM site_locations WHERE id = $1", loc); got != locXid {
		t.Errorf("a written sync_xid was kept (%d), want the transaction's %d", got, locXid)
	}
}

func TestSyncDeletions(t *testing.T) {
	ctx := context.Background()

	// A photo link can only be deleted while the inspection is in progress.
	open := fresh()
	committedInspection(t, open, map[string]string{"a": "pass"}, "a")
	if _, err := admin.Exec(ctx, evidenceSQL, fresh2(open), orgC, siteC, userC, hash32(9), "del/"+open, "v1", "verified"); err != nil {
		t.Fatal(err)
	}
	asOrgC(t, func(ctx context.Context, tx pgx.Tx) uint64 {
		if _, err := tx.Exec(ctx, attachSQL, open, "a", fresh2(open)); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	delXid := asOrgC(t, func(ctx context.Context, tx pgx.Tx) uint64 {
		tag, err := tx.Exec(ctx, `DELETE FROM inspection_response_evidence WHERE inspection_id = $1`, open)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("delete: %v (%d rows)", err, tag.RowsAffected())
		}
		return xid(t, tx, "pg_current_xact_id()")
	})
	var kind, root string
	if err := admin.QueryRow(ctx, `SELECT kind, root FROM sync_deletions WHERE root = $1`, open).Scan(&kind, &root); err != nil {
		t.Fatalf("no tombstone: %v", err)
	}
	if kind != "inspection" || root != open {
		t.Errorf("tombstone %s %s", kind, root)
	}
	if got := xid(t, admin, "SELECT sync_xid FROM sync_deletions WHERE root = $1", open); got != delXid {
		t.Errorf("tombstone sync_xid %d, want %d", got, delXid)
	}

	for name, sql := range map[string]string{
		"app insert": `INSERT INTO sync_deletions (org_id, site_id, kind, root) VALUES ('` + orgC + `', '` + siteC + `', 'inspection', 'x')`,
		"app update": `UPDATE sync_deletions SET kind = 'x'`,
		"app delete": `DELETE FROM sync_deletions`,
	} {
		err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s: %v, want permission denied", name, err)
		}
	}
	for name, sql := range map[string]string{
		"update": `UPDATE sync_deletions SET kind = 'x' WHERE root = '` + open + `'`,
		"delete": `DELETE FROM sync_deletions WHERE root = '` + open + `'`,
	} {
		if _, err := admin.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("superuser %s: %v, want append-only", name, err)
		}
	}
}

func fresh2(seed string) string { return "2e000000-0000-0000-0000-" + seed[len(seed)-12:] }

// The SYNC D1 property: a pull's upper bound never passes a transaction
// that hasn't finished. A takes an xid and stays open; B commits after it;
// the window stops below A, so B's committed row waits too, and both are
// read once A commits.
func TestSyncWindowWaitsForSlowCommit(t *testing.T) {
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	lo := xid(t, admin, "pg_snapshot_xmin(pg_current_snapshot())")

	slow, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Rollback(ctx)
	if err := database.SetTenant(ctx, slow, database.Tenant{OrgID: orgC}); err != nil {
		t.Fatal(err)
	}
	slowXid := xid(t, slow, "pg_current_xact_id()")

	fast := fresh()
	asOrgC(t, func(ctx context.Context, tx pgx.Tx) uint64 {
		if _, err := tx.Exec(ctx, locationSQL, fast, orgC, siteC, "fast "+fast); err != nil {
			t.Fatal(err)
		}
		return 0
	})

	window := func() (hi uint64, ids []string) {
		err := database.WithTenantSnapshot(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
			hi = xid(t, tx, "pg_snapshot_xmin(pg_current_snapshot())")
			rows, err := tx.Query(ctx, `SELECT id::text FROM site_locations WHERE site_id = $1
				AND sync_xid >= $2::text::xid8 AND sync_xid < $3::text::xid8 ORDER BY id`, siteC, strconv.FormatUint(lo, 10), strconv.FormatUint(hi, 10))
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return hi, ids
	}

	hi, ids := window()
	if hi > slowXid {
		t.Fatalf("hi %d passed the open transaction %d", hi, slowXid)
	}
	for _, id := range ids {
		if id == fast {
			t.Fatalf("the window read %s, committed after an open transaction", fast)
		}
	}

	slowRow := fresh()
	if _, err := slow.Exec(ctx, locationSQL, slowRow, orgC, siteC, "slow "+slowRow); err != nil {
		t.Fatal(err)
	}
	if err := slow.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	_, ids = window()
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[fast] || !got[slowRow] {
		t.Fatalf("after the slow commit the window has %v; want %s and %s", ids, fast, slowRow)
	}
}

func TestSyncEpoch(t *testing.T) {
	ctx := context.Background()
	epoch := func() string {
		var e string
		if err := database.WithTenant(ctx, pool, database.Tenant{}, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT sync_epoch()`).Scan(&e)
		}); err != nil {
			t.Fatalf("app reads the epoch: %v", err)
		}
		return e
	}
	a := epoch()
	if len(a) != 64 || a != epoch() {
		t.Fatalf("epoch %q is not a stable sha256", a)
	}

	for name, sql := range map[string]string{
		"new generation":  `SELECT sync_new_generation()`,
		"read generation": `SELECT generation FROM sync_generation`,
	} {
		err := database.WithTenant(ctx, pool, database.Tenant{}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("app %s: %v, want permission denied", name, err)
		}
	}

	if _, err := admin.Exec(ctx, `SELECT sync_new_generation()`); err != nil {
		t.Fatal(err)
	}
	if b := epoch(); b == a {
		t.Errorf("a new generation kept the epoch %s", a)
	}
}

// 000051: a sealed replay needs its successor, and only a used token has one.
func TestRefreshReplayConstraints(t *testing.T) {
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var session, first, second string
	if err := admin.QueryRow(ctx, `INSERT INTO sessions (user_id, org_id, expires_at) VALUES ($1, $2, now() + interval '1 day') RETURNING id`,
		userC, orgC).Scan(&session); err != nil {
		t.Fatal(err)
	}
	for _, id := range []*string{&first, &second} {
		if err := admin.QueryRow(ctx, `INSERT INTO refresh_tokens (session_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 day') RETURNING id`,
			session, fresh()).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	for name, sql := range map[string]string{
		"a replay with no successor":   `UPDATE refresh_tokens SET replay = '\x00' WHERE id = $1`,
		"a successor of an unused one": `UPDATE refresh_tokens SET replaced_by = $2 WHERE id = $1`,
	} {
		args := []any{first}
		if strings.Contains(sql, "$2") {
			args = append(args, second)
		}
		if _, err := admin.Exec(ctx, sql, args...); err == nil || !strings.Contains(err.Error(), "refresh_tokens_") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := admin.Exec(ctx, `UPDATE refresh_tokens SET used_at = now(), replaced_by = $2, replay = '\x00' WHERE id = $1`, first, second); err != nil {
		t.Errorf("a rotation: %v", err)
	}
}

// The pull reads hi from pg_current_snapshot() in a REPEATABLE READ
// transaction; it must be that transaction's snapshot, the one its queries
// see. A fresh snapshot could pass a commit those queries can't see.
func TestSyncSnapshotIsTheTransactions(t *testing.T) {
	ctx := context.Background()
	err := database.WithTenantSnapshot(ctx, pool, database.Tenant{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1`); err != nil {
			return err
		}
		later, err := admin.Begin(ctx)
		if err != nil {
			return err
		}
		laterXid := xid(t, later, "pg_current_xact_id()")
		if err := later.Commit(ctx); err != nil {
			return err
		}
		if xmax := xid(t, tx, "pg_snapshot_xmax(pg_current_snapshot())"); xmax > laterXid {
			t.Errorf("xmax %d counts a transaction (%d) that committed after this snapshot", xmax, laterXid)
		}
		if xmin := xid(t, tx, "pg_snapshot_xmin(pg_current_snapshot())"); xmin > laterXid {
			t.Errorf("xmin %d passed %d", xmin, laterXid)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
