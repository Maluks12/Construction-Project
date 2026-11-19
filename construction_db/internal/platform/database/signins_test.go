package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func signInID(n int) string { return fmt.Sprintf("45000000-0000-0000-0000-%012d", n) }

const (
	// $1 id, $2 org, $3 site, $4 worker, $5 method, $6 recorded_by, $7 client_signed_in_at.
	signInSQL = `
		INSERT INTO site_sign_ins (id, org_id, site_id, worker_id, name, method, recorded_by, client_signed_in_at, time_zone)
		VALUES ($1, $2, $3, $4, 'as typed', $5, $6, $7, 'Africa/Johannesburg')`
	// $1 sign-in, $2 at, $3 by.
	signOutSQL = `UPDATE site_sign_ins SET client_signed_out_at = $2, sign_out_method = 'tap', signed_out_by = $3 WHERE id = $1`
	// $1 sign-in, $2 by.
	retractSignInSQL = `UPDATE site_sign_ins SET retracted_by = $2, retract_reason = 'Wrong person' WHERE id = $1`
)

// 10 Nov 2026, SAST: 07:00 local is 05:00 UTC.
func localNov10(h, m int) time.Time { return time.Date(2026, 11, 10, h-2, m, 0, 0, time.UTC) }

func signInA(id, worker, method, by string, at time.Time) stmt {
	return stmt{signInSQL, []any{id, rlsOrgA, rlsSiteA, worker, method, by, at}}
}

