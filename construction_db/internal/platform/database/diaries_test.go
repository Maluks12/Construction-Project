package database_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func revID(n int) string { return fmt.Sprintf("3e000000-0000-0000-0000-%012d", n) }
func addID(n int) string { return fmt.Sprintf("3f000000-0000-0000-0000-%012d", n) }

const (
	// $1 org, $2 site, $3 date, $4 created_by. Two phones may create the
	// same day; the second does nothing.
	diarySQL = `
		INSERT INTO site_diaries (org_id, site_id, diary_date, time_zone, created_by)
		VALUES ($1, $2, $3, 'Africa/Johannesburg', $4) ON CONFLICT (site_id, diary_date) DO NOTHING`
	// $1 id, $2 org, $3 site, $4 date, $5 base, $6 headcount, $7 saved_by,
	// $8 client_saved_at, $9 late_reason, $10 evidence_count.
	revisionSQL = `
		INSERT INTO site_diary_revisions (id, org_id, site_id, diary_date, base_revision_id, weather_conditions, headcount, work_done,
		                                  saved_by, client_saved_at, late_reason, evidence_count)
		VALUES ($1, $2, $3, $4, $5, ARRAY['fine'], $6, 'Slab poured', $7, $8, $9, $10)`
	// $1 org, $2 site, $3 date, $4 revision, $5 evidence.
	revisionPhotoSQL = `
		INSERT INTO site_diary_revision_evidence (org_id, site_id, diary_date, revision_id, evidence_id) VALUES ($1, $2, $3, $4, $5)`
	// $1 id, $2 org, $3 site, $4 date, $5 reason, $6 note, $7 added_by,
	// $8 evidence_count.
	addendumSQL = `
		INSERT INTO site_diary_addenda (id, org_id, site_id, diary_date, reason, note, added_by, client_added_at, evidence_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8)`
	addendumPhotoSQL = `
		INSERT INTO site_diary_addendum_evidence (org_id, site_id, diary_date, addendum_id, evidence_id) VALUES ($1, $2, $3, $4, $5)`
)

// When the seed's late diary was written: after its day, by the test run.
var lateSaveAt = time.Now()

var sastLoc = func() *time.Location {
	loc, err := time.LoadLocation(sast)
	if err != nil {
		panic(err)
	}
	return loc
}()

// localDate is the local date daysAgo days before today (SAST).
func localDate(daysAgo int) string {
	return time.Now().In(sastLoc).AddDate(0, 0, -daysAgo).Format(time.DateOnly)
}

// localAt is hour:min on that local date. Yesterday's diary closed at this
// morning's midnight, so a save from yesterday is always inside the 48-hour
// grace; a day 5 back is always past it.
func localAt(daysAgo, hour, min int) time.Time {
	y, m, d := time.Now().In(sastLoc).AddDate(0, 0, -daysAgo).Date()
	return time.Date(y, m, d, hour, min, 0, 0, sastLoc)
}

func diaryA(date string) stmt { return stmt{diarySQL, []any{rlsOrgA, rlsSiteA, date, rlsUserA}} }

func revA(id, date string, base any, by string, at time.Time, late any) stmt {
	return stmt{revisionSQL, []any{id, rlsOrgA, rlsSiteA, date, base, 12, by, at, late, 0}}
}

type diaryState struct {
	head      *string
	conflicts []string
	late      bool
	count     int
}

func readDiary(t *testing.T, tx pgx.Tx, site, date string) diaryState {
	t.Helper()
	var s diaryState
	err := tx.QueryRow(context.Background(), `
		SELECT head_revision_id::text, conflict_revision_ids::text[], late, revision_count
		FROM site_diaries WHERE site_id = $1 AND diary_date = $2`, site, date).Scan(&s.head, &s.conflicts, &s.late, &s.count)
	if err != nil {
		t.Fatalf("read diary %s: %v", date, err)
	}
	slices.Sort(s.conflicts)
	return s
}

func wantDiary(t *testing.T, tx pgx.Tx, site, date, head string, conflicts []string, late bool, count int) {
	t.Helper()
	got := readDiary(t, tx, site, date)
	conflicts = slices.Sorted(slices.Values(conflicts))
	if got.head == nil || *got.head != head || !slices.Equal(got.conflicts, conflicts) || got.late != late || got.count != count {
		h := "<nil>"
		if got.head != nil {
			h = *got.head
		}
		t.Errorf("diary %s: head %s, conflicts %v, late %v, %d revisions; want %s, %v, %v, %d",
			date, h, got.conflicts, got.late, got.count, head, conflicts, late, count)
	}
}

func TestSiteDiaryRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	yesterday := localDate(1)

	t.Run("a diary is created by its first revision", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "has no revision", diaryA(yesterday), stmt{checkNow, nil})
		okBatch(t, tx, diaryA(yesterday), revA(revID(1), yesterday, nil, rlsUserA, localAt(1, 17, 0), nil), stmt{checkNow, nil})
		var opens, closes time.Time
		var seq, suggested int
		var savedAfter, receivedAfter bool
		if err := tx.QueryRow(ctx, `
			SELECT d.opens_at, d.closes_at, r.seq, r.headcount_suggested, r.saved_after_close, r.received_after_close
			FROM site_diaries d JOIN site_diary_revisions r USING (site_id, diary_date) WHERE r.id = $1`, revID(1)).Scan(
			&opens, &closes, &seq, &suggested, &savedAfter, &receivedAfter); err != nil {
			t.Fatal(err)
		}
		if !opens.Equal(localAt(1, 0, 0)) || !closes.Equal(localAt(0, 0, 0)) || seq != 1 || suggested != 0 || savedAfter || !receivedAfter {
			t.Errorf("opens %s, closes %s, seq %d, suggested %d, saved after %v, received after %v",
				opens, closes, seq, suggested, savedAfter, receivedAfter)
		}
		wantDiary(t, tx, rlsSiteA, yesterday, revID(1), nil, false, 1)
	})

	t.Run("the day must have begun, and saves can't predate it", func(t *testing.T) {
		// Two days on: "tomorrow" is within the 5-minute clock leeway just before midnight.
		later := time.Now().In(sastLoc).AddDate(0, 0, 2).Format(time.DateOnly)
		refusedBatch(t, tx, "23514", "the day hasn't begun", diaryA(later))
		refusedBatch(t, tx, "23514", "saved before the day began", revA(revID(2), yesterday, revID(1), rlsUserA, localAt(2, 23, 0), nil))
		refusedBatch(t, tx, "23514", "client_saved_at is in the future", revA(revID(2), yesterday, revID(1), rlsUserA, time.Now().Add(time.Hour), nil))
	})

	t.Run("what a revision may say", func(t *testing.T) {
		with := func(set string) stmt {
			return stmt{`INSERT INTO site_diary_revisions (id, org_id, site_id, diary_date, base_revision_id, weather_conditions, headcount,
			                                               work_done, sections, saved_by, client_saved_at)
			             VALUES ($1, $2, $3, $4, $5, ` + set + `, $6, $7)`, []any{revID(3), rlsOrgA, rlsSiteA, yesterday, revID(1), rlsUserA, localAt(1, 18, 0)}}
		}
		for set, constraint := range map[string]string{
			`'{}', 12, 'Work', '{}'`:                              "weather_conditions_check",
			`ARRAY['sunny'], 12, 'Work', '{}'`:                    "weather_conditions_check",
			`ARRAY['fine'], -1, 'Work', '{}'`:                     "headcount_check",
			`ARRAY['fine'], 12, '  ', '{}'`:                       "work_done_check",
			`ARRAY['fine'], 12, 'Work', '{"gossip": []}'`:         "sections_check",
			`ARRAY['fine'], 12, 'Work', '[]'`:                     "sections_check",
			`ARRAY['rain', 'wind'], 12, 'Work', '{"labour": []}'`: "",
		} {
			if constraint == "" {
				okBatch(t, tx, with(set))
				continue
			}
			refusedBatch(t, tx, "23514", constraint, with(set))
		}
		refusedBatch(t, tx, "23514", "site_diary_revisions_late_reason", revA(revID(4), yesterday, revID(3), rlsUserA, localAt(1, 19, 0), "Not late"))
	})

	t.Run("who may write a diary", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "saved_by can't see its site", revA(revID(5), yesterday, revID(3), supOffSite, localAt(1, 19, 0), nil))
		refusedBatch(t, tx, "23514", "created_by can't see its site", stmt{diarySQL, []any{rlsOrgA, rlsSiteA, localDate(3), supOffSite}},
			revA(revID(5), localDate(3), nil, rlsUserA, time.Now(), "Late"))
		okBatch(t, tx, revA(revID(5), yesterday, revID(3), supOnSite, localAt(1, 19, 0), nil))
	})

	t.Run("the base is a revision of the same diary", func(t *testing.T) {
		okBatch(t, tx, diaryA(localDate(2)), revA(revID(6), localDate(2), nil, rlsUserA, localAt(2, 9, 0), nil))
		refusedBatch(t, tx, "23503", "site_diary_revisions_base_fkey", revA(revID(7), yesterday, revID(6), rlsUserA, localAt(1, 20, 0), nil))
	})

	t.Run("an archived site takes diaries for days before it was archived", func(t *testing.T) {
		const archived = "22000000-0000-0000-0000-0000000000e1"
		must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name, status, archived_at)
			SELECT $1, $2, id, 'Archived', 'archived', $3 FROM regulatory_regimes LIMIT 1`, archived, rlsOrgA, localAt(3, 12, 0))
		day := func(n int) []stmt {
			return []stmt{{diarySQL, []any{rlsOrgA, archived, localDate(n), rlsUserA}},
				{revisionSQL, []any{revID(10 + n), rlsOrgA, archived, localDate(n), nil, 3, rlsUserA, time.Now(), "Late", 0}}}
		}
		okBatch(t, tx, day(3)...)
		refusedBatch(t, tx, "23514", "the day began after its site was archived", day(2)...)
	})
}

// D6: a save from before midnight counts if it arrives within 48 hours; a
// save after midnight is refused once the diary has a revision; a missed day
// can be written late, once.
func TestSiteDiaryClosing(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	yesterday, twoAgo, old := localDate(1), localDate(2), localDate(5)

	t.Run("a save from before midnight, received after it", func(t *testing.T) {
		okBatch(t, tx, diaryA(yesterday), revA(revID(20), yesterday, nil, rlsUserA, localAt(1, 23, 59), nil))
		wantDiary(t, tx, rlsSiteA, yesterday, revID(20), nil, false, 1)
	})

	t.Run("a save after midnight is refused", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "closed at midnight; add an addendum", revA(revID(21), yesterday, revID(20), rlsUserA, localAt(0, 0, 0), "Forgot the plant"))
	})

	t.Run("a save from before midnight, received after the grace", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "received more than 48 hours after", diaryA(old), revA(revID(22), old, nil, rlsUserA, localAt(5, 17, 0), nil))
	})

	t.Run("a late diary for a missed day: a reason, and only once", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "site_diary_revisions_late_reason", diaryA(old), revA(revID(23), old, nil, rlsUserA, time.Now(), nil))
		okBatch(t, tx, diaryA(old), revA(revID(23), old, nil, rlsUserA, time.Now(), "Phone was stolen"))
		wantDiary(t, tx, rlsSiteA, old, revID(23), nil, true, 1)
		refusedBatch(t, tx, "23514", "closed at midnight; add an addendum", revA(revID(24), old, revID(23), rlsUserA, time.Now(), "Again"))
	})

	t.Run("an on-time save in the grace still lands after a late diary", func(t *testing.T) {
		okBatch(t, tx, diaryA(twoAgo), revA(revID(25), twoAgo, nil, rlsUserA, time.Now(), "Written up the next morning"))
		okBatch(t, tx, revA(revID(26), twoAgo, nil, rlsUserA, localAt(2, 17, 0), nil))
		// The late one is newer, so it stays the head; the conflict can't be
		// cleared after midnight.
		wantDiary(t, tx, rlsSiteA, twoAgo, revID(25), []string{revID(25), revID(26)}, true, 2)
	})
}

// D5, step by step on one diary.
func TestSiteDiaryConflicts(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	day := localDate(1)
	at := func(h int) time.Time { return localAt(1, h, 0) }

	okBatch(t, tx, diaryA(day), revA(revID(30), day, nil, rlsUserA, at(8), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(30), nil, false, 1)

	// Based on the head and newer: the new head.
	okBatch(t, tx, revA(revID(31), day, revID(30), rlsUserA, at(9), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(31), nil, false, 2)

	// Based on an older revision: a conflict with the head it didn't see.
	okBatch(t, tx, revA(revID(32), day, revID(30), rlsUserA, at(10), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(32), []string{revID(31), revID(32)}, false, 3)

	// Based on a revision in the conflict that isn't the newest: it joins.
	okBatch(t, tx, revA(revID(33), day, revID(31), rlsUserA, at(11), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(33), []string{revID(31), revID(32), revID(33)}, false, 4)

	// Older than the head and based on something else: joins, not the head.
	okBatch(t, tx, revA(revID(34), day, revID(30), rlsUserA, at(7), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(33), []string{revID(31), revID(32), revID(33), revID(34)}, false, 5)

	// Based on the newest of the conflict, and newer: merged, cleared.
	okBatch(t, tx, revA(revID(35), day, revID(33), rlsUserA, at(12), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(35), nil, false, 6)

	// Based on the head but older by device time (a skewed clock): kept, and
	// flagged rather than lost.
	okBatch(t, tx, revA(revID(36), day, revID(35), rlsUserA, at(11), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(35), []string{revID(35), revID(36)}, false, 7)

	// A tie on device time isn't newer.
	okBatch(t, tx, revA(revID(37), day, revID(35), rlsUserA, at(12), nil))
	wantDiary(t, tx, rlsSiteA, day, revID(35), []string{revID(35), revID(36), revID(37)}, false, 8)
}

// The diary row only moves on to its revisions' replay, for every role.
func TestSiteDiaryHeaderGuard(t *testing.T) {
	seedRLS(t)
	su := beginAs(t, admin, "")
	day := localDate(1)
	okBatch(t, su, diaryA(day), revA(revID(40), day, nil, rlsUserA, localAt(1, 8, 0), nil), revA(revID(41), day, nil, rlsUserA, localAt(1, 9, 0), nil))
	wantDiary(t, su, rlsSiteA, day, revID(41), []string{revID(40), revID(41)}, false, 2)

	where := ` WHERE site_id = '` + rlsSiteA + `' AND diary_date = '` + day + `'`
	for _, set := range []string{
		`head_revision_id = '` + revID(40) + `'`,
		`conflict_revision_ids = '{}'`,
		`late = true`,
		`revision_count = 3`,
		`revision_count = 3, head_revision_id = '` + revID(40) + `'`,
	} {
		refused(t, su, "23514", "its state must be what its revisions replay to", `UPDATE site_diaries SET `+set+where)
	}
	for _, set := range []string{`closes_at = closes_at + interval '1 day'`, `time_zone = 'UTC'`, `created_by = '` + rlsUserB + `'`} {
		refused(t, su, "23514", "only its revisions move it on", `UPDATE site_diaries SET `+set+where)
	}
	must(t, su, 1, `UPDATE site_diaries SET late = late`+where)
	refused(t, su, "23514", "diaries are never deleted", `DELETE FROM site_diaries`+where)

	// Each revision must take the next number.
	refusedBatch(t, su, "23514", "revision 4 is not the next revision", stmt{checkNow, nil},
		stmt{`ALTER TABLE site_diary_revisions DISABLE TRIGGER site_diary_revisions_insert`, nil},
		stmt{`INSERT INTO site_diary_revisions (id, org_id, site_id, diary_date, seq, weather_conditions, headcount, work_done, saved_by, client_saved_at)
		      VALUES ($1, $2, $3, $4, 4, ARRAY['fine'], 1, 'W', $5, $6)`, []any{revID(42), rlsOrgA, rlsSiteA, day, rlsUserA, localAt(1, 10, 0)}})

	// Each part of the state on its own: a third revision lands with the
	// advance switched off, so the row is one behind, then only the exact
	// replay (r43 based on the head: no conflict) is accepted.
	okBatch(t, su, stmt{checkNow, nil}, stmt{`ALTER TABLE site_diary_revisions DISABLE TRIGGER site_diary_revisions_advance`, nil},
		revA(revID(43), day, revID(41), rlsUserA, localAt(1, 10, 0), nil))
	setN := func(n int, head, conflicts string, late bool) string {
		return fmt.Sprintf(`UPDATE site_diaries SET revision_count = %d, head_revision_id = '%s', conflict_revision_ids = '%s', late = %v`,
			n, head, conflicts, late) + where
	}
	set := func(head, conflicts string, late bool) string { return setN(3, head, conflicts, late) }
	for _, sql := range []string{
		set(revID(41), "{}", false),                            // the wrong head
		set(revID(43), "{"+revID(40)+","+revID(41)+"}", false), // the wrong conflict
		set(revID(43), "{}", true),                             // the wrong late flag
	} {
		refused(t, su, "23514", "its state must be what its revisions replay to", sql)
	}
	must(t, su, 1, set(revID(43), "{}", false))
	// Two more revisions, and the row can't leap to their state in one step.
	// (The second is numbered by hand: with the row held back, the insert
	// trigger would give both seq 4.)
	okBatch(t, su, revA(revID(44), day, revID(43), rlsUserA, localAt(1, 11, 0), nil),
		stmt{checkNow, nil}, stmt{`ALTER TABLE site_diary_revisions DISABLE TRIGGER site_diary_revisions_insert`, nil},
		stmt{`INSERT INTO site_diary_revisions (id, org_id, site_id, diary_date, seq, base_revision_id, weather_conditions, headcount, work_done, saved_by, client_saved_at)
		      VALUES ($1, $2, $3, $4, 5, $5, ARRAY['fine'], 1, 'W', $6, $7)`, []any{revID(45), rlsOrgA, rlsSiteA, day, revID(44), rlsUserA, localAt(1, 12, 0)}})
	refused(t, su, "23514", "its state must be what its revisions replay to", setN(5, revID(45), "{}", false))
}

func TestSiteDiaryPhotos(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	day := localDate(1)
	rev := func(id string, n int) stmt {
		return stmt{revisionSQL, []any{id, rlsOrgA, rlsSiteA, day, nil, 3, rlsUserA, localAt(1, 8, 0), nil, n}}
	}
	photo := func(rev, ev string) stmt { return stmt{revisionPhotoSQL, []any{rlsOrgA, rlsSiteA, day, rev, ev}} }

	refusedBatch(t, tx, "23514", "has 0 photos but evidence_count is 1", diaryA(day), rev(revID(50), 1), stmt{checkNow, nil})
	okBatch(t, tx, diaryA(day), rev(revID(50), 1), photo(revID(50), rlsEvidenceA), stmt{checkNow, nil})
	// A photo added to a committed revision breaks its count.
	const second = "2d000000-0000-0000-0000-0000000000e9"
	must(t, tx, 1, evidenceSQL, second, rlsOrgA, rlsSiteA, rlsUserA, hash32(14), "diary/second", "v1", "verified")
	refusedBatch(t, tx, "23514", "has 2 photos but evidence_count is 1", photo(revID(50), second), stmt{checkNow, nil})

	const pending = "2d000000-0000-0000-0000-0000000000ea"
	must(t, tx, 1, evidenceSQL, pending, rlsOrgA, rlsSiteA, rlsUserA, hash32(15), "diary/pending", nil, "pending")
	refusedBatch(t, tx, "23503", "", stmt{revisionSQL, []any{revID(51), rlsOrgA, rlsSiteA, day, revID(50), 3, rlsUserA, localAt(1, 9, 0), nil, 1}}, photo(revID(51), pending))
}

func TestSiteDiaryAddenda(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	today, yesterday := localDate(0), localDate(1)
	okBatch(t, tx, diaryA(yesterday), revA(revID(60), yesterday, nil, rlsUserA, localAt(1, 17, 0), nil),
		diaryA(today), revA(revID(61), today, nil, rlsUserA, time.Now(), nil))
	add := func(id, date, reason string, note any, by string, photos int) stmt {
		return stmt{addendumSQL, []any{id, rlsOrgA, rlsSiteA, date, reason, note, by, photos}}
	}

	refusedBatch(t, tx, "23514", "is still open; edit it instead", add(addID(1), today, "Forgot", "Crane idle 2h", rlsUserA, 0))
	refusedBatch(t, tx, "23514", "site_diary_addenda_reason_check", add(addID(1), yesterday, " ", "Crane idle 2h", rlsUserA, 0))
	refusedBatch(t, tx, "23514", "site_diary_addenda_adds", add(addID(1), yesterday, "Forgot", nil, rlsUserA, 0))
	refusedBatch(t, tx, "23514", "added_by can't see its site", add(addID(1), yesterday, "Forgot", "Crane idle 2h", supOffSite, 0))
	refusedBatch(t, tx, "23514", "client_added_at is in the future", stmt{`
		INSERT INTO site_diary_addenda (id, org_id, site_id, diary_date, reason, note, added_by, client_added_at)
		VALUES ($1, $2, $3, $4, 'Forgot', 'Crane idle', $5, now() + interval '1 hour')`, []any{addID(1), rlsOrgA, rlsSiteA, yesterday, rlsUserA}})
	refusedBatch(t, tx, "23514", "has 0 photos but evidence_count is 1", add(addID(1), yesterday, "Forgot", nil, rlsUserA, 1), stmt{checkNow, nil})
	okBatch(t, tx, add(addID(1), yesterday, "Forgot the crane", "Crane idle 2h waiting for the drawings", supOnSite, 0),
		add(addID(2), yesterday, "Photo of the gauge", nil, rlsUserA, 1),
		stmt{addendumPhotoSQL, []any{rlsOrgA, rlsSiteA, yesterday, addID(2), rlsEvidenceA}}, stmt{checkNow, nil})
	const another = "2d000000-0000-0000-0000-0000000000eb"
	must(t, tx, 1, evidenceSQL, another, rlsOrgA, rlsSiteA, rlsUserA, hash32(16), "diary/another", "v1", "verified")
	refusedBatch(t, tx, "23514", "has 2 photos but evidence_count is 1", stmt{addendumPhotoSQL, []any{rlsOrgA, rlsSiteA, yesterday, addID(2), another}}, stmt{checkNow, nil})
	// The original is untouched.
	wantDiary(t, tx, rlsSiteA, yesterday, revID(60), nil, false, 1)
}

// Revisions, addenda and their photos never change, for any role.
func TestSiteDiaryIsAppendOnly(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	for table, where := range map[string]string{
		"site_diary_revisions":         "id = '" + rlsRevA + "'",
		"site_diary_revision_evidence": "revision_id = '" + rlsRevA + "'",
		"site_diary_addenda":           "id = '" + rlsAddA + "'",
		"site_diary_addendum_evidence": "addendum_id = '" + rlsAddA + "'",
	} {
		refused(t, tx, "23514", table+" is append-only", `UPDATE `+table+` SET org_id = org_id WHERE `+where)
		refused(t, tx, "23514", table+" is append-only", `DELETE FROM `+table+` WHERE `+where)
	}
	tables := []string{"site_diaries", "site_diary_revisions", "site_diary_revision_evidence", "site_diary_addenda", "site_diary_addendum_evidence"}
	for _, target := range tables {
		var stmts []stmt
		for _, other := range tables {
			if other != target {
				stmts = append(stmts, stmt{`ALTER TABLE ` + other + ` DISABLE TRIGGER ` + other + `_no_truncate`, nil})
			}
		}
		stmts = append(stmts, stmt{`TRUNCATE ` + target + ` CASCADE`, nil})
		refusedBatch(t, tx, "23514", target+" is append-only", stmts...)
	}
}

// The diary's suggested headcount is that day's toolbox attendance when the
// revision arrives.
func TestSiteDiaryHeadcountSuggestion(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	day := localDate(1)
	okBatch(t, tx, stmt{talkSQL, []any{talkID(60), rlsOrgA, rlsSiteA, nil, "Housekeeping", localAt(1, 7, 0), nil, sast, rlsUserA}},
		signA(attID(60), talkID(60), "Sipho N", nil, "tap", rlsUserA, localAt(1, 7, 30)),
		signA(attID(61), talkID(60), "Lerato K", nil, "tap", rlsUserA, localAt(1, 7, 31)),
		diaryA(day), revA(revID(70), day, nil, rlsUserA, localAt(1, 17, 0), nil))
	var entered, suggested int
	if err := tx.QueryRow(context.Background(), `SELECT headcount, headcount_suggested FROM site_diary_revisions WHERE id = $1`, revID(70)).Scan(&entered, &suggested); err != nil {
		t.Fatal(err)
	}
	if entered != 12 || suggested != 2 {
		t.Errorf("headcount %d, suggested %d; want 12 and 2", entered, suggested)
	}
}
