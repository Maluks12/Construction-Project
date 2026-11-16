package database_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// freshSiteC commits a new site in org C. Each race gets its own, so the
// days in the grace (yesterday, the day before) are free on every run.
func freshSiteC(t *testing.T) string {
	t.Helper()
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	site := fresh()
	if _, err := admin.Exec(context.Background(), `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Diary race' FROM regulatory_regimes LIMIT 1`,
		site, orgC); err != nil {
		t.Fatal(err)
	}
	return site
}

// diaryC writes a revision of org C's diary on site for date, creating the
// diary if it doesn't exist yet (as the API does).
func diaryC(ctx context.Context, tx pgx.Tx, site, date, id string, base any, at time.Time, late any) error {
	if _, err := tx.Exec(ctx, diarySQL, orgC, site, date, userC); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, revisionSQL, id, orgC, site, date, base, 4, userC, at, late, 0)
	return err
}

func wantDiaryC(t *testing.T, site, date, head string, conflicts []string, late bool, count int) {
	t.Helper()
	if err := asC(context.Background(), func(tx pgx.Tx) error {
		wantDiary(t, tx, site, date, head, conflicts, late, count)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Two revisions on one base at once: the second waits on the diary row, is
// numbered after the first, and is flagged as a conflict. The newer by device
// time is the head, whichever arrived first.
func TestTwoRevisionsOnOneBase(t *testing.T) {
	ctx := context.Background()
	for _, firstNewer := range []bool{true, false} {
		name := "first newer"
		if !firstNewer {
			name = "second newer"
		}
		t.Run(name, func(t *testing.T) {
			site, date := freshSiteC(t), localDate(1)
			base, a, b := fresh(), fresh(), fresh()
			committed(t, func(ctx context.Context, tx pgx.Tx) error {
				return diaryC(ctx, tx, site, date, base, nil, localAt(1, 8, 0), nil)
			})
			atA, atB := localAt(1, 10, 0), localAt(1, 9, 0)
			if !firstNewer {
				atA, atB = atB, atA
			}

			first := tenantTx(t)
			defer first.Rollback(ctx)
			if err := diaryC(ctx, first, site, date, a, base, atA, nil); err != nil {
				t.Fatalf("first revision: %v", err)
			}
			// The revision alone (the diary exists), so it meets the revision
			// trigger's lock on the diary row. Through diaryC it would wait
			// earlier, in the diary's ON CONFLICT, on the first's update.
			second := make(chan error, 1)
			go func() {
				second <- asC(ctx, func(tx pgx.Tx) error {
					_, err := tx.Exec(ctx, revisionSQL, b, orgC, site, date, base, 4, userC, atB, nil, 0)
					return err
				})
			}()
			waitingOn(t, "INSERT INTO site_diary_revisions")
			if err := first.Commit(ctx); err != nil {
				t.Fatalf("commit first: %v", err)
			}
			if err := <-second; err != nil {
				t.Fatalf("second revision: %v", err)
			}
			head := a
			if !firstNewer {
				head = b
			}
			wantDiaryC(t, site, date, head, []string{a, b}, false, 3)
			var seqs []int
			rows, err := admin.Query(ctx, `SELECT seq FROM site_diary_revisions WHERE id = ANY($1::uuid[]) ORDER BY id = $2 DESC`, []string{a, b}, a)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var n int
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
				seqs = append(seqs, n)
			}
			if !slices.Equal(seqs, []int{2, 3}) {
				t.Errorf("seqs (first, second) %v, want [2 3]", seqs)
			}
		})
	}
}

// Two phones create the same day at once: the second waits on the diary's
// key, then adds its revision to the first's diary, as a conflict.
func TestTwoPhonesCreateTheDay(t *testing.T) {
	ctx := context.Background()
	site, date := freshSiteC(t), localDate(1)
	a, b := fresh(), fresh()

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := diaryC(ctx, first, site, date, a, nil, localAt(1, 9, 0), nil); err != nil {
		t.Fatalf("first phone: %v", err)
	}
	second := make(chan error, 1)
	go func() {
		second <- asC(ctx, func(tx pgx.Tx) error { return diaryC(ctx, tx, site, date, b, nil, localAt(1, 10, 0), nil) })
	}()
	waitingOn(t, "INSERT INTO site_diaries")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second phone: %v", err)
	}
	wantDiaryC(t, site, date, b, []string{a, b}, false, 2)
}

// A late diary and an on-time save of the same day arriving together. The
// on-time save lands whichever is first; a late save lands only if it's the
// first revision.
func TestLateDiaryAndOnTimeSave(t *testing.T) {
	ctx := context.Background()

	t.Run("late first: the on-time save waits, then joins", func(t *testing.T) {
		site, date := freshSiteC(t), localDate(2)
		late, onTime := fresh(), fresh()
		first := tenantTx(t)
		defer first.Rollback(ctx)
		if err := diaryC(ctx, first, site, date, late, nil, time.Now(), "Written up the next morning"); err != nil {
			t.Fatalf("late diary: %v", err)
		}
		second := make(chan error, 1)
		go func() {
			second <- asC(ctx, func(tx pgx.Tx) error { return diaryC(ctx, tx, site, date, onTime, nil, localAt(2, 17, 0), nil) })
		}()
		waitingOn(t, "INSERT INTO site_diaries")
		if err := first.Commit(ctx); err != nil {
			t.Fatalf("commit the late diary: %v", err)
		}
		if err := <-second; err != nil {
			t.Fatalf("on-time save: %v", err)
		}
		wantDiaryC(t, site, date, late, []string{late, onTime}, true, 2)
	})

	t.Run("on-time first: the late save waits, then is refused", func(t *testing.T) {
		site, date := freshSiteC(t), localDate(2)
		onTime := fresh()
		first := tenantTx(t)
		defer first.Rollback(ctx)
		if err := diaryC(ctx, first, site, date, onTime, nil, localAt(2, 17, 0), nil); err != nil {
			t.Fatalf("on-time save: %v", err)
		}
		second := make(chan error, 1)
		go func() {
			second <- asC(ctx, func(tx pgx.Tx) error { return diaryC(ctx, tx, site, date, fresh(), nil, time.Now(), "Late") })
		}()
		waitingOn(t, "INSERT INTO site_diaries")
		if err := first.Commit(ctx); err != nil {
			t.Fatalf("commit the on-time save: %v", err)
		}
		if err := <-second; pgCode(err) != "23514" || !strings.Contains(err.Error(), "closed at midnight; add an addendum") {
			t.Fatalf("late save: %v, want SQLSTATE 23514 (closed at midnight)", err)
		}
		wantDiaryC(t, site, date, onTime, nil, false, 1)
	})
}