func headcount(t *testing.T, tx pgx.Tx, day string) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(context.Background(), `SELECT site_headcount($1, $2)`, rlsSiteA, day).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSignInRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	w1, w2 := workerID(201), workerID(202)
	must(t, tx, 1, workerSQL, w1, rlsOrgA, "Thabo Mokoena", "ABC", nil, rlsUserA)
	must(t, tx, 1, workerSQL, w2, rlsOrgA, "Sipho Dlamini", nil, nil, rlsUserA)

	t.Run("the phone's time sets the day; the register sets the name", func(t *testing.T) {
		okBatch(t, tx, signInA(signInID(1), w1, "tap", supOnSite, localNov10(7, 0)))
		var day, name string
		var late bool
		if err := tx.QueryRow(ctx, `SELECT sign_in_date::text, name, received_after_close FROM site_sign_ins WHERE id = $1`, signInID(1)).
			Scan(&day, &name, &late); err != nil {
			t.Fatal(err)
		}
		if day != "2026-11-10" || name != "Thabo Mokoena" || !late {
			t.Errorf("day %s, name %q, received_after_close %v; want 2026-11-10, the register's name, true (days after the grace)", day, name, late)
		}
		okBatch(t, tx, signInA(signInID(2), w2, "tap", supOnSite, localNov10(23, 50)))
		if err := tx.QueryRow(ctx, `SELECT sign_in_date::text FROM site_sign_ins WHERE id = $1`, signInID(2)).Scan(&day); err != nil || day != "2026-11-10" {
			t.Errorf("23:50 local counts on %s", day)
		}
	})

	t.Run("one open sign-in per worker per site", func(t *testing.T) {
		// A second tap while the first stands (no sign-out, within 16 hours).
		refusedBatch(t, tx, "23514", "already signed in at that time", signInA(signInID(3), w1, "tap", supOnSite, localNov10(13, 0)))
		refusedBatch(t, tx, "23514", "already signed in at that time", signInA(signInID(3), w1, "tap", supOnSite, localNov10(7, 0)))
		// An earlier one arriving late doesn't overlap it: allowed.
		okBatch(t, tx, signInA(signInID(4), w1, "tap", supOnSite, localNov10(6, 0)))
		// The night shift's sign-in at 23:50 still stands at 06:00 the next day.
		refusedBatch(t, tx, "23514", "already signed in at that time", signInA(signInID(5), w2, "tap", supOnSite, localNov10(23, 50).Add(6*time.Hour)))
		// After 16 hours without a sign-out it no longer does.
		okBatch(t, tx, signInA(signInID(5), w2, "tap", supOnSite, localNov10(23, 50).Add(17*time.Hour)))
	})

	t.Run("a sign-out is once, after the sign-in, within 16 hours, before a later sign-in", func(t *testing.T) {
		refused(t, tx, "23514", "signed out before signing in", signOutSQL, signInID(1), localNov10(6, 59), supOnSite)
		refused(t, tx, "23514", "signed out more than 16 hours after signing in", signOutSQL, signInID(1), localNov10(7, 0).Add(17*time.Hour), supOnSite)
		// 06:00's sign-in can't end after 07:00's began.
		refused(t, tx, "23514", "signed out after a later sign-in", signOutSQL, signInID(4), localNov10(8, 0), supOnSite)
		must(t, tx, 1, signOutSQL, signInID(4), localNov10(6, 30), supOnSite)
		refused(t, tx, "23514", "already signed out", signOutSQL, signInID(4), localNov10(6, 40), supOnSite)
		must(t, tx, 1, signOutSQL, signInID(1), localNov10(12, 0), supOnSite)
		// Back after lunch: a new sign-in. One inside a closed interval is refused.
		okBatch(t, tx, signInA(signInID(6), w1, "tap", supOnSite, localNov10(13, 0)))
		refusedBatch(t, tx, "23514", "already signed in at that time", signInA(signInID(7), w1, "tap", supOnSite, localNov10(10, 0)))
		refused(t, tx, "23514", "client_signed_out_at is in the future", signOutSQL, signInID(6), time.Now().Add(10*time.Minute), supOnSite)
	})

	t.Run("paper needs that day's sheet photo first", func(t *testing.T) {
		w3 := workerID(203)
		must(t, tx, 1, workerSQL, w3, rlsOrgA, "Paper Person", nil, nil, rlsUserA)
		refusedBatch(t, tx, "23514", "paper sign-in needs a photo of that day's signed sheet", signInA(signInID(8), w3, "paper", supOnSite, localNov10(8, 0)))
		must(t, tx, 1, `INSERT INTO site_sign_in_sheets (org_id, site_id, sheet_date, evidence_id, added_by) VALUES ($1, $2, '2026-11-10', $3, $4)`,
			rlsOrgA, rlsSiteA, rlsEvidenceA, supOnSite)
		okBatch(t, tx, signInA(signInID(8), w3, "paper", supOnSite, localNov10(8, 0)))
		refused(t, tx, "42501", "", `DELETE FROM site_sign_in_sheets WHERE site_id = $1`, rlsSiteA)
	})

	t.Run("a retraction is the recorder's or a lead's, and then counts nowhere", func(t *testing.T) {
		refused(t, tx, "23514", "only the person who recorded it, an owner or a SHE officer can retract it", retractSignInSQL, signInID(6), supOnSite2)
		must(t, tx, 1, retractSignInSQL, signInID(6), supOnSite)
		refused(t, tx, "23514", "retracted; it never changes again", signOutSQL, signInID(6), localNov10(14, 0), supOnSite)
		// The retracted 13:00 no longer stands, so a sign-in then is fine.
		okBatch(t, tx, signInA(signInID(9), w1, "tap", supOnSite, localNov10(13, 5)))
		refused(t, tx, "23514", "already signed out", `UPDATE site_sign_ins SET client_signed_out_at = NULL, sign_out_method = NULL,
			signed_out_by = NULL, sign_out_received_at = NULL WHERE id = $1`, signInID(1))
		refused(t, tx, "42501", "", `UPDATE site_sign_ins SET name = 'x' WHERE id = $1`, signInID(1))
		refused(t, tx, "42501", "", `DELETE FROM site_sign_ins WHERE id = $1`, signInID(1))
	})

	t.Run("the recorder must see the site; archived sites take earlier sign-ins only", func(t *testing.T) {
		w4 := workerID(204)
		must(t, tx, 1, workerSQL, w4, rlsOrgA, "Late Joiner", nil, nil, rlsUserA)
		refusedBatch(t, tx, "23514", "recorded_by can't see its site", signInA(signInID(10), w4, "tap", supOffSite, localNov10(8, 0)))
		okBatch(t, tx, stmt{`UPDATE sites SET status = 'archived', archived_at = $2 WHERE id = $1`, []any{rlsSiteA, localNov10(9, 0)}},
			signInA(signInID(10), w4, "tap", supOnSite, localNov10(8, 0)))
		w5 := workerID(205)
		must(t, tx, 1, workerSQL, w5, rlsOrgA, "After Archive", nil, nil, rlsUserA)
		refusedBatch(t, tx, "23514", "signed in after its site was archived", signInA(signInID(11), w5, "tap", supOnSite, localNov10(9, 30)))
	})

	t.Run("a sign-in puts the worker on the roster, once", func(t *testing.T) {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM site_roster WHERE site_id = $1 AND worker_id = $2`, rlsSiteA, w1).Scan(&n); err != nil || n != 1 {
			t.Errorf("roster rows for w1: %d, err %v", n, err)
		}
		const remove = `UPDATE site_roster SET removed_by = $2, remove_reason = 'Left the job' WHERE worker_id = $1 AND removed_at IS NULL`
		refused(t, tx, "23514", "removed_by can't see its site", remove, w1, supOffSite)
		must(t, tx, 1, remove, w1, supOnSite)
		refused(t, tx, "42501", "", `DELETE FROM site_roster WHERE worker_id = $1`, w1)
	})
}

// Distinct people (D11 headcount): a worker once however often they signed
// in and whether or not they also signed a talk; a talk-only name once; a
// retracted sign-in or signature not at all.
func TestSiteHeadcount(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	const day = "2026-11-10"
	w1, w2, w3 := workerID(301), workerID(302), workerID(303)
	must(t, tx, 1, workerSQL, w1, rlsOrgA, "Thabo Mokoena", nil, nil, rlsUserA)
	must(t, tx, 1, workerSQL, w2, rlsOrgA, "Sipho Dlamini", nil, nil, rlsUserA)
	must(t, tx, 1, workerSQL, w3, rlsOrgA, "Retracted Person", nil, nil, rlsUserA)
	if n := headcount(t, tx, day); n != 0 {
		t.Fatalf("empty day counts %d", n)
	}

	okBatch(t, tx, signInA(signInID(101), w1, "tap", supOnSite, localNov10(7, 0)))
	must(t, tx, 1, signOutSQL, signInID(101), localNov10(12, 0), supOnSite)
	okBatch(t, tx, signInA(signInID(102), w1, "tap", supOnSite, localNov10(13, 0)))
	if n := headcount(t, tx, day); n != 1 {
		t.Errorf("one worker signed in twice counts %d", n)
	}
	// Thabo also signs the talk: still one. Sipho only signs the talk: two.
	// A visitor's name not on the register: three.
	okBatch(t, tx, talkA(talkID(700), localNov10(8, 0), rlsUserA),
		signA(attID(701), talkID(700), "thabo  MOKOENA", nil, "tap", rlsUserA, localNov10(8, 5)),
		signA(attID(702), talkID(700), "Sipho Dlamini", nil, "tap", rlsUserA, localNov10(8, 5)),
		signA(attID(703), talkID(700), "A Visitor", nil, "tap", rlsUserA, localNov10(8, 5)))
	if n := headcount(t, tx, day); n != 3 {
		t.Errorf("headcount %d, want 3 (Thabo once, Sipho, the visitor)", n)
	}
	okBatch(t, tx, signInA(signInID(103), w2, "tap", supOnSite, localNov10(9, 0)))
	if n := headcount(t, tx, day); n != 3 {
		t.Errorf("Sipho signed in and signed the talk: headcount %d, want 3", n)
	}
	// A retracted sign-in counts nowhere.
	okBatch(t, tx, signInA(signInID(104), w3, "tap", supOnSite, localNov10(9, 0)))
	must(t, tx, 1, retractSignInSQL, signInID(104), supOnSite)
	if n := headcount(t, tx, day); n != 3 {
		t.Errorf("a retracted sign-in was counted: %d", n)
	}
	// Unlinked as a different person, the signature is its own key again.
	okBatch(t, tx, stmt{`INSERT INTO toolbox_attendance_workers (org_id, site_id, attendance_id, worker_id, method, linked_by) VALUES ($1, $2, $3, $4, 'auto', $5)`,
		[]any{rlsOrgA, rlsSiteA, attID(702), w2, rlsUserA}}, stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
	must(t, tx, 1, `UPDATE toolbox_attendance_workers SET unlinked_by = $2, unlink_reason = 'Different person' WHERE attendance_id = $1`, attID(702), sheOfficer)
	if n := headcount(t, tx, day); n != 4 {
		t.Errorf("after unlinking the talk's Sipho from the worker: headcount %d, want 4", n)
	}
	// The next day has nobody.
	if n := headcount(t, tx, "2026-11-11"); n != 0 {
		t.Errorf("the next day counts %d", n)
	}
}

// Two phones tap the same worker at once: the second waits on the worker's
// row, then sees the first and is refused. Forced: the first is held open.
func TestSignInDoubleTapRace(t *testing.T) {
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	w := fresh()
	if err := asC(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, workerSQL, w, orgC, "Race Worker "+w[:8], nil, nil, userC)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if _, err := first.Exec(ctx, signInSQL, fresh(), orgC, siteC, w, "tap", userC, at); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- asC(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, signInSQL, fresh(), orgC, siteC, w, "tap", userC, at.Add(time.Second))
			return err
		})
	}()
	waitingOn(t, "INSERT INTO site_sign_ins")
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; pgCode(err) != "23514" {
		t.Fatalf("second tap: %v, want 23514 already signed in", err)
	}
	var n int
	if err := asC(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM site_sign_ins WHERE worker_id = $1`, w).Scan(&n)
	}); err != nil || n != 1 {
		t.Errorf("%d sign-ins stored, err %v; want 1", n, err)
	}
}

