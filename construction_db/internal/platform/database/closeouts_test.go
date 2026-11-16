package database_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Close-out and waiver ids used inside rolled-back transactions.
func coID(n int) string { return fmt.Sprintf("37000000-0000-0000-0000-%012d", n) }
func wvID(n int) string { return fmt.Sprintf("38000000-0000-0000-0000-%012d", n) }
func phID(n int) string { return fmt.Sprintf("39000000-0000-0000-0000-%012d", n) }

const (
	// $1 id, $2 org, $3 site, $4 action, $5 submitted_by, $6 evidence_count.
	closeoutSQL = `
		INSERT INTO corrective_action_closeouts (id, org_id, site_id, action_id, note, submitted_by, client_submitted_at, evidence_count)
		VALUES ($1, $2, $3, $4, 'Done', $5, now(), $6)`
	closeoutPhotoSQL = `
		INSERT INTO corrective_action_closeout_evidence (org_id, site_id, closeout_id, evidence_id) VALUES ($1, $2, $3, $4)`
	// eventSQL plus $13 closeout and $14 self_verified.
	closeoutEventSQL = `
		INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, reason,
		                                      status, owner_id, responsible_party, due_on, closeout_id, self_verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8, $9, $10, $11, (now() AT TIME ZONE 'Africa/Johannesburg')::date + $12::int, $13, $14)`
	// $1 id, $2 org, $3 site, $4 inspection, $5 item, $6 severity, $7 waived_by.
	waiverSQL = `
		INSERT INTO finding_waivers (id, org_id, site_id, inspection_id, item_key, severity, reason, waived_by, client_waived_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'Paperwork only', $7, now())`
	retractWaiverSQL = `UPDATE finding_waivers SET retracted_by = $2, retracted_at = now(), retract_reason = $3 WHERE id = $1`
	// A verified photo on a site, captured $5 from now, stored under key $7.
	photoAtSQL = `
		INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at,
		                      object_key, object_version_id, retain_until, status)
		VALUES ($1, $2, $3, $4, $6, 1, 'image/jpeg', now() + $5::interval, $7, 'v1', now() + interval '1 day', 'verified')`
)

// closeoutEvent records event seq of action with a close-out (org A, site A).
func closeoutEvent(action string, seq int, kind, actor string, reason any, s state, closeout, self any) stmt {
	return stmt{closeoutEventSQL, []any{evID(raiseSeq(action) + seq), rlsOrgA, rlsSiteA, action, seq, kind, actor, reason,
		s.status, s.owner, s.party, s.due, closeout, self}}
}

// closeOut builds a close-out by by with photos, and its event.
func closeOut(action string, seq int, co, by string, s state, photos ...string) []stmt {
	out := []stmt{{closeoutSQL, []any{co, rlsOrgA, rlsSiteA, action, by, len(photos)}}}
	for _, p := range photos {
		out = append(out, stmt{closeoutPhotoSQL, []any{rlsOrgA, rlsSiteA, co, p}})
	}
	return append(out, closeoutEvent(action, seq, "closeout_submitted", by, nil, s, co, nil))
}

func addPhoto(t *testing.T, tx pgx.Tx, id, site, captured string) {
	t.Helper()
	must(t, tx, 1, photoAtSQL, id, rlsOrgA, site, rlsUserA, captured, hash32(9), "co/"+id)
}

// actionState reads an action's status, pending close-out and awaiting_since.
func actionState(t *testing.T, tx pgx.Tx, action string) (status string, pending *string, since *time.Time) {
	t.Helper()
	err := tx.QueryRow(context.Background(), `SELECT status, pending_closeout_id::text, awaiting_since FROM corrective_actions WHERE id = $1`,
		action).Scan(&status, &pending, &since)
	if err != nil {
		t.Fatalf("read action: %v", err)
	}
	return
}

