package database_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// 000023: the same name is told apart by firm and clock number.
func TestToolboxSameNameWorkers(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	start := time.Date(2026, 11, 13, 6, 0, 0, 0, time.UTC)
	sign := func(id, name string, firm, clock any) stmt {
		return stmt{`INSERT INTO toolbox_attendance (id, org_id, site_id, talk_id, name, firm, clock_number, method, recorded_by, client_signed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'tap', $8, $9)`, []any{id, rlsOrgA, rlsSiteA, talkID(70), name, firm, clock, rlsUserA, start.Add(time.Minute)}}
	}
	okBatch(t, tx, talkA(talkID(70), start, rlsUserA))

	okBatch(t, tx, sign(attID(70), "Thabo M", "Own team", nil), sign(attID(71), "thabo  m", "ABC Scaffolding", nil))
	var n int
	if err := tx.QueryRow(ctx, `SELECT toolbox_headcount($1, '2026-11-13')`, rlsSiteA).Scan(&n); err != nil || n != 2 {
		t.Errorf("headcount %d, err %v; want 2 (two Thabos, two firms)", n, err)
	}
	// Same name and firm, no clock number: case and spacing don't count.
	refusedBatch(t, tx, "23505", "toolbox_attendance_once", sign(attID(72), " THABO\tM ", "own   TEAM", nil))
	// Clock numbers tell them apart.
	okBatch(t, tx, sign(attID(73), "Thabo M", "Own team", "A1"), sign(attID(74), "Thabo M", "Own team", "A2"))
	refusedBatch(t, tx, "23505", "toolbox_attendance_once", sign(attID(75), "thabo m", "own team", " a1 "))
}

// 000023: a talk is signed from its start to its end, or to 12 hours after
// the start without one, across midnight; the diary counts it on the day it
// started.
func TestToolboxSigningWindow(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	// 23:40 SAST on 14 Nov.
	night := time.Date(2026, 11, 14, 21, 40, 0, 0, time.UTC)
	okBatch(t, tx, talkA(talkID(71), night, rlsUserA))

	t.Run("a night shift signs after midnight", func(t *testing.T) {
		okBatch(t, tx, signA(attID(80), talkID(71), "Night worker", nil, "tap", rlsUserA, night.Add(25*time.Minute))) // 00:05 SAST
		for day, want := range map[string]int{"2026-11-14": 1, "2026-11-15": 0} {
			var n int
			if err := tx.QueryRow(ctx, `SELECT toolbox_headcount($1, $2)`, rlsSiteA, day).Scan(&n); err != nil || n != want {
				t.Errorf("headcount on %s: %d, err %v; want %d (the day the talk started)", day, n, err, want)
			}
		}
	})

	t.Run("no end: at most 12 hours after the start", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "signed more than 12 hours after the talk started",
			signA(attID(81), talkID(71), "Too late", nil, "tap", rlsUserA, night.Add(12*time.Hour+time.Microsecond)))
		refusedBatch(t, tx, "23514", "signed before the talk started", signA(attID(81), talkID(71), "Too early", nil, "tap", rlsUserA, night.Add(-time.Microsecond)))
	})

	t.Run("the end time and the signatures", func(t *testing.T) {
		okBatch(t, tx, signA(attID(82), talkID(71), "Second", nil, "tap", rlsUserA, night.Add(time.Hour)))
		// Ending before a signature that stands is refused…
		refused(t, tx, "23514", "ended_at is before a signature", `UPDATE toolbox_talks SET ended_at = $2 WHERE id = $1`, talkID(71), night.Add(50*time.Minute))
		// …but a retracted one doesn't count.
		must(t, tx, 1, unsignSQL, attID(82), rlsOrgA, rlsSiteA, "Signed the wrong talk", rlsUserA)
		must(t, tx, 1, `UPDATE toolbox_talks SET ended_at = $2 WHERE id = $1`, talkID(71), night.Add(50*time.Minute))
		refusedBatch(t, tx, "23514", "signed after the talk ended",
			signA(attID(83), talkID(71), "After the end", nil, "tap", rlsUserA, night.Add(50*time.Minute+time.Microsecond)))
		okBatch(t, tx, signA(attID(83), talkID(71), "At the end", nil, "tap", rlsUserA, night.Add(50*time.Minute)))
	})
}

// An end time and a signature after it, arriving together: they take turns
// on the talk's row, and the second is refused, in either order.
func TestEndTimeAndSignature(t *testing.T) {
	ctx := context.Background()
	setEnd := func(tx pgx.Tx, talk string) error {
		_, err := tx.Exec(ctx, `UPDATE toolbox_talks SET ended_at = $2 WHERE id = $1`, talk, talkStart.Add(20*time.Minute))
		return err
	}
	late := talkStart.Add(30 * time.Minute)

	t.Run("signature first: the end time waits, then is refused", func(t *testing.T) {
		talk := committedTalk(t)
		signing := tenantTx(t)
		defer signing.Rollback(ctx)
		if err := signC(ctx, signing, fresh(), talk, "Sipho N", late); err != nil {
			t.Fatalf("sign: %v", err)
		}
		ended := make(chan error, 1)
		go func() { ended <- asC(ctx, func(tx pgx.Tx) error { return setEnd(tx, talk) }) }()
		waitingOn(t, "UPDATE toolbox_talks")
		if err := signing.Commit(ctx); err != nil {
			t.Fatalf("commit the signature: %v", err)
		}
		if err := <-ended; pgCode(err) != "23514" || !strings.Contains(err.Error(), "ended_at is before a signature") {
			t.Fatalf("end time: %v, want SQLSTATE 23514 (before a signature)", err)
		}
	})

	t.Run("end time first: the signature waits, then is refused", func(t *testing.T) {
		talk := committedTalk(t)
		ending := tenantTx(t)
		defer ending.Rollback(ctx)
		if err := setEnd(ending, talk); err != nil {
			t.Fatalf("set the end: %v", err)
		}
		signed := make(chan error, 1)
		go func() {
			signed <- asC(ctx, func(tx pgx.Tx) error { return signC(ctx, tx, fresh(), talk, "Sipho N", late) })
		}()
		waitingOn(t, "INSERT INTO toolbox_attendance")
		if err := ending.Commit(ctx); err != nil {
			t.Fatalf("commit the end time: %v", err)
		}
		if err := <-signed; pgCode(err) != "23514" || !strings.Contains(err.Error(), "signed after the talk ended") {
			t.Fatalf("signature: %v, want SQLSTATE 23514 (after the talk ended)", err)
		}
	})
}
