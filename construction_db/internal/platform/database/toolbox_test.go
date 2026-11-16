package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func talkID(n int) string { return fmt.Sprintf("3c000000-0000-0000-0000-%012d", n) }
func attID(n int) string  { return fmt.Sprintf("3d000000-0000-0000-0000-%012d", n) }

const (
	// $1 id, $2 org, $3 site, $4 topic_key, $5 topic, $6 started_at, $7
	// ended_at, $8 time_zone, $9 recorded_by.
	talkSQL = `
		INSERT INTO toolbox_talks (id, org_id, site_id, topic_key, topic, presenter_name, started_at, ended_at, time_zone, recorded_by)
		VALUES ($1, $2, $3, $4, $5, 'Thabo M', $6, $7, $8, $9)`
	// $1 id, $2 org, $3 site, $4 talk, $5 name, $6 clock_number, $7 method,
	// $8 recorded_by, $9 client_signed_at.
	signSQL = `
		INSERT INTO toolbox_attendance (id, org_id, site_id, talk_id, name, clock_number, method, recorded_by, client_signed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	// $1 org, $2 site, $3 talk, $4 evidence, $5 kind, $6 added_by.
	talkPhotoSQL = `
		INSERT INTO toolbox_talk_evidence (org_id, site_id, talk_id, evidence_id, kind, added_by) VALUES ($1, $2, $3, $4, $5, $6)`
	// $1 attendance, $2 org, $3 site, $4 reason, $5 retracted_by.
	unsignSQL = `
		INSERT INTO toolbox_attendance_retractions (attendance_id, org_id, site_id, reason, retracted_by, client_retracted_at)
		VALUES ($1, $2, $3, $4, $5, now())`
	sast = "Africa/Johannesburg"
)

// A morning on 10 Nov 2026, SAST (UTC+2): the talk starts at 08:30 local and
// people sign at 09:00.
var (
	talkStart = time.Date(2026, 11, 10, 6, 30, 0, 0, time.UTC)
	signTime  = time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
)

// talkA is a talk on site A recorded by by, starting at start.
func talkA(id string, start time.Time, by string) stmt {
	return stmt{talkSQL, []any{id, rlsOrgA, rlsSiteA, "scaffolding", "Scaffolding", start, nil, sast, by}}
}

func signA(id, talk, name string, clock any, method, by string, at time.Time) stmt {
	return stmt{signSQL, []any{id, rlsOrgA, rlsSiteA, talk, name, clock, method, by, at}}
}

func TestToolboxTalkRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)

	t.Run("the server sets its fields and the local date", func(t *testing.T) {
		// 23:30 local is still the 10th; 00:30 local is the 11th.
		okBatch(t, tx, talkA(talkID(1), time.Date(2026, 11, 10, 21, 30, 0, 0, time.UTC), rlsUserA),
			talkA(talkID(2), time.Date(2026, 11, 10, 22, 30, 0, 0, time.UTC), rlsUserA))
		for id, want := range map[string]string{talkID(1): "2026-11-10", talkID(2): "2026-11-11"} {
			var day time.Time
			var fresh bool
			if err := tx.QueryRow(ctx, `SELECT talk_date, created_at > now() - interval '1 minute' AND updated_at IS NULL FROM toolbox_talks WHERE id = $1`, id).Scan(&day, &fresh); err != nil {
				t.Fatal(err)
			}
			if day.Format("2006-01-02") != want || !fresh {
				t.Errorf("%s: talk_date %s, server times set %v; want %s and true", id, day.Format("2006-01-02"), fresh, want)
			}
		}
	})

	t.Run("what a talk may say", func(t *testing.T) {
		row := func(key, topic any, start, end any, tz string) []any {
			return []any{talkID(9), rlsOrgA, rlsSiteA, key, topic, start, end, tz, rlsUserA}
		}
		refused(t, tx, "23514", "toolbox_talks_topic_key_check", talkSQL, row("Scaffolding!", "Scaffolding", talkStart, nil, sast)...)
		refused(t, tx, "23514", "toolbox_talks_topic_check", talkSQL, row(nil, "   ", talkStart, nil, sast)...)
		refused(t, tx, "23514", "toolbox_talks_ends_after_start", talkSQL, row(nil, "Ladders", talkStart, talkStart.Add(-time.Microsecond), sast)...)
		refused(t, tx, "23514", "ended_at is more than 12 hours after started_at", talkSQL, row(nil, "Ladders", talkStart, talkStart.Add(12*time.Hour+time.Microsecond), sast)...)
		refused(t, tx, "23514", "started_at is in the future", talkSQL, row(nil, "Ladders", time.Now().Add(time.Hour), nil, sast)...)
		refused(t, tx, "23514", "ended_at is in the future", talkSQL, row(nil, "Ladders", talkStart, time.Now().Add(time.Hour), sast)...)
		refused(t, tx, "22023", "", talkSQL, row(nil, "Ladders", talkStart, nil, "Mars/Olympus")...)
		must(t, tx, 1, talkSQL, row(nil, "Ladders (free text)", talkStart, talkStart.Add(20*time.Minute), sast)...)
	})

	t.Run("who may record a talk", func(t *testing.T) {
		for _, who := range []string{supOffSite, claims, suspendedSO} {
			refused(t, tx, "23514", "recorded_by can't see its site", talkSQL, talkA(talkID(10), talkStart, who).args...)
		}
		must(t, tx, 1, talkSQL, talkA(talkID(11), talkStart, supOnSite).args...)
		must(t, tx, 1, talkSQL, talkA(talkID(12), talkStart, sheOfficer).args...)
	})

	t.Run("an archived site takes talks from before it was archived", func(t *testing.T) {
		const archived = "22000000-0000-0000-0000-0000000000d1"
		must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name, status, archived_at)
			SELECT $1, $2, id, 'Archived', 'archived', '2026-11-12T00:00:00Z' FROM regulatory_regimes LIMIT 1`, archived, rlsOrgA)
		at := func(day int) []any {
			return []any{talkID(20 + day), rlsOrgA, archived, nil, "Late talk", time.Date(2026, 11, day, 6, 0, 0, 0, time.UTC), nil, sast, rlsUserA}
		}
		must(t, tx, 1, talkSQL, at(11)...)
		refused(t, tx, "23514", "started after its site was archived", talkSQL, at(13)...)
	})
}