// The rules on close-outs and their verification, as the app role.
func TestActionCloseoutRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	const otherSite = "22000000-0000-0000-0000-0000000000c2"
	must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Second site' FROM regulatory_regimes LIMIT 1`, otherSite, rlsOrgA)
	fresh, fresh2, fresh3, old, elsewhere, pending, skewed := phID(1), phID(2), phID(3), phID(4), phID(5), phID(6), phID(7)
	addPhoto(t, tx, fresh, rlsSiteA, "0 minutes")
	addPhoto(t, tx, fresh2, rlsSiteA, "0 minutes")
	addPhoto(t, tx, fresh3, rlsSiteA, "0 minutes")
	addPhoto(t, tx, old, rlsSiteA, "-1 hour") // before the finding was answered
	addPhoto(t, tx, elsewhere, otherSite, "0 minutes")
	addPhoto(t, tx, skewed, rlsSiteA, "-4 minutes") // a little before the source: clock skew, accepted
	must(t, tx, 1, evidenceSQL, pending, rlsOrgA, rlsSiteA, rlsUserA, hash32(10), "co/pending", nil, "pending")

	a := actID(30)
	okBatch(t, tx, raiseOn(a, rlsInspA, "item", "medium", rlsUserA, state{"open", supOnSite, nil, 1}, nil)...)
	open := state{"open", supOnSite, nil, 1}
	awaiting := state{"awaiting_verification", supOnSite, nil, 1}

	t.Run("submitted_at is the server's", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer sp.Rollback(ctx)
		if _, err := sp.Exec(ctx, `
			INSERT INTO corrective_action_closeouts (id, org_id, site_id, action_id, submitted_by, client_submitted_at, submitted_at, evidence_count)
			VALUES ($1, $2, $3, $4, $5, now(), '2000-01-01', 1)`, coID(9), rlsOrgA, rlsSiteA, a, supOnSite); err != nil {
			t.Fatal(err)
		}
		var at time.Time
		if err := sp.QueryRow(ctx, `SELECT submitted_at FROM corrective_action_closeouts WHERE id = $1`, coID(9)).Scan(&at); err != nil || time.Since(at) > time.Minute {
			t.Errorf("submitted_at %v, err %v; want the server's time", at, err)
		}
	})

	t.Run("who closes out", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "only its owner, an owner or a SHE officer can close it out", closeOut(a, 2, coID(1), supOnSite2, awaiting, fresh)...)
	})

	t.Run("the photos must be able to show the fix", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "was taken before the action's source", closeOut(a, 2, coID(1), supOnSite, awaiting, old)...)
		refusedBatch(t, tx, "23514", "is one of the source's own photos", closeOut(a, 2, coID(1), supOnSite, awaiting, rlsEvidenceA)...)
		refusedBatch(t, tx, "23503", "", closeOut(a, 2, coID(1), supOnSite, awaiting, pending)...)
		refusedBatch(t, tx, "23503", "", closeOut(a, 2, coID(1), supOnSite, awaiting, elsewhere)...)
		refusedBatch(t, tx, "23514", "evidence_status_check", stmt{closeoutSQL, []any{coID(1), rlsOrgA, rlsSiteA, a, supOnSite, 1}},
			stmt{`INSERT INTO corrective_action_closeout_evidence (org_id, site_id, closeout_id, evidence_id, evidence_status)
			      VALUES ($1, $2, $3, $4, 'pending')`, []any{rlsOrgA, rlsSiteA, coID(1), pending}})
	})

	t.Run("photos and the event are checked at commit", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "corrective_action_closeouts_evidence_count_check", closeOut(a, 2, coID(1), supOnSite, awaiting)...)
		two := closeOut(a, 2, coID(1), supOnSite, awaiting, fresh)
		two[0].args[5] = 2
		refusedBatch(t, tx, "23514", "has 1 photos but evidence_count is 2", append(two, stmt{checkNow, nil})...)
		refusedBatch(t, tx, "23514", "has no closeout_submitted event",
			stmt{closeoutSQL, []any{coID(1), rlsOrgA, rlsSiteA, a, supOnSite, 1}}, stmt{closeoutPhotoSQL, []any{rlsOrgA, rlsSiteA, coID(1), fresh}}, stmt{checkNow, nil})
	})

	t.Run("the event records its close-out", func(t *testing.T) {
		base := closeOut(a, 2, coID(1), supOnSite, awaiting, fresh)
		refusedBatch(t, tx, "23514", "must record its close-out as submitted",
			base[0], base[1], closeoutEvent(a, 2, "closeout_submitted", rlsUserA, nil, awaiting, coID(1), nil))
		refusedBatch(t, tx, "23514", "changes the status to awaiting_verification, and nothing else",
			base[0], base[1], closeoutEvent(a, 2, "closeout_submitted", supOnSite, nil, open, coID(1), nil))
		refusedBatch(t, tx, "23514", "corrective_action_events_closeout", base[0], base[1], event(a, 2, "closeout_submitted", supOnSite, nil, awaiting))
		refusedBatch(t, tx, "23514", "client_submitted_at is in the future", stmt{`
			INSERT INTO corrective_action_closeouts (id, org_id, site_id, action_id, submitted_by, client_submitted_at, evidence_count)
			VALUES ($1, $2, $3, $4, $5, now() + interval '1 hour', 1)`, []any{coID(1), rlsOrgA, rlsSiteA, a, supOnSite}})
	})

	t.Run("a close-out makes the action await verification", func(t *testing.T) {
		okBatch(t, tx, append(closeOut(a, 2, coID(1), supOnSite, awaiting, fresh, skewed), stmt{checkNow, nil})...)
		status, pendingID, since := actionState(t, tx, a)
		var submitted time.Time
		if err := tx.QueryRow(ctx, `SELECT submitted_at FROM corrective_action_closeouts WHERE id = $1`, coID(1)).Scan(&submitted); err != nil {
			t.Fatal(err)
		}
		if status != "awaiting_verification" || pendingID == nil || *pendingID != coID(1) || since == nil || !since.Equal(submitted) {
			t.Errorf("status %s, pending %v, since %v; want awaiting %s since %v", status, pendingID, since, coID(1), submitted)
		}
		refusedBatch(t, tx, "23514", "a close-out is already awaiting verification", closeOut(a, 3, coID(2), supOnSite, awaiting, fresh2)...)
		refusedBatch(t, tx, "23514", "has 3 photos but evidence_count is 2", stmt{closeoutPhotoSQL, []any{rlsOrgA, rlsSiteA, coID(1), fresh2}}, stmt{checkNow, nil})
	})

	t.Run("a reassignment while awaiting keeps the close-out", func(t *testing.T) {
		okBatch(t, tx, event(a, 3, "reassigned", rlsUserA, nil, state{"awaiting_verification", supOnSite2, nil, 1}))
		awaiting = state{"awaiting_verification", supOnSite2, nil, 1}
		if status, pendingID, since := actionState(t, tx, a); status != "awaiting_verification" || pendingID == nil || *pendingID != coID(1) || since == nil {
			t.Errorf("status %s, pending %v, since %v; want still awaiting %s", status, pendingID, since, coID(1))
		}
	})

	closed := state{"closed", supOnSite2, nil, 1}
	reopened := state{"open", supOnSite2, nil, 1}

	t.Run("verifying and rejecting", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "only an owner or a SHE officer can verify or reject a close-out",
			closeoutEvent(a, 4, "verified", supOnSite2, nil, closed, coID(1), false))
		refusedBatch(t, tx, "23514", "self_verified must say whether the verifier submitted the close-out",
			closeoutEvent(a, 4, "verified", rlsUserA, nil, closed, coID(1), true))
		// Only a verification says whether it was the submitter's own.
		refusedBatch(t, tx, "23514", "corrective_action_events_self_verified", closeoutEvent(a, 4, "rejected", rlsUserA, "No", reopened, coID(1), false))
		refusedBatch(t, tx, "23514", "verifying closes it and rejecting reopens it, and nothing else",
			closeoutEvent(a, 4, "verified", rlsUserA, nil, reopened, coID(1), false))
		// A second close-out of the same action, not the one awaiting.
		other := closeOut(a, 99, coID(3), supOnSite2, awaiting, fresh3)[:2]
		refusedBatch(t, tx, "23514", "only the close-out awaiting verification can be verified or rejected",
			append(other, closeoutEvent(a, 4, "verified", rlsUserA, nil, closed, coID(3), false))...)
		refusedBatch(t, tx, "23514", "corrective_action_events_reason", closeoutEvent(a, 4, "rejected", rlsUserA, nil, reopened, coID(1), nil))

		okBatch(t, tx, closeoutEvent(a, 4, "rejected", sheOfficer, "The guardrail is still loose", reopened, coID(1), nil))
		if status, pendingID, since := actionState(t, tx, a); status != "open" || pendingID != nil || since != nil {
			t.Errorf("after rejecting: status %s, pending %v, since %v; want open, none", status, pendingID, since)
		}
		refusedBatch(t, tx, "23514", "has no close-out awaiting verification", closeoutEvent(a, 5, "verified", rlsUserA, nil, closed, coID(1), false))

		okBatch(t, tx, append(closeOut(a, 5, coID(2), supOnSite2, awaiting, fresh2), stmt{checkNow, nil})...)
		okBatch(t, tx, closeoutEvent(a, 6, "verified", rlsUserA, nil, closed, coID(2), false))
		if status, pendingID, since := actionState(t, tx, a); status != "closed" || pendingID != nil || since != nil {
			t.Errorf("after verifying: status %s, pending %v, since %v; want closed, none", status, pendingID, since)
		}
	})

	t.Run("a closed action changes only by reopening", func(t *testing.T) {
		no := func(msg string, s stmt) { t.Helper(); refusedBatch(t, tx, "23514", msg, s) }
		no("is closed", event(a, 7, "reassigned", rlsUserA, nil, state{"closed", supOnSite, nil, 1}))
		no("is closed", event(a, 7, "cancelled", rlsUserA, "Duplicate", state{"cancelled", supOnSite2, nil, 1}))
		no("is closed", closeoutEvent(a, 7, "verified", rlsUserA, nil, closed, coID(2), false))
		no("only an owner or a SHE officer can reopen it", event(a, 7, "reopened", supOnSite2, "Loose again", reopened))
		no("corrective_action_events_reason", event(a, 7, "reopened", rlsUserA, nil, reopened))
		no("reopening changes the status to open, and nothing else", event(a, 7, "reopened", rlsUserA, "Loose again", state{"open", supOnSite, nil, 1}))
		okBatch(t, tx, event(a, 7, "reopened", rlsUserA, "Loose again", reopened))
		no("only a closed action can be reopened", event(a, 8, "reopened", rlsUserA, "Again", reopened))
	})

	t.Run("a lead may verify their own close-out, marked as such", func(t *testing.T) {
		okBatch(t, tx, append(closeOut(a, 8, coID(4), rlsUserA, state{"awaiting_verification", supOnSite2, nil, 1}, fresh3), stmt{checkNow, nil})...)
		refusedBatch(t, tx, "23514", "self_verified must say", closeoutEvent(a, 9, "verified", rlsUserA, nil, closed, coID(4), false))
		okBatch(t, tx, closeoutEvent(a, 9, "verified", rlsUserA, nil, closed, coID(4), true))
	})

	t.Run("nothing changes as the app", func(t *testing.T) {
		refused(t, tx, "42501", "", `UPDATE corrective_action_closeouts SET note = 'x' WHERE action_id = $1`, a)
		refused(t, tx, "42501", "", `DELETE FROM corrective_action_closeouts WHERE action_id = $1`, a)
		refused(t, tx, "42501", "", `UPDATE corrective_action_closeout_evidence SET evidence_id = evidence_id WHERE closeout_id = $1`, coID(1))
		refused(t, tx, "42501", "", `DELETE FROM corrective_action_closeout_evidence WHERE closeout_id = $1`, coID(1))
		refused(t, tx, "23514", "an action changes only by a new event", `UPDATE corrective_actions SET awaiting_since = now() WHERE id = $1`, a)
	})
}