func TestSignInTablesRefuseTruncate(t *testing.T) {
	seedRLS(t)
	refuseTruncates(t, beginAs(t, admin, ""), "site_roster", "site_sign_in_sheets", "site_sign_ins")
}

func endsAt(t *testing.T, tx pgx.Tx, id string) time.Time {
	t.Helper()
	var at time.Time
	if err := tx.QueryRow(context.Background(), `SELECT site_sign_in_ends_at(s) FROM site_sign_ins s WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

// Out of order (M11 review): an 08:00 sign-in is recorded first, then a
// 07:00 one from an offline phone arrives. The 07:00 one is accepted and
// ends at 08:00, so the worker is never signed in twice at once.
func TestSignInsOutOfOrder(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	w := workerID(601)
	must(t, tx, 1, workerSQL, w, rlsOrgA, "Early Bird", nil, nil, rlsUserA)
	eight, seven := signInID(601), signInID(602)
	okBatch(t, tx, signInA(eight, w, "tap", supOnSite, localNov10(8, 0)))
	okBatch(t, tx, signInA(seven, w, "tap", supOnSite, localNov10(7, 0)))
	if got := endsAt(t, tx, seven); !got.Equal(localNov10(8, 0)) {
		t.Errorf("the late 07:00 sign-in ends at %s, want 08:00 (the next sign-in)", got)
	}
	if got := endsAt(t, tx, eight); !got.Equal(localNov10(8, 0).Add(16 * time.Hour)) {
		t.Errorf("the 08:00 sign-in ends at %s, want 16 hours on", got)
	}
	// Inside the 07:00 one's presence (07:00–08:00): refused.
	refusedBatch(t, tx, "23514", "already signed in at that time", signInA(signInID(603), w, "tap", supOnSite, localNov10(7, 30)))
	// The 08:00 one signed out at 12:00; back at 13:00 is a new sign-in. (With
	// the late 07:00 standing for 16 hours, 13:00 would be refused.)
	must(t, tx, 1, signOutSQL, eight, localNov10(12, 0), supOnSite)
	okBatch(t, tx, signInA(signInID(604), w, "tap", supOnSite, localNov10(13, 0)))
	// Signing the 07:00 one out before 08:00 works; after it, no.
	refused(t, tx, "23514", "signed out after a later sign-in", signOutSQL, seven, localNov10(8, 30), supOnSite)
	must(t, tx, 1, signOutSQL, seven, localNov10(7, 45), supOnSite)
	if got := endsAt(t, tx, seven); !got.Equal(localNov10(7, 45)) {
		t.Errorf("signed out at 07:45, ends at %s", got)
	}
	// A retracted next sign-in doesn't end an earlier one.
	w2 := workerID(602)
	must(t, tx, 1, workerSQL, w2, rlsOrgA, "Second Worker", nil, nil, rlsUserA)
	okBatch(t, tx, signInA(signInID(605), w2, "tap", supOnSite, localNov10(9, 0)), signInA(signInID(606), w2, "tap", supOnSite, localNov10(6, 0)))
	must(t, tx, 1, retractSignInSQL, signInID(605), supOnSite)
	if got := endsAt(t, tx, signInID(606)); !got.Equal(localNov10(6, 0).Add(16 * time.Hour)) {
		t.Errorf("with the next sign-in retracted, 06:00 ends at %s", got)
	}
	// A sign-in on another site never ends this one: that's an overlap (D14).
	const otherSite = "22000000-0000-0000-0000-0000000000a3"
	must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Other site' FROM regulatory_regimes LIMIT 1`, otherSite, rlsOrgA)
	okBatch(t, tx, stmt{signInSQL, []any{signInID(607), rlsOrgA, otherSite, w, "tap", rlsUserA, localNov10(14, 0)}})
	if got := endsAt(t, tx, signInID(604)); !got.Equal(localNov10(13, 0).Add(16 * time.Hour)) {
		t.Errorf("13:00 here ends at %s after a 14:00 sign-in on another site; want 16 hours on", got)
	}
}

// The same out-of-order pair arriving at once: the 07:00 insert waits on the
// worker's row while the 08:00 one is uncommitted, then is accepted and ends
// at 08:00. Forced: the 08:00 one is held open.
func TestSignInsOutOfOrderRace(t *testing.T) {
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	w := fresh()
	if err := asC(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, workerSQL, w, orgC, "Order Worker "+w[:8], nil, nil, userC)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	eight := time.Now().Add(-time.Hour).Truncate(time.Second)
	held, late := fresh(), fresh()
	first := tenantTx(t)
	defer first.Rollback(ctx)
	if _, err := first.Exec(ctx, signInSQL, held, orgC, siteC, w, "tap", userC, eight); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- asC(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, signInSQL, late, orgC, siteC, w, "tap", userC, eight.Add(-time.Hour))
			return err
		})
	}()
	waitingOn(t, "INSERT INTO site_sign_ins")
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the earlier sign-in arriving late: %v", err)
	}
	var end time.Time
	if err := asC(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT site_sign_in_ends_at(s) FROM site_sign_ins s WHERE id = $1`, late).Scan(&end)
	}); err != nil || !end.Equal(eight) {
		t.Errorf("the late sign-in ends at %s, err %v; want %s", end, err, eight)
	}
}