func TestToolboxTalkEdits(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	okBatch(t, tx, talkA(talkID(30), talkStart, rlsUserA))

	t.Run("before anyone signs, the talk can change", func(t *testing.T) {
		must(t, tx, 1, `UPDATE toolbox_talks SET topic = 'Scaffolding inspections', notes = 'Bring the tags',
			started_at = started_at - interval '1 day' WHERE id = $1`, talkID(30))
		var day time.Time
		var stamped bool
		if err := tx.QueryRow(ctx, `SELECT talk_date, updated_at IS NOT NULL FROM toolbox_talks WHERE id = $1`, talkID(30)).Scan(&day, &stamped); err != nil {
			t.Fatal(err)
		}
		if day.Format("2006-01-02") != "2026-11-09" || !stamped {
			t.Errorf("talk_date %s, updated_at set %v; want 2026-11-09 and true", day.Format("2006-01-02"), stamped)
		}
		refused(t, tx, "23514", "started_at is in the future", `UPDATE toolbox_talks SET started_at = now() + interval '1 hour' WHERE id = $1`, talkID(30))
		refused(t, tx, "42501", "", `UPDATE toolbox_talks SET recorded_by = recorded_by WHERE id = $1`, talkID(30))
		refused(t, tx, "42501", "", `DELETE FROM toolbox_talks WHERE id = $1`, talkID(30))
		must(t, tx, 1, `UPDATE toolbox_talks SET started_at = $2 WHERE id = $1`, talkID(30), talkStart)
	})

	t.Run("after a signature only the end time can be set, once", func(t *testing.T) {
		okBatch(t, tx, signA(attID(30), talkID(30), "Sipho N", nil, "tap", rlsUserA, signTime))
		for _, set := range []string{"topic = 'Other'", "notes = 'Changed'", "presenter_name = 'Someone else'",
			"topic_key = NULL", "started_at = started_at - interval '1 minute'"} {
			refused(t, tx, "23514", "people have signed it", `UPDATE toolbox_talks SET `+set+` WHERE id = $1`, talkID(30))
		}
		must(t, tx, 1, `UPDATE toolbox_talks SET notes = notes WHERE id = $1`, talkID(30))
		must(t, tx, 1, `UPDATE toolbox_talks SET ended_at = $2 WHERE id = $1`, talkID(30), signTime.Add(10*time.Minute))
		refused(t, tx, "23514", "people have signed it", `UPDATE toolbox_talks SET ended_at = ended_at + interval '1 minute' WHERE id = $1`, talkID(30))
		refused(t, tx, "23514", "people have signed it", `UPDATE toolbox_talks SET ended_at = NULL WHERE id = $1`, talkID(30))
	})

	// The guard holds for every role, superuser included.
	t.Run("as superuser", func(t *testing.T) {
		su := beginAs(t, admin, "")
		must(t, su, 1, talkSQL, talkA(talkID(31), talkStart, rlsUserA).args...)
		for _, set := range []string{"recorded_by = '" + rlsUserB + "'", "site_id = '" + rlsSiteB + "'", "time_zone = 'UTC'",
			"device_id = 'other'", "created_at = now() - interval '1 day'"} {
			refused(t, su, "23514", "only its topic, presenter, notes and times can change", `UPDATE toolbox_talks SET `+set+` WHERE id = $1`, talkID(31))
		}
		refused(t, su, "23514", "talks are never deleted", `DELETE FROM toolbox_talks WHERE id = $1`, talkID(31))
	})
}

func TestToolboxAttendanceRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	okBatch(t, tx, talkA(talkID(40), talkStart, rlsUserA))

	t.Run("a signature", func(t *testing.T) {
		okBatch(t, tx, signA(attID(40), talkID(40), "Thabo M", nil, "tap", rlsUserA, signTime))
		var serverSet bool
		if err := tx.QueryRow(ctx, `SELECT signed_at > now() - interval '1 minute' FROM toolbox_attendance WHERE id = $1`, attID(40)).Scan(&serverSet); err != nil || !serverSet {
			t.Errorf("signed_at set by the server: %v, err %v", serverSet, err)
		}
	})

	t.Run("the same person can't sign twice", func(t *testing.T) {
		refusedBatch(t, tx, "23505", "toolbox_attendance_once", signA(attID(41), talkID(40), "  thabo   M ", nil, "tap", rlsUserA, signTime))
		okBatch(t, tx, signA(attID(42), talkID(40), "Thabo M", "A1", "tap", rlsUserA, signTime))
		refusedBatch(t, tx, "23505", "toolbox_attendance_once", signA(attID(43), talkID(40), "THABO M", " a1", "tap", rlsUserA, signTime))
	})

	t.Run("when a signature can be given", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "signed before the talk started", signA(attID(44), talkID(40), "Early", nil, "tap", rlsUserA, talkStart.Add(-time.Minute)))
		// No end yet: up to 12 hours after the start, to the microsecond.
		refusedBatch(t, tx, "23514", "signed more than 12 hours after the talk started", signA(attID(44), talkID(40), "Late", nil, "tap", rlsUserA, talkStart.Add(12*time.Hour+time.Microsecond)))
		refusedBatch(t, tx, "23514", "client_signed_at is in the future", signA(attID(44), talkID(40), "Future", nil, "tap", rlsUserA, time.Now().Add(time.Hour)))
		okBatch(t, tx, signA(attID(44), talkID(40), "Last minute", nil, "tap", rlsUserA, talkStart.Add(12*time.Hour)))
	})

	t.Run("who may record a signature", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "recorded_by can't see its site", signA(attID(45), talkID(40), "Off site", nil, "tap", supOffSite, signTime))
		okBatch(t, tx, signA(attID(45), talkID(40), "On site", nil, "tap", supOnSite, signTime))
	})

	t.Run("paper attendance needs a photo of the register", func(t *testing.T) {
		okBatch(t, tx, talkA(talkID(41), talkStart, rlsUserA))
		paper := signA(attID(46), talkID(41), "Lerato K", nil, "paper", rlsUserA, signTime)
		refusedBatch(t, tx, "23514", "paper attendance needs a photo of the signed register", paper, stmt{checkNow, nil})
		photo := func(kind string) stmt {
			return stmt{talkPhotoSQL, []any{rlsOrgA, rlsSiteA, talkID(41), rlsEvidenceA, kind, rlsUserA}}
		}
		refusedBatch(t, tx, "23514", "paper attendance needs a photo of the signed register", photo("photo"), paper, stmt{checkNow, nil})
		// Either order in one transaction: the rule is checked at commit.
		okBatch(t, tx, paper, photo("paper_register"), stmt{checkNow, nil})
	})

	t.Run("only verified photos from the talk's site", func(t *testing.T) {
		const pending, otherSite, otherEv = "2d000000-0000-0000-0000-0000000000d1", "22000000-0000-0000-0000-0000000000d2", "2d000000-0000-0000-0000-0000000000d2"
		must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Other' FROM regulatory_regimes LIMIT 1`, otherSite, rlsOrgA)
		must(t, tx, 1, evidenceSQL, pending, rlsOrgA, rlsSiteA, rlsUserA, hash32(12), "tb/pending", nil, "pending")
		must(t, tx, 1, evidenceSQL, otherEv, rlsOrgA, otherSite, rlsUserA, hash32(13), "tb/other", "v1", "verified")
		refused(t, tx, "23503", "", talkPhotoSQL, rlsOrgA, rlsSiteA, talkID(40), pending, "photo", rlsUserA)
		refused(t, tx, "23503", "", talkPhotoSQL, rlsOrgA, rlsSiteA, talkID(40), otherEv, "photo", rlsUserA)
		refused(t, tx, "23514", "toolbox_talk_evidence_kind_check", talkPhotoSQL, rlsOrgA, rlsSiteA, talkID(40), rlsEvidenceA, "selfie", rlsUserA)
		refused(t, tx, "23514", "added_by can't see its site", talkPhotoSQL, rlsOrgA, rlsSiteA, talkID(40), rlsEvidenceA, "photo", supOffSite)
		must(t, tx, 1, talkPhotoSQL, rlsOrgA, rlsSiteA, talkID(40), rlsEvidenceA, "photo", rlsUserA)
	})

	t.Run("retractions", func(t *testing.T) {
		retract := func(att, by, reason string) []any { return []any{att, rlsOrgA, rlsSiteA, reason, by} }
		refused(t, tx, "23514", "only its recorder or a lead on the site may retract it", unsignSQL, retract(attID(40), supOnSite2, "Not here")...)
		refused(t, tx, "23514", "only its recorder or a lead on the site may retract it", unsignSQL, retract(attID(45), supOffSite, "Not here")...)
		refused(t, tx, "23514", "toolbox_attendance_retractions_reason_check", unsignSQL, retract(attID(40), rlsUserA, " ")...)
		refused(t, tx, "23514", "client_retracted_at is in the future",
			`INSERT INTO toolbox_attendance_retractions (attendance_id, org_id, site_id, reason, retracted_by, client_retracted_at)
			 VALUES ($1, $2, $3, 'Wrong talk', $4, now() + interval '1 hour')`, attID(40), rlsOrgA, rlsSiteA, rlsUserA)
		must(t, tx, 1, unsignSQL, retract(attID(40), rlsUserA, "Tapped the wrong name")...)  // the recorder
		must(t, tx, 1, unsignSQL, retract(attID(45), sheOfficer, "Left before the talk")...) // a lead
		refused(t, tx, "23505", "toolbox_attendance_retractions_pkey", unsignSQL, retract(attID(40), rlsUserA, "Again")...)
		// A recorder taken off the site can't retract their own any more.
		okBatch(t, tx, talkA(talkID(44), talkStart.Add(48*time.Hour), rlsUserA),
			signA(attID(50), talkID(44), "Removed recorder's", nil, "tap", supOnSite2, signTime.Add(48*time.Hour)))
		must(t, tx, 1, `DELETE FROM site_members WHERE site_id = $1 AND user_id = $2`, rlsSiteA, supOnSite2)
		refused(t, tx, "23514", "only its recorder or a lead on the site may retract it", unsignSQL, retract(attID(50), supOnSite2, "Mine")...)
	})

	t.Run("the diary's headcount", func(t *testing.T) {
		// Talk 40: Thabo M (retracted), Thabo M/A1, Last minute, On site
		// (retracted). Talk 41: Lerato K. Talk 42 (same day): Thabo M/A1 again
		// and Sipho N. Talk 43 is the next day.
		okBatch(t, tx, talkA(talkID(42), talkStart.Add(time.Hour), rlsUserA),
			signA(attID(47), talkID(42), "thabo m", "A1", "tap", rlsUserA, signTime.Add(time.Hour)),
			signA(attID(48), talkID(42), "Sipho N", nil, "tap", rlsUserA, signTime.Add(time.Hour)),
			talkA(talkID(43), talkStart.Add(24*time.Hour), rlsUserA),
			signA(attID(49), talkID(43), "Next Day", nil, "tap", rlsUserA, signTime.Add(24*time.Hour)))
		var n int
		if err := tx.QueryRow(ctx, `SELECT toolbox_headcount($1, '2026-11-10')`, rlsSiteA).Scan(&n); err != nil || n != 4 {
			t.Errorf("headcount %d, err %v; want 4 (Thabo M/A1, Last minute, Lerato K, Sipho N)", n, err)
		}
	})
}