// completeChain inserts and completes an inspection in org A on site A with item
// "x" answered (a fail gets rlsEvidenceA as its photo).
func completeChain(t *testing.T, tx pgx.Tx, id string, corrects any, result string, sev any) {
	t.Helper()
	must(t, tx, 1, `
		INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
		                         template_title, template_source_ref, item_count, started_by, client_started_at, corrects_id)
		SELECT $1, $2, $3, id, 'chain_check', 1, $5, 'Chain check', 'reg 1', 1, $4, now(), $6 FROM regulatory_regimes LIMIT 1`,
		id, rlsOrgA, rlsSiteA, rlsUserA, hash32(5), corrects)
	must(t, tx, 1, responseSQL, rlsOrgA, rlsSiteA, id, "x", 1, "Is x OK?")
	answer(t, tx, id, "x", result, sev)
	if result == "fail" {
		must(t, tx, 1, attachSQL, id, "x", rlsEvidenceA)
	}
	must(t, tx, 1, completeSQL, id)
}

// coverage reads finding_coverage for (insp, "x"): "" when there's no finding.
func coverage(t *testing.T, tx pgx.Tx, insp string) (string, []string, *string) {
	t.Helper()
	var cov string
	var ids []string
	var waiver *string
	err := tx.QueryRow(context.Background(), `SELECT coverage, action_ids::text[], waiver_id::text FROM finding_coverage($1, 'x')`, insp).Scan(&cov, &ids, &waiver)
	if err == pgx.ErrNoRows {
		return "", nil, nil
	}
	if err != nil {
		t.Fatalf("finding_coverage: %v", err)
	}
	return cov, ids, waiver
}

func review(t *testing.T, tx pgx.Tx, action string) string {
	t.Helper()
	var r *string
	if err := tx.QueryRow(context.Background(), `SELECT action_review($1)`, action).Scan(&r); err != nil {
		t.Fatalf("action_review: %v", err)
	}
	if r == nil {
		return ""
	}
	return *r
}

// Coverage of a finding through its correction chain (D1, D7).
func TestFindingCoverage(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	raise := func(id, insp, sev string, due int) {
		t.Helper()
		okBatch(t, tx, raiseOn(id, insp, "x", sev, rlsUserA, state{"open", supOnSite, nil, due}, nil)...)
	}
	wantCov := func(insp, want string, actions ...string) {
		t.Helper()
		got, ids, _ := coverage(t, tx, insp)
		if got != want || (len(actions) > 0 && !slices.Equal(ids, actions)) {
			t.Errorf("coverage of %s: %s %v, want %s %v", insp[len(insp)-3:], got, ids, want, actions)
		}
	}

	t.Run("unactioned, then actioned", func(t *testing.T) {
		completeChain(t, tx, inspID(201), nil, "fail", "medium")
		wantCov(inspID(201), "unactioned")
		raise(actID(201), inspID(201), "medium", 7)
		wantCov(inspID(201), "actioned", actID(201))
	})

	t.Run("a same or lower severity correction is covered by the old action", func(t *testing.T) {
		completeChain(t, tx, inspID(202), inspID(201), "fail", "medium")
		wantCov(inspID(201), "superseded", actID(201))
		wantCov(inspID(202), "actioned", actID(201))
		completeChain(t, tx, inspID(203), inspID(202), "fail", "low")
		wantCov(inspID(202), "superseded")
		wantCov(inspID(203), "actioned", actID(201))
		if r := review(t, tx, actID(201)); r != "" {
			t.Errorf("review %q, want none: the correction is lower", r)
		}
	})

	t.Run("a higher severity correction is unactioned and flags the old action", func(t *testing.T) {
		completeChain(t, tx, inspID(204), nil, "fail", "medium")
		raise(actID(204), inspID(204), "medium", 7)
		completeChain(t, tx, inspID(205), inspID(204), "fail", "high")
		wantCov(inspID(205), "unactioned")
		if r := review(t, tx, actID(204)); r != "severity_raised" {
			t.Errorf("review %q, want severity_raised", r)
		}
		raise(actID(205), inspID(205), "high", 1)
		wantCov(inspID(205), "actioned", actID(205))
	})

	t.Run("a chain of three: covered, then raised", func(t *testing.T) {
		completeChain(t, tx, inspID(206), nil, "fail", "high")
		raise(actID(206), inspID(206), "high", 1)
		completeChain(t, tx, inspID(207), inspID(206), "fail", "medium")
		wantCov(inspID(207), "actioned", actID(206))
		if r := review(t, tx, actID(206)); r != "" {
			t.Errorf("review %q, want none", r)
		}
		completeChain(t, tx, inspID(208), inspID(207), "fail", "critical")
		wantCov(inspID(208), "unactioned")
		if r := review(t, tx, actID(206)); r != "severity_raised" {
			t.Errorf("review %q, want severity_raised against the newest correction", r)
		}
		// A correction in progress doesn't count.
		must(t, tx, 1, `
			INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
			                         template_title, template_source_ref, item_count, started_by, client_started_at, corrects_id)
			SELECT $1, $2, $3, id, 'chain_check', 1, $5, 'Chain check', 'reg 1', 1, $4, now(), $6 FROM regulatory_regimes LIMIT 1`,
			inspID(209), rlsOrgA, rlsSiteA, rlsUserA, hash32(5), inspID(208))
		if r := review(t, tx, actID(206)); r != "severity_raised" {
			t.Errorf("review %q with a correction in progress, want still severity_raised", r)
		}
		wantCov(inspID(208), "unactioned")
	})

	t.Run("a correction that passes the item flags the old action", func(t *testing.T) {
		completeChain(t, tx, inspID(210), nil, "fail", "medium")
		raise(actID(210), inspID(210), "medium", 7)
		completeChain(t, tx, inspID(211), inspID(210), "pass", nil)
		if got, _, _ := coverage(t, tx, inspID(211)); got != "" {
			t.Errorf("coverage %q for an item with no finding, want no row", got)
		}
		if r := review(t, tx, actID(210)); r != "item_not_failed" {
			t.Errorf("review %q, want item_not_failed", r)
		}
	})

	t.Run("waivers carry over to the same or lower severity only", func(t *testing.T) {
		completeChain(t, tx, inspID(212), nil, "fail", "low")
		must(t, tx, 1, waiverSQL, wvID(212), rlsOrgA, rlsSiteA, inspID(212), "x", "low", rlsUserA)
		wantCov(inspID(212), "waived")
		completeChain(t, tx, inspID(213), inspID(212), "fail", "low")
		wantCov(inspID(213), "waived")
		completeChain(t, tx, inspID(214), inspID(213), "fail", "medium")
		wantCov(inspID(214), "unactioned")
	})

	t.Run("a cancelled action never counts; an action beats a waiver", func(t *testing.T) {
		completeChain(t, tx, inspID(215), nil, "fail", "medium")
		raise(actID(215), inspID(215), "medium", 7)
		okBatch(t, tx, event(actID(215), 2, "cancelled", rlsUserA, "Duplicate", state{"cancelled", supOnSite, nil, 7}))
		wantCov(inspID(215), "unactioned")
		must(t, tx, 1, waiverSQL, wvID(215), rlsOrgA, rlsSiteA, inspID(215), "x", "medium", rlsUserA)
		wantCov(inspID(215), "waived")
		raise(actID(216), inspID(215), "medium", 7)
		wantCov(inspID(215), "actioned", actID(216))
		must(t, tx, 1, retractWaiverSQL, wvID(215), rlsUserA, "Wrong finding")
		wantCov(inspID(215), "actioned", actID(216))
	})

	t.Run("a retracted waiver covers nothing", func(t *testing.T) {
		completeChain(t, tx, inspID(218), nil, "fail", "low")
		must(t, tx, 1, waiverSQL, wvID(218), rlsOrgA, rlsSiteA, inspID(218), "x", "low", rlsUserA)
		must(t, tx, 1, retractWaiverSQL, wvID(218), rlsUserA, "Wrong finding")
		wantCov(inspID(218), "unactioned")
	})

	t.Run("a correction at the same severity needs no review", func(t *testing.T) {
		completeChain(t, tx, inspID(230), nil, "fail", "medium")
		raise(actID(230), inspID(230), "medium", 7)
		completeChain(t, tx, inspID(231), inspID(230), "fail", "medium")
		if r := review(t, tx, actID(230)); r != "" {
			t.Errorf("review %q, want none at the same severity", r)
		}
	})

	t.Run("incident actions have no review", func(t *testing.T) {
		okBatch(t, tx, raiseOnIncident(actID(217), rlsIncA, 1, "low", rlsUserA, state{"open", supOnSite, nil, 1})...)
		if r := review(t, tx, actID(217)); r != "" {
			t.Errorf("review %q, want none", r)
		}
	})
}