// Attendance, photos and retractions never change, for any role.
func TestToolboxIsAppendOnly(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	for _, s := range []string{
		`UPDATE toolbox_attendance SET name = 'Changed' WHERE id = $1`,
		`DELETE FROM toolbox_attendance WHERE id = $1`,
	} {
		refused(t, tx, "23514", "toolbox_attendance is append-only", s, rlsAttA)
	}
	refused(t, tx, "23514", "toolbox_attendance_retractions is append-only", `UPDATE toolbox_attendance_retractions SET reason = 'Changed' WHERE attendance_id = $1`, rlsAttA)
	refused(t, tx, "23514", "toolbox_attendance_retractions is append-only", `DELETE FROM toolbox_attendance_retractions WHERE attendance_id = $1`, rlsAttA)
	refused(t, tx, "23514", "toolbox_talk_evidence is append-only", `UPDATE toolbox_talk_evidence SET kind = 'paper_register' WHERE talk_id = $1`, rlsTalkA)
	refused(t, tx, "23514", "toolbox_talk_evidence is append-only", `DELETE FROM toolbox_talk_evidence WHERE talk_id = $1`, rlsTalkA)

	// TRUNCATE ... CASCADE reaches the other tables too, so each table's own
	// trigger is shown with its neighbours' switched off.
	tables := []string{"toolbox_talks", "toolbox_talk_evidence", "toolbox_attendance", "toolbox_attendance_retractions"}
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

// committedTalk commits a talk in org C, started at talkStart, and returns its id.
func committedTalk(t *testing.T) string {
	t.Helper()
	id := fresh()
	committed(t, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, talkSQL, id, orgC, siteC, nil, "Housekeeping", talkStart, nil, sast, userC)
		return err
	})
	return id
}