// The rules on waivers, as the app role.
func TestWaiverRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	waive := func(id, insp, sev, by string) stmt {
		return stmt{waiverSQL, []any{id, rlsOrgA, rlsSiteA, insp, "x", sev, by}}
	}
	completeChain(t, tx, inspID(220), nil, "fail", "medium")
	completeChain(t, tx, inspID(221), nil, "fail", "high")

	t.Run("only a lead, only on a low or medium finding", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "only an owner or a SHE officer can waive a finding", waive(wvID(1), inspID(220), "medium", supOnSite))
		refusedBatch(t, tx, "23514", "only an owner or a SHE officer can waive a finding", waive(wvID(1), inspID(220), "medium", suspendedSO))
		refusedBatch(t, tx, "23514", "finding_waivers_severity_check", waive(wvID(1), inspID(221), "high", rlsUserA))
		refusedBatch(t, tx, "23503", "", waive(wvID(1), inspID(221), "medium", rlsUserA))
		refusedBatch(t, tx, "23503", "", waive(wvID(1), inspID(220), "low", rlsUserA))
	})

	t.Run("times and state", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "client_waived_at is in the future", stmt{`
			INSERT INTO finding_waivers (id, org_id, site_id, inspection_id, item_key, severity, reason, waived_by, client_waived_at)
			VALUES ($1, $2, $3, $4, 'x', 'medium', 'R', $5, now() + interval '1 hour')`, []any{wvID(1), rlsOrgA, rlsSiteA, inspID(220), rlsUserA}})
		refusedBatch(t, tx, "23514", "a new waiver is not retracted", stmt{`
			INSERT INTO finding_waivers (id, org_id, site_id, inspection_id, item_key, severity, reason, waived_by, client_waived_at,
			                             retracted_by, retracted_at, retract_reason)
			VALUES ($1, $2, $3, $4, 'x', 'medium', 'R', $5, now(), $5, now(), 'R')`, []any{wvID(1), rlsOrgA, rlsSiteA, inspID(220), rlsUserA}})
	})

	t.Run("one live waiver per finding; retracting frees it", func(t *testing.T) {
		okBatch(t, tx, waive(wvID(2), inspID(220), "medium", sheOfficer))
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT waived_at FROM finding_waivers WHERE id = $1`, wvID(2)).Scan(&at); err != nil || time.Since(at) > time.Minute {
			t.Errorf("waived_at %v, err %v; want the server's time", at, err)
		}
		refusedBatch(t, tx, "23505", "finding_waivers_one_live", waive(wvID(3), inspID(220), "medium", rlsUserA))
		refused(t, tx, "23514", "only an owner or a SHE officer can retract a waiver", retractWaiverSQL, wvID(2), supOnSite, "Wrong")
		refused(t, tx, "23514", "finding_waivers_retraction", `UPDATE finding_waivers SET retracted_by = $2 WHERE id = $1`, wvID(2), rlsUserA)
		must(t, tx, 1, `UPDATE finding_waivers SET retracted_by = $2, retracted_at = '2000-01-01', retract_reason = 'Wrong finding' WHERE id = $1`, wvID(2), rlsUserA)
		if err := tx.QueryRow(ctx, `SELECT retracted_at FROM finding_waivers WHERE id = $1`, wvID(2)).Scan(&at); err != nil || time.Since(at) > time.Minute {
			t.Errorf("retracted_at %v, err %v; want the server's time", at, err)
		}
		refused(t, tx, "23514", "already retracted", retractWaiverSQL, wvID(2), rlsUserA, "Again")
		okBatch(t, tx, waive(wvID(3), inspID(220), "medium", rlsUserA))
	})

	t.Run("nothing else changes as the app", func(t *testing.T) {
		refused(t, tx, "42501", "", `UPDATE finding_waivers SET reason = 'x' WHERE id = $1`, wvID(3))
		refused(t, tx, "42501", "", `DELETE FROM finding_waivers WHERE id = $1`, wvID(3))
	})

	t.Run("an archived site takes no waiver", func(t *testing.T) {
		const archived = "22000000-0000-0000-0000-0000000000c3"
		must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name, status, archived_at) SELECT $1, $2, id, 'Old site', 'archived', now() FROM regulatory_regimes LIMIT 1`,
			archived, rlsOrgA)
		refusedBatch(t, tx, "23514", "its site is archived", stmt{waiverSQL, []any{wvID(4), rlsOrgA, archived, inspID(99), "x", "low", rlsUserA}})
	})

	t.Run("a superseded finding takes no waiver", func(t *testing.T) {
		completeChain(t, tx, inspID(222), nil, "fail", "low")
		completeChain(t, tx, inspID(223), inspID(222), "fail", "low")
		refusedBatch(t, tx, "23514", "the finding is superseded by a correction", waive(wvID(5), inspID(222), "low", rlsUserA))
		okBatch(t, tx, waive(wvID(5), inspID(223), "low", rlsUserA))
	})
}

// The guards hold for the superuser too.
func TestCloseoutsAndWaiversFrozenForEveryRole(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	for _, sql := range []string{
		`UPDATE corrective_action_closeouts SET note = 'x' WHERE action_id = $1`,
		`DELETE FROM corrective_action_closeouts WHERE action_id = $1`,
		`UPDATE corrective_action_closeout_evidence SET evidence_id = evidence_id WHERE closeout_id IN (SELECT id FROM corrective_action_closeouts WHERE action_id = $1)`,
		`DELETE FROM corrective_action_closeout_evidence WHERE closeout_id IN (SELECT id FROM corrective_action_closeouts WHERE action_id = $1)`,
		`UPDATE corrective_actions SET pending_closeout_id = NULL, awaiting_since = NULL, status = 'open' WHERE id = $1`,
	} {
		refused(t, tx, "23514", "", sql, rlsActionA)
	}
	for _, sql := range []string{
		`UPDATE finding_waivers SET reason = 'x' WHERE org_id = $1`,
		`UPDATE finding_waivers SET retracted_by = waived_by, retract_reason = 'x', reason = 'y' WHERE org_id = $1`,
		`DELETE FROM finding_waivers WHERE org_id = $1`,
	} {
		refused(t, tx, "23514", "", sql, rlsOrgA)
	}
	refusedBatch(t, tx, "23514", "finding_waivers is append-only; retract a waiver instead", stmt{`TRUNCATE finding_waivers`, nil})
}