func signC(ctx context.Context, tx pgx.Tx, id, talk, name string, at time.Time) error {
	_, err := tx.Exec(ctx, signSQL, id, orgC, siteC, talk, name, nil, "tap", userC, at)
	return err
}

// Two signatures of one name at once: the second waits on the unique key,
// then fails.
func TestTwoSignaturesOfOneNameQueue(t *testing.T) {
	ctx := context.Background()
	talk := committedTalk(t)

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := signC(ctx, first, fresh(), talk, "Thabo M", signTime); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	second := make(chan error, 1)
	go func() {
		second <- asC(ctx, func(tx pgx.Tx) error { return signC(ctx, tx, fresh(), talk, "thabo m", signTime) })
	}()
	waitingOn(t, "INSERT INTO toolbox_attendance")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23505" || !strings.Contains(err.Error(), "toolbox_attendance_once") {
		t.Fatalf("second signature: %v, want SQLSTATE 23505 on toolbox_attendance_once", err)
	}
}

// A signature and an edit of the talk take turns on the talk's row, in
// either order, and the second sees the first.
func TestSignatureAndTalkEdit(t *testing.T) {
	ctx := context.Background()

	t.Run("signature first: the edit waits, then is refused", func(t *testing.T) {
		talk := committedTalk(t)
		signing := tenantTx(t)
		defer signing.Rollback(ctx)
		if err := signC(ctx, signing, fresh(), talk, "Sipho N", signTime); err != nil {
			t.Fatalf("sign: %v", err)
		}
		edited := make(chan error, 1)
		go func() {
			edited <- asC(ctx, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE toolbox_talks SET topic = 'Something else' WHERE id = $1`, talk)
				return err
			})
		}()
		waitingOn(t, "UPDATE toolbox_talks")
		if err := signing.Commit(ctx); err != nil {
			t.Fatalf("commit the signature: %v", err)
		}
		if err := <-edited; pgCode(err) != "23514" || !strings.Contains(err.Error(), "people have signed it") {
			t.Fatalf("edit: %v, want SQLSTATE 23514 (people have signed it)", err)
		}
	})

	t.Run("edit first: the signature waits, then checks the edited talk", func(t *testing.T) {
		talk := committedTalk(t)
		editing := tenantTx(t)
		defer editing.Rollback(ctx)
		// The talk now starts after the signature's time.
		if _, err := editing.Exec(ctx, `UPDATE toolbox_talks SET started_at = $2 WHERE id = $1`, talk, signTime.Add(time.Minute)); err != nil {
			t.Fatalf("edit: %v", err)
		}
		signed := make(chan error, 1)
		go func() {
			signed <- asC(ctx, func(tx pgx.Tx) error { return signC(ctx, tx, fresh(), talk, "Sipho N", signTime) })
		}()
		waitingOn(t, "INSERT INTO toolbox_attendance")
		if err := editing.Commit(ctx); err != nil {
			t.Fatalf("commit the edit: %v", err)
		}
		if err := <-signed; pgCode(err) != "23514" || !strings.Contains(err.Error(), "signed before the talk started") {
			t.Fatalf("signature: %v, want SQLSTATE 23514 (signed before the talk started)", err)
		}
	})
}

// Two retractions of one signature: the second waits on the key, then fails.
func TestTwoRetractionsQueue(t *testing.T) {
	ctx := context.Background()
	talk, att := committedTalk(t), fresh()
	committed(t, func(ctx context.Context, tx pgx.Tx) error { return signC(ctx, tx, att, talk, "Lerato K", signTime) })

	retractC := func(tx pgx.Tx, reason string) error {
		_, err := tx.Exec(ctx, unsignSQL, att, orgC, siteC, reason, userC)
		return err
	}
	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := retractC(first, "Wrong name"); err != nil {
		t.Fatalf("first retraction: %v", err)
	}
	second := make(chan error, 1)
	go func() { second <- asC(ctx, func(tx pgx.Tx) error { return retractC(tx, "Not here") }) }()
	waitingOn(t, "INSERT INTO toolbox_attendance_retractions")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23505" || !strings.Contains(err.Error(), "toolbox_attendance_retractions_pkey") {
		t.Fatalf("second retraction: %v, want SQLSTATE 23505 on the primary key", err)
	}
}