// corrective_actions_awaiting on its own: with the guard off (rolled back
// with the test), the header still can't await without a close-out, or
// hold one while open.
func TestAwaitingCheckOnItsOwn(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	must(t, tx, 0, `ALTER TABLE corrective_actions DISABLE TRIGGER corrective_actions_guard`)
	for _, sql := range []string{
		`UPDATE corrective_actions SET status = 'open' WHERE id = $1`,
		`UPDATE corrective_actions SET pending_closeout_id = NULL WHERE id = $1`,
		`UPDATE corrective_actions SET awaiting_since = NULL WHERE id = $1`,
	} {
		refused(t, tx, "23514", "corrective_actions_awaiting", sql, rlsActionA)
	}
	must(t, tx, 1, `UPDATE corrective_actions SET status = 'open', pending_closeout_id = NULL, awaiting_since = NULL WHERE id = $1`, rlsActionA)
}

// committedAction commits, in org C, a finding (high) and an action on it
// owned by userC, and a fresh verified photo for its close-outs.
func committedAction(t *testing.T, insp, action string) (photos [2]string) {
	t.Helper()
	committedFinding(t, insp, "high")
	committed(t, func(ctx context.Context, tx pgx.Tx) error { return raiseC(ctx, tx, action, insp, nil, nil, "high", 1) })
	for i := range photos {
		photos[i] = fmt.Sprintf("39000000-0000-0000-%04d-%012s", i, insp[len(insp)-12:])
		if _, err := admin.Exec(context.Background(), `
			INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at,
			                      object_key, object_version_id, retain_until, status)
			VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', now(), $6, 'v1', now() + interval '1 day', 'verified')`,
			photos[i], orgC, siteC, userC, hash32(11), "co/"+photos[i]); err != nil {
			t.Fatalf("photo: %v", err)
		}
	}
	return photos
}

// closeOutC submits a close-out of action in org C as userC.
func closeOutC(ctx context.Context, tx pgx.Tx, action, co, photo, eventID string, seq int) error {
	for _, s := range []stmt{
		{closeoutSQL, []any{co, orgC, siteC, action, userC, 1}},
		{closeoutPhotoSQL, []any{orgC, siteC, co, photo}},
		{closeoutEventSQL, []any{eventID, orgC, siteC, action, seq, "closeout_submitted", userC, nil, "awaiting_verification", userC, nil, 1, co, nil}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// Two close-outs of one action at once: the second waits for the first,
// then fails on its sequence number.
func TestTwoCloseoutsQueue(t *testing.T) {
	ctx := context.Background()
	insp, a := fresh(), fresh()
	photos := committedAction(t, insp, a)

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := closeOutC(ctx, first, a, fresh(), photos[0], fresh(), 2); err != nil {
		t.Fatalf("first close-out: %v", err)
	}
	second := make(chan error, 1)
	go func() {
		second <- asC(ctx, func(tx pgx.Tx) error { return closeOutC(ctx, tx, a, fresh(), photos[1], fresh(), 2) })
	}()
	waitingOn(t, "INSERT INTO corrective_action_events")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23514" || !strings.Contains(err.Error(), "event 2 is not the next event") {
		t.Fatalf("second close-out: %v, want SQLSTATE 23514 (not the next event)", err)
	}
}

// Verifying and cancelling at once: the second waits, then finds the
// action closed.
func TestVerifyAndCancelQueue(t *testing.T) {
	ctx := context.Background()
	insp, a, co := fresh(), fresh(), fresh()
	photos := committedAction(t, insp, a)
	committed(t, func(ctx context.Context, tx pgx.Tx) error {
		return closeOutC(ctx, tx, a, co, photos[0], fresh(), 2)
	})

	verifying := tenantTx(t)
	defer verifying.Rollback(ctx)
	if _, err := verifying.Exec(ctx, closeoutEventSQL, fresh(), orgC, siteC, a, 3, "verified", userC, nil, "closed", userC, nil, 1, co, true); err != nil {
		t.Fatalf("verify: %v", err)
	}
	cancelled := make(chan error, 1)
	go func() {
		cancelled <- asC(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, eventSQL, fresh(), orgC, siteC, a, 3, "cancelled", userC, "Duplicate", "cancelled", userC, nil, 1)
			return err
		})
	}()
	waitingOn(t, "INSERT INTO corrective_action_events")
	if err := verifying.Commit(ctx); err != nil {
		t.Fatalf("commit the verification: %v", err)
	}
	if err := <-cancelled; pgCode(err) != "23514" || !strings.Contains(err.Error(), "event 3 is not the next event") {
		t.Fatalf("cancel: %v, want SQLSTATE 23514 (not the next event)", err)
	}
}

// Two waivers of one finding at once: the second waits on the unique index,
// then fails.
func TestTwoWaiversQueue(t *testing.T) {
	ctx := context.Background()
	insp := fresh()
	committedFinding(t, insp, "medium")
	waiveC := func(tx pgx.Tx, id string) error {
		_, err := tx.Exec(ctx, waiverSQL, id, orgC, siteC, insp, "a", "medium", userC)
		return err
	}
	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := waiveC(first, fresh()); err != nil {
		t.Fatalf("first waiver: %v", err)
	}
	second := make(chan error, 1)
	go func() { second <- asC(ctx, func(tx pgx.Tx) error { return waiveC(tx, fresh()) }) }()
	waitingOn(t, "INSERT INTO finding_waivers")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23505" || !strings.Contains(err.Error(), "finding_waivers_one_live") {
		t.Fatalf("second waiver: %v, want SQLSTATE 23505 on finding_waivers_one_live", err)
	}
}

// A waiver and the completion of a correction take turns, as a raise does.
func TestWaiverWaitsForCorrection(t *testing.T) {
	ctx := context.Background()
	insp, corr := fresh(), fresh()
	committedFinding(t, insp, "medium")
	committedCorrection(t, corr, insp, "pass", nil)

	completing := tenantTx(t)
	defer completing.Rollback(ctx)
	if _, err := completing.Exec(ctx, completeSQL, corr); err != nil {
		t.Fatalf("complete the correction: %v", err)
	}
	waived := make(chan error, 1)
	go func() {
		waived <- asC(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, waiverSQL, fresh(), orgC, siteC, insp, "a", "medium", userC)
			return err
		})
	}()
	waitingOn(t, "INSERT INTO finding_waivers")
	if err := completing.Commit(ctx); err != nil {
		t.Fatalf("commit the completion: %v", err)
	}
	if err := <-waived; pgCode(err) != "23514" || !strings.Contains(err.Error(), "superseded by a correction") {
		t.Fatalf("waiver: %v, want SQLSTATE 23514 (superseded)", err)
	}
}
