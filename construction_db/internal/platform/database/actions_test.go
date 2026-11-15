package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/database"
)

// Action and event ids used inside rolled-back transactions; "35"/"36" so
// they never collide with the RLS fixtures' "33"/"34".
func actID(n int) string { return fmt.Sprintf("35000000-0000-0000-0000-%012d", n) }
func evID(n int) string  { return fmt.Sprintf("36000000-0000-0000-0000-%012d", n) }

const (
	// $1 id, $2 org, $3 site, $4 inspection, $5 item, $6 incident, $7
	// version, $8 severity, $9 raiser, $10 owner, $11 responsible party, $12
	// due in days from today (local).
	actionSQL = `
		INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, incident_id, incident_version, severity, title,
		                                raised_by, client_raised_at, time_zone, owner_id, responsible_party, due_on)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'Fix it', $9, now(), 'Africa/Johannesburg', $10, $11,
		        (now() AT TIME ZONE 'Africa/Johannesburg')::date + $12::int)`
	// $1 id, $2 org, $3 site, $4 action, $5 seq, $6 kind, $7 actor, $8
	// reason, then the state after: $9 status, $10 owner, $11 responsible
	// party, $12 due in days from today (local).
	eventSQL = `
		INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, reason,
		                                      status, owner_id, responsible_party, due_on)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8, $9, $10, $11, (now() AT TIME ZONE 'Africa/Johannesburg')::date + $12::int)`
)

// state is an action's state as an event records it.
type state struct {
	status, owner string
	party         any
	due           int
}

// raiseOn builds the two statements that raise an action on a finding of org
// A, site A: the header and its raise.
func raiseOn(id, insp, item, sev, by string, s state, reason any) []stmt {
	return []stmt{
		{actionSQL, []any{id, rlsOrgA, rlsSiteA, insp, item, nil, nil, sev, by, s.owner, s.party, s.due}},
		{eventSQL, []any{evID(raiseSeq(id)), rlsOrgA, rlsSiteA, id, 1, "raised", by, reason, "open", s.owner, s.party, s.due}},
	}
}

// raiseOnIncident is raiseOn for an incident version.
func raiseOnIncident(id, inc string, version int, sev, by string, s state) []stmt {
	return []stmt{
		{actionSQL, []any{id, rlsOrgA, rlsSiteA, nil, nil, inc, version, sev, by, s.owner, s.party, s.due}},
		{eventSQL, []any{evID(raiseSeq(id)), rlsOrgA, rlsSiteA, id, 1, "raised", by, nil, "open", s.owner, s.party, s.due}},
	}
}

// raiseSeq gives each action's raise its own event id: the action's number
// times 100.
func raiseSeq(action string) int {
	var n int
	fmt.Sscanf(action[len(action)-12:], "%d", &n)
	return n * 100
}

// event records event seq of action (org A, site A); its id is the action's
// number times 100 plus seq.
func event(action string, seq int, kind, actor string, reason any, s state) stmt {
	return stmt{eventSQL, []any{evID(raiseSeq(action) + seq), rlsOrgA, rlsSiteA, action, seq, kind, actor, reason, s.status, s.owner, s.party, s.due}}
}

// addMember adds a user to org A with a role (and status), optionally on site A.
func addMember(t *testing.T, tx pgx.Tx, id, role, status string, onSiteA bool) {
	t.Helper()
	must(t, tx, 1, `INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'Member')`, id, id+"@actions.test")
	must(t, tx, 1, `INSERT INTO org_members (org_id, user_id, role, status) VALUES ($1, $2, $3, $4)`, rlsOrgA, id, role, status)
	if onSiteA {
		must(t, tx, 1, `INSERT INTO site_members (org_id, site_id, user_id) VALUES ($1, $2, $3)`, rlsOrgA, rlsSiteA, id)
	}
}

const (
	supOnSite   = "21000000-0000-0000-0000-0000000000c1" // supervisor added to site A
	supOffSite  = "21000000-0000-0000-0000-0000000000c2" // supervisor not on site A
	supOnSite2  = "21000000-0000-0000-0000-0000000000c3" // another supervisor on site A
	claims      = "21000000-0000-0000-0000-0000000000c4"
	sheOfficer  = "21000000-0000-0000-0000-0000000000c5"
	suspendedSO = "21000000-0000-0000-0000-0000000000c6"
)

func addActionMembers(t *testing.T, tx pgx.Tx) {
	t.Helper()
	addMember(t, tx, supOnSite, "supervisor", "active", true)
	addMember(t, tx, supOffSite, "supervisor", "active", false)
	addMember(t, tx, supOnSite2, "supervisor", "active", true)
	addMember(t, tx, claims, "claims_handler", "active", false)
	addMember(t, tx, sheOfficer, "she_officer", "active", false)
	addMember(t, tx, suspendedSO, "she_officer", "suspended", false)
}

// The rules the database enforces on raising an action, as the app role.
func TestActionRaiseRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	open := func(owner string, due int) state { return state{"open", owner, nil, due} }

	t.Run("an action is created with its raise", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "has no raise", raiseOn(actID(1), rlsInspA, "item", "medium", supOnSite, open(supOnSite, 1), nil)[0], stmt{checkNow, nil})
		refusedBatch(t, tx, "23514", "a new action is open", stmt{`
			INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, severity, title, raised_by, client_raised_at,
			                                time_zone, owner_id, due_on, status)
			VALUES ($1, $2, $3, $4, 'item', 'high', 'Fix', $5, now(), 'Africa/Johannesburg', $5, current_date + 1, 'closed')`,
			[]any{actID(1), rlsOrgA, rlsSiteA, rlsInspA, supOnSite}})
		okBatch(t, tx, append(raiseOn(actID(2), rlsInspA, "item", "medium", supOnSite, open(supOnSite, 1), nil), stmt{checkNow, nil})...)
	})

	t.Run("the server sets its times, its source time and its due values", func(t *testing.T) {
		var stale, wrong int
		err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE a.raised_at < now() - interval '1 minute' OR e.recorded_at < now() - interval '1 minute'),
			       count(*) FILTER (WHERE a.source_at <> r.client_answered_at
			                           OR a.default_due_on <> (a.client_raised_at AT TIME ZONE 'Africa/Johannesburg')::date + 7
			                           OR a.due_at <> ((a.due_on + 1)::timestamp AT TIME ZONE 'Africa/Johannesburg'))
			FROM corrective_actions a
			JOIN corrective_action_events e ON e.action_id = a.id AND e.seq = 1
			JOIN inspection_responses r ON r.inspection_id = a.inspection_id AND r.item_key = a.item_key
			WHERE a.id = $1`, actID(2)).Scan(&stale, &wrong)
		if err != nil || stale != 0 || wrong != 0 {
			t.Errorf("%d rows kept a client time, %d have wrong derived values, err %v; want 0, 0", stale, wrong, err)
		}
		okBatch(t, tx, stmt{`
			INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, severity, title, raised_by, client_raised_at,
			                                raised_at, time_zone, owner_id, due_on, due_at, source_at, default_due_on)
			VALUES ($1, $2, $3, $4, 'item', 'medium', 'Fix', $5, now(), '2000-01-01', 'Africa/Johannesburg', $5,
			        (now() AT TIME ZONE 'Africa/Johannesburg')::date, '2000-01-01', '2000-01-01', '2000-01-01')`,
			[]any{actID(3), rlsOrgA, rlsSiteA, rlsInspA, supOnSite}},
			event(actID(3), 1, "raised", supOnSite, nil, open(supOnSite, 0)))
		var forged int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM corrective_actions
			WHERE id = $1 AND (raised_at < '2001-01-01' OR due_at < '2001-01-01' OR source_at < '2001-01-01' OR default_due_on < '2001-01-01')`,
			actID(3)).Scan(&forged)
		if err != nil || forged != 0 {
			t.Errorf("the client's derived values were kept (%d rows, err %v)", forged, err)
		}
	})

	t.Run("the header computes due_at itself, before its raise", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer sp.Rollback(ctx)
		if _, err := sp.Exec(ctx, `
			INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, severity, title, raised_by, client_raised_at,
			                                time_zone, owner_id, due_on, due_at)
			VALUES ($1, $2, $3, $4, 'item', 'medium', 'Fix', $5, now(), 'Africa/Johannesburg', $5, current_date + 1, '2000-01-01')`,
			actID(15), rlsOrgA, rlsSiteA, rlsInspA, supOnSite); err != nil {
			t.Fatal(err)
		}
		var ok bool
		if err := sp.QueryRow(ctx, `SELECT due_at = action_due_at(due_on, time_zone) FROM corrective_actions WHERE id = $1`, actID(15)).Scan(&ok); err != nil || !ok {
			t.Errorf("due_at right %v, err %v; want it computed from due_on", ok, err)
		}
	})

	t.Run("exactly one source", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "corrective_actions_one_source", stmt{actionSQL,
			[]any{actID(4), rlsOrgA, rlsSiteA, nil, nil, nil, nil, "high", supOnSite, supOnSite, nil, 1}})
		refusedBatch(t, tx, "23514", "corrective_actions_one_source", stmt{actionSQL,
			[]any{actID(4), rlsOrgA, rlsSiteA, rlsInspA, "item", rlsIncA, 1, "high", supOnSite, supOnSite, nil, 1}})
		refusedBatch(t, tx, "23514", "corrective_actions_one_source", stmt{actionSQL,
			[]any{actID(4), rlsOrgA, rlsSiteA, rlsInspA, nil, nil, nil, "high", supOnSite, supOnSite, nil, 1}})
	})

	t.Run("a finding's action has the finding's severity", func(t *testing.T) {
		refusedBatch(t, tx, "23503", "corrective_actions_finding_fkey", raiseOn(actID(4), rlsInspA, "item", "low", supOnSite, open(supOnSite, 1), nil)...)
		refusedBatch(t, tx, "23503", "corrective_actions_finding_fkey", raiseOn(actID(4), rlsInspA, "nope", "high", supOnSite, open(supOnSite, 1), nil)...)
	})

	t.Run("times", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "client_raised_at is in the future", stmt{`
			INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, severity, title, raised_by, client_raised_at,
			                                time_zone, owner_id, due_on)
			VALUES ($1, $2, $3, $4, 'item', 'high', 'Fix', $5, now() + interval '1 hour', 'Africa/Johannesburg', $5, current_date + 1)`,
			[]any{actID(4), rlsOrgA, rlsSiteA, rlsInspA, supOnSite}})
		refusedBatch(t, tx, "23514", "due_on is before the day it was raised", raiseOn(actID(4), rlsInspA, "item", "medium", supOnSite, open(supOnSite, -1), nil)...)
		refusedBatch(t, tx, "22023", "", stmt{`
			INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, severity, title, raised_by, client_raised_at,
			                                time_zone, owner_id, due_on)
			VALUES ($1, $2, $3, $4, 'item', 'high', 'Fix', $5, now(), 'Mars/Olympus', $5, current_date + 1)`,
			[]any{actID(4), rlsOrgA, rlsSiteA, rlsInspA, supOnSite}})
	})

	t.Run("a due date later than the default needs a lead and a reason", func(t *testing.T) {
		// medium: due in 7 days by default.
		refusedBatch(t, tx, "23514", "later than the default", raiseOn(actID(5), rlsInspA, "item", "medium", supOnSite, open(supOnSite, 8), "Crane booked")...)
		refusedBatch(t, tx, "23514", "later than the default", raiseOn(actID(5), rlsInspA, "item", "medium", rlsUserA, open(supOnSite, 8), nil)...)
		okBatch(t, tx, raiseOn(actID(5), rlsInspA, "item", "medium", rlsUserA, open(supOnSite, 8), "Crane booked")...)
		okBatch(t, tx, raiseOn(actID(6), rlsInspA, "item", "medium", supOnSite, open(supOnSite, 0), nil)...)
	})

	t.Run("the owner is someone who sees the site", func(t *testing.T) {
		for _, owner := range []string{supOffSite, claims, suspendedSO} {
			refusedBatch(t, tx, "23514", "the owner must be", raiseOn(actID(7), rlsInspA, "item", "medium", rlsUserA, open(owner, 1), nil)...)
		}
		okBatch(t, tx, raiseOn(actID(7), rlsInspA, "item", "medium", rlsUserA, open(sheOfficer, 1), nil)...)
		okBatch(t, tx, raiseOn(actID(8), rlsInspA, "item", "medium", rlsUserA, state{"open", supOnSite, "ABC Scaffolding, J. Dlamini", 1}, nil)...)
	})

	t.Run("the raiser sees the site", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "is not an owner, SHE officer or supervisor on its site",
			raiseOn(actID(9), rlsInspA, "item", "medium", supOffSite, open(supOnSite, 1), nil)...)
	})

	t.Run("the raise records the action as raised", func(t *testing.T) {
		base := raiseOn(actID(9), rlsInspA, "item", "medium", supOnSite, open(supOnSite, 1), nil)
		for name, ev := range map[string]stmt{
			"another owner":    event(actID(9), 1, "raised", supOnSite, nil, open(rlsUserA, 1)),
			"another due date": event(actID(9), 1, "raised", supOnSite, nil, open(supOnSite, 0)),
			"another status":   event(actID(9), 1, "raised", supOnSite, nil, state{"cancelled", supOnSite, nil, 1}),
			"another party":    event(actID(9), 1, "raised", supOnSite, nil, state{"open", supOnSite, "Someone", 1}),
			"another actor":    event(actID(9), 1, "raised", rlsUserA, nil, open(supOnSite, 1)),
		} {
			t.Run(name, func(t *testing.T) {
				refusedBatch(t, tx, "23514", "must record the action as it was raised", base[0], ev)
			})
		}
		refusedBatch(t, tx, "23514", "its first event, and only its first, is its raise", base[0], event(actID(9), 1, "reassigned", supOnSite, nil, open(supOnSite, 1)))
		refusedBatch(t, tx, "23514", "event 2 is not the next event", base[0], event(actID(9), 2, "reassigned", supOnSite, nil, open(supOnSite, 1)))
	})

	t.Run("an incident action is raised against its latest version", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "raised against version 2 but the incident is at version 1",
			raiseOnIncident(actID(10), rlsIncA, 2, "medium", supOnSite, open(supOnSite, 7))...)
		okBatch(t, tx, raiseOnIncident(actID(10), rlsIncA, 1, "medium", supOnSite, open(supOnSite, 7))...)
		okBatch(t, tx, v(rlsIncA, 2, nearMiss, ago, nil, "Reclassified", 0, 0))
		refusedBatch(t, tx, "23514", "raised against version 1 but the incident is at version 2",
			raiseOnIncident(actID(11), rlsIncA, 1, "medium", supOnSite, open(supOnSite, 7))...)
		okBatch(t, tx, raiseOnIncident(actID(11), rlsIncA, 2, "critical", supOnSite, open(supOnSite, 0))...)
		var version, kept int
		err := tx.QueryRow(ctx, `SELECT incident_version, (SELECT count(*) FROM corrective_actions WHERE id = $2) FROM corrective_actions WHERE id = $1`,
			actID(10), actID(10)).Scan(&version, &kept)
		if err != nil || version != 1 || kept != 1 {
			t.Errorf("the v1 action: version %d, %d rows, err %v; want it kept on version 1", version, kept, err)
		}
	})

	t.Run("an archived site takes no new action", func(t *testing.T) {
		const archived = "22000000-0000-0000-0000-0000000000c1"
		must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name, status, archived_at) SELECT $1, $2, id, 'Old site', 'archived', now() FROM regulatory_regimes LIMIT 1`,
			archived, rlsOrgA)
		refusedBatch(t, tx, "23514", "its site is archived", stmt{actionSQL,
			[]any{actID(12), rlsOrgA, archived, inspID(99), "item", nil, nil, "high", rlsUserA, rlsUserA, nil, 1}})
	})

	t.Run("a finding superseded by a completed correction takes no new action", func(t *testing.T) {
		const correction = "2b000000-0000-0000-0000-0000000000c1"
		must(t, tx, 1, `
			INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
			                         template_title, template_source_ref, item_count, started_by, client_started_at, corrects_id)
			SELECT $1, $2, $3, id, 'rls_check', 1, $5, 'RLS check', 'reg 1', 1, $4, now(), $6 FROM regulatory_regimes LIMIT 1`,
			correction, rlsOrgA, rlsSiteA, rlsUserA, hash32(4), rlsInspA)
		must(t, tx, 1, responseSQL, rlsOrgA, rlsSiteA, correction, "item", 1, "OK?")
		answer(t, tx, correction, "item", "pass", nil)
		// While the correction is in progress, the finding still stands.
		okBatch(t, tx, raiseOn(actID(13), rlsInspA, "item", "medium", supOnSite, open(supOnSite, 1), nil)...)
		must(t, tx, 1, completeSQL, correction)
		refusedBatch(t, tx, "23514", "the finding is superseded by a correction",
			raiseOn(actID(14), rlsInspA, "item", "medium", supOnSite, open(supOnSite, 1), nil)...)
	})
}

// The rules on an action's later events, as the app role.
func TestActionEventRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)

	// actID(20): raised by supOnSite, owned by supOnSite, due in 3 days
	// (a medium finding's default is 7; this inspection's is high, so use a
	// lead to raise it with a reason).
	a := actID(20)
	okBatch(t, tx, raiseOn(a, rlsInspA, "item", "medium", rlsUserA, state{"open", supOnSite, nil, 3}, "Scaffold hire")...)
	cur := state{"open", supOnSite, nil, 3}
	seq := 2
	ok := func(kind, actor string, reason any, next state) {
		t.Helper()
		okBatch(t, tx, event(a, seq, kind, actor, reason, next))
		seq++
		cur = next
	}
	no := func(msg, kind, actor string, reason any, next state) {
		t.Helper()
		refusedBatch(t, tx, "23514", msg, event(a, seq, kind, actor, reason, next))
	}

	t.Run("reassigning", func(t *testing.T) {
		no("only its raiser, an owner or a SHE officer can reassign it", "reassigned", supOnSite2, nil, state{"open", supOnSite2, nil, 3})
		no("the owner must be", "reassigned", rlsUserA, nil, state{"open", supOffSite, nil, 3})
		no("changes the owner or responsible party, and nothing else", "reassigned", rlsUserA, nil, state{"open", supOnSite2, nil, 4})
		no("changes the owner or responsible party, and nothing else", "reassigned", rlsUserA, nil, cur)
		ok("reassigned", rlsUserA, nil, state{"open", supOnSite2, nil, 3})
		ok("reassigned", rlsUserA, "Sub does the work", state{"open", supOnSite2, "ABC Scaffolding", 3})
	})

	t.Run("moving the due date", func(t *testing.T) {
		no("changes the due date, and nothing else", "due_changed", rlsUserA, "x", state{"open", supOnSite, "ABC Scaffolding", 5})
		no("changes the due date, and nothing else", "due_changed", rlsUserA, "x", cur)
		no("due_on is in the past", "due_changed", rlsUserA, nil, state{"open", supOnSite2, "ABC Scaffolding", -1})
		// Later: a lead with a reason.
		no("moving a due date later needs", "due_changed", supOnSite2, "Rain", state{"open", supOnSite2, "ABC Scaffolding", 5})
		no("moving a due date later needs", "due_changed", sheOfficer, nil, state{"open", supOnSite2, "ABC Scaffolding", 5})
		ok("due_changed", sheOfficer, "Rain", state{"open", supOnSite2, "ABC Scaffolding", 5})
		// Earlier: the raiser, the owner or a lead. supOnSite is neither now.
		no("only its raiser, its owner, an owner or a SHE officer can move its due date earlier", "due_changed", supOnSite, nil,
			state{"open", supOnSite2, "ABC Scaffolding", 4})
		ok("due_changed", supOnSite2, nil, state{"open", supOnSite2, "ABC Scaffolding", 4})
		ok("due_changed", rlsUserA, nil, state{"open", supOnSite2, "ABC Scaffolding", 0})
	})

	t.Run("the header follows its events", func(t *testing.T) {
		var owner, party string
		var count int
		var dueOK bool
		err := tx.QueryRow(ctx, `
			SELECT owner_id::text, responsible_party, event_count,
			       due_on = (now() AT TIME ZONE 'Africa/Johannesburg')::date AND due_at = action_due_at(due_on, time_zone)
			FROM corrective_actions WHERE id = $1`, a).Scan(&owner, &party, &count, &dueOK)
		if err != nil || owner != supOnSite2 || party != "ABC Scaffolding" || count != seq-1 || !dueOK {
			t.Errorf("header owner %s party %s events %d due ok %v, err %v; want the last event's state", owner, party, count, dueOK, err)
		}
	})

	t.Run("events come in order", func(t *testing.T) {
		refusedBatch(t, tx, "23514", fmt.Sprintf("event %d is not the next event", seq+1), event(a, seq+1, "reassigned", rlsUserA, nil, state{"open", rlsUserA, nil, 0}))
		refusedBatch(t, tx, "23514", fmt.Sprintf("event %d is not the next event", seq-1), event(a, seq-1, "reassigned", rlsUserA, nil, state{"open", rlsUserA, nil, 0}))
		refusedBatch(t, tx, "23514", "client_at is in the future", stmt{`
			INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, status, owner_id, due_on)
			VALUES ($1, $2, $3, $4, $5, 'reassigned', $6, now() + interval '1 hour', 'open', $6, current_date)`,
			[]any{evID(9999), rlsOrgA, rlsSiteA, a, seq, rlsUserA}})
		refusedBatch(t, tx, "23514", "is not an owner, SHE officer or supervisor on its site",
			event(a, seq, "due_changed", supOffSite, nil, state{"open", supOnSite2, "ABC Scaffolding", 1}))
	})

	t.Run("cancelling", func(t *testing.T) {
		no("only an owner or a SHE officer can cancel it", "cancelled", supOnSite2, "Duplicate", state{"cancelled", supOnSite2, "ABC Scaffolding", 0})
		refusedBatch(t, tx, "23514", "corrective_action_events_cancel_reason",
			event(a, seq, "cancelled", rlsUserA, nil, state{"cancelled", supOnSite2, "ABC Scaffolding", 0}))
		no("changes the status to cancelled, and nothing else", "cancelled", rlsUserA, "Duplicate", state{"cancelled", rlsUserA, "ABC Scaffolding", 0})
		ok("cancelled", rlsUserA, "Duplicate", state{"cancelled", supOnSite2, "ABC Scaffolding", 0})
		no("is cancelled", "reassigned", rlsUserA, nil, state{"cancelled", rlsUserA, nil, 0})
		no("is cancelled", "due_changed", rlsUserA, "x", state{"cancelled", supOnSite2, "ABC Scaffolding", 1})
	})

	t.Run("nothing changes as the app", func(t *testing.T) {
		refused(t, tx, "42501", "", `UPDATE corrective_actions SET title = 'x' WHERE id = $1`, a)
		refused(t, tx, "42501", "", `DELETE FROM corrective_actions WHERE id = $1`, a)
		refused(t, tx, "42501", "", `UPDATE corrective_action_events SET reason = 'x' WHERE action_id = $1`, a)
		refused(t, tx, "42501", "", `DELETE FROM corrective_action_events WHERE action_id = $1`, a)
		// The state columns move only to the next event's state.
		refused(t, tx, "23514", "an action changes only by a new event", `UPDATE corrective_actions SET status = 'open' WHERE id = $1`, a)
		refused(t, tx, "23514", "an action changes only by a new event", `UPDATE corrective_actions SET event_count = event_count + 1 WHERE id = $1`, a)
		must(t, tx, 1, `UPDATE corrective_actions SET status = status WHERE id = $1`, a)
	})
}

// The guards hold for the superuser too.
func TestActionsFrozenForEveryRole(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	for _, sql := range []string{
		`UPDATE corrective_actions SET title = 'x' WHERE id = $1`,
		`UPDATE corrective_actions SET severity = 'low' WHERE id = $1`,
		`UPDATE corrective_actions SET status = 'closed' WHERE id = $1`,
		`UPDATE corrective_actions SET due_on = due_on + 30, due_at = due_at + interval '30 days' WHERE id = $1`,
		`DELETE FROM corrective_actions WHERE id = $1`,
		`UPDATE corrective_action_events SET reason = 'x' WHERE action_id = $1`,
		`DELETE FROM corrective_action_events WHERE action_id = $1`,
	} {
		refused(t, tx, "23514", "", sql, rlsActionA)
	}
	refuseTruncatesOf(t, tx, "record a new event instead", "corrective_actions", "corrective_action_events")

	// corrective_actions_guard on its own: with the advance trigger off
	// (rolled back with the test), event 2 exists but the header is still
	// at 1. Only event 2's exact state is accepted.
	must(t, tx, 0, `ALTER TABLE corrective_action_events DISABLE TRIGGER corrective_action_events_advance`)
	must(t, tx, 1, eventSQL, evID(1), rlsOrgA, rlsSiteA, rlsActionA, 2, "due_changed", rlsUserA, nil, "open", rlsUserA, nil, 0)
	for _, sql := range []string{
		`UPDATE corrective_actions SET event_count = 2 WHERE id = $1`,
		`UPDATE corrective_actions SET event_count = 2, due_on = due_on - 1 WHERE id = $1`,
		`UPDATE corrective_actions SET event_count = 2, due_on = due_on - 1, due_at = due_at - interval '1 day', status = 'cancelled' WHERE id = $1`,
		`UPDATE corrective_actions SET event_count = 3, due_on = due_on - 1, due_at = due_at - interval '1 day' WHERE id = $1`,
		`UPDATE corrective_actions SET event_count = 2, due_on = due_on - 1, due_at = action_due_at(due_on - 1, time_zone), title = 'x' WHERE id = $1`,
	} {
		refused(t, tx, "23514", "an action changes only by a new event", sql, rlsActionA)
	}
	must(t, tx, 1, `UPDATE corrective_actions SET event_count = 2, due_on = due_on - 1, due_at = action_due_at(due_on - 1, time_zone) WHERE id = $1`, rlsActionA)

	// The advance trigger on its own: with the insert trigger off, an event
	// that skips a number still fails, on the header's sequence.
	must(t, tx, 0, `ALTER TABLE corrective_action_events ENABLE TRIGGER corrective_action_events_advance`)
	must(t, tx, 0, `ALTER TABLE corrective_action_events DISABLE TRIGGER corrective_action_events_insert`)
	refused(t, tx, "23514", "event 4 is not the next event", eventSQL, evID(2), rlsOrgA, rlsSiteA, rlsActionA, 4, "due_changed", rlsUserA, nil, "open", rlsUserA, nil, 0)
}

// refuseTruncatesOf is refuseTruncates for tables whose trigger says msg.
func refuseTruncatesOf(t *testing.T, tx pgx.Tx, msg string, tables ...string) {
	t.Helper()
	for _, target := range tables {
		var stmts []stmt
		for _, other := range tables {
			if other != target {
				stmts = append(stmts, stmt{`ALTER TABLE ` + other + ` DISABLE TRIGGER ` + other + `_no_truncate`, nil})
			}
		}
		stmts = append(stmts, stmt{`TRUNCATE ` + target + ` CASCADE`, nil})
		refusedBatch(t, tx, "23514", target+" is append-only; "+msg, stmts...)
	}
}

// committed runs fn as org C and commits.
func committed(t *testing.T, fn func(ctx context.Context, tx pgx.Tx) error) {
	t.Helper()
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatalf("committed fixture: %v", err)
	}
}

// committedFinding commits a completed inspection in org C with one failed
// item "a" (severity sev) and its photo, and returns the photo's id.
func committedFinding(t *testing.T, insp, sev string) string {
	t.Helper()
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	photo := "2d000000-0000-0000-0000-" + insp[len(insp)-12:]
	if _, err := admin.Exec(context.Background(), evidenceSQL, photo, orgC, siteC, userC, hash32(8), "act/"+insp, "v1", "verified"); err != nil {
		t.Fatalf("photo: %v", err)
	}
	committed(t, func(ctx context.Context, tx pgx.Tx) error {
		for _, s := range []stmt{
			{startSQL, []any{insp, orgC, siteC, hash32(5), 1, userC, nil}},
			{responseSQL, []any{orgC, siteC, insp, "a", 1, "Is a OK?"}},
			{answerSQL, []any{insp, "a", "fail", sev}},
			{attachSQL, []any{insp, "a", photo}},
			{completeSQL, []any{insp}},
		} {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return fmt.Errorf("%s: %w", strings.TrimSpace(s.sql), err)
			}
		}
		return nil
	})
	return photo
}

// committedCorrection commits an in-progress correction of insp in org C,
// with item "a" answered as given.
func committedCorrection(t *testing.T, id, insp, result string, sev any) {
	t.Helper()
	committed(t, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, startSQL, id, orgC, siteC, hash32(5), 1, userC, insp); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, responseSQL, orgC, siteC, id, "a", 1, "Is a OK?"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, answerSQL, id, "a", result, sev)
		return err
	})
}

// raiseC raises an action in org C on finding (insp, "a") or incident
// version, owned by userC, due on the default for its severity.
func raiseC(ctx context.Context, tx pgx.Tx, id string, insp, inc any, version any, sev string, days int) error {
	if _, err := tx.Exec(ctx, actionSQL, id, orgC, siteC, insp, itemIf(insp), inc, version, sev, userC, userC, nil, days); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, eventSQL, evID(raiseSeq(id)), orgC, siteC, id, 1, "raised", userC, nil, "open", userC, nil, days)
	return err
}

func itemIf(insp any) any {
	if insp == nil {
		return nil
	}
	return "a"
}

func asC(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, fn)
}

// Two events on one action at once: the second waits for the first, then
// fails on its sequence number.
func TestActionEventsQueue(t *testing.T) {
	ctx := context.Background()
	insp, a := inspID(160), actID(60)
	committedFinding(t, insp, "high")
	committed(t, func(ctx context.Context, tx pgx.Tx) error { return raiseC(ctx, tx, a, insp, nil, nil, "high", 1) })

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if _, err := first.Exec(ctx, eventSQL, evID(6002), orgC, siteC, a, 2, "due_changed", userC, nil, "open", userC, nil, 0); err != nil {
		t.Fatalf("first event 2: %v", err)
	}

	second := make(chan error, 1)
	go func() {
		second <- asC(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, eventSQL, evID(6003), orgC, siteC, a, 2, "reassigned", userC, "x", "open", userC, "Sub", 1)
			return err
		})
	}()
	waitingOn(t, "INSERT INTO corrective_action_events")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23514" || !strings.Contains(err.Error(), "event 2 is not the next event") {
		t.Fatalf("second event 2: %v, want SQLSTATE 23514 (not the next event)", err)
	}
}

// A raise on a finding and the completion of its correction take turns on
// the correction's row, in either order.
func TestRaiseAndCorrectionCompletion(t *testing.T) {
	ctx := context.Background()

	t.Run("completion first: the raise waits, then is refused", func(t *testing.T) {
		insp, corr := inspID(161), inspID(162)
		committedFinding(t, insp, "high")
		committedCorrection(t, corr, insp, "pass", nil)

		completing := tenantTx(t)
		defer completing.Rollback(ctx)
		if _, err := completing.Exec(ctx, completeSQL, corr); err != nil {
			t.Fatalf("complete the correction: %v", err)
		}
		raised := make(chan error, 1)
		go func() {
			raised <- asC(ctx, func(tx pgx.Tx) error { return raiseC(ctx, tx, actID(61), insp, nil, nil, "high", 1) })
		}()
		waitingOn(t, "INSERT INTO corrective_actions")
		if err := completing.Commit(ctx); err != nil {
			t.Fatalf("commit the completion: %v", err)
		}
		if err := <-raised; pgCode(err) != "23514" || !strings.Contains(err.Error(), "superseded by a correction") {
			t.Fatalf("raise: %v, want SQLSTATE 23514 (superseded)", err)
		}
	})

	t.Run("raise first: the completion waits, then both stand", func(t *testing.T) {
		insp, corr := inspID(163), inspID(164)
		committedFinding(t, insp, "high")
		committedCorrection(t, corr, insp, "pass", nil)

		raising := tenantTx(t)
		defer raising.Rollback(ctx)
		if err := raiseC(ctx, raising, actID(63), insp, nil, nil, "high", 1); err != nil {
			t.Fatalf("raise: %v", err)
		}
		completed := make(chan error, 1)
		go func() {
			completed <- asC(ctx, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, completeSQL, corr); return err })
		}()
		waitingOn(t, "UPDATE inspections")
		if err := raising.Commit(ctx); err != nil {
			t.Fatalf("commit the raise: %v", err)
		}
		if err := <-completed; err != nil {
			t.Fatalf("completion after the raise: %v", err)
		}
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM corrective_actions WHERE id = $1`, actID(63)).Scan(&n); err != nil || n != 1 {
			t.Errorf("%d actions, err %v; want the raise kept", n, err)
		}
	})
}

// A raise on an incident version and an amendment of the incident queue on
// the incident's row, in either order.
func TestRaiseAndAmendment(t *testing.T) {
	ctx := context.Background()

	t.Run("amendment first: the raise waits, then is refused", func(t *testing.T) {
		inc := incID(160)
		committedIncident(t, inc)
		amending := tenantTx(t)
		defer amending.Rollback(ctx)
		if err := submitC(ctx, amending, inc, 2); err != nil {
			t.Fatalf("amend: %v", err)
		}
		raised := make(chan error, 1)
		go func() {
			raised <- asC(ctx, func(tx pgx.Tx) error { return raiseC(ctx, tx, actID(65), nil, inc, 1, "medium", 7) })
		}()
		waitingOn(t, "INSERT INTO corrective_actions")
		if err := amending.Commit(ctx); err != nil {
			t.Fatalf("commit the amendment: %v", err)
		}
		if err := <-raised; pgCode(err) != "23514" || !strings.Contains(err.Error(), "raised against version 1 but the incident is at version 2") {
			t.Fatalf("raise: %v, want SQLSTATE 23514 (not the latest version)", err)
		}
	})

	t.Run("raise first: the amendment waits, then both stand", func(t *testing.T) {
		inc := incID(161)
		committedIncident(t, inc)
		raising := tenantTx(t)
		defer raising.Rollback(ctx)
		if err := raiseC(ctx, raising, actID(66), nil, inc, 1, "medium", 7); err != nil {
			t.Fatalf("raise: %v", err)
		}
		amended := make(chan error, 1)
		go func() { amended <- asC(ctx, func(tx pgx.Tx) error { return submitC(ctx, tx, inc, 2) }) }()
		waitingOn(t, "INSERT INTO incident_versions")
		if err := raising.Commit(ctx); err != nil {
			t.Fatalf("commit the raise: %v", err)
		}
		if err := <-amended; err != nil {
			t.Fatalf("amendment after the raise: %v", err)
		}
	})
}

// Severity order and default due days, as the migration defines them.
func TestActionDueFunctions(t *testing.T) {
	ctx := context.Background()
	want := map[string][2]int{"low": {1, 14}, "medium": {2, 7}, "high": {3, 1}, "critical": {4, 0}}
	for sev, w := range want {
		var rank, days int
		if err := pool.QueryRow(ctx, `SELECT severity_rank($1), action_default_due_days($1)`, sev).Scan(&rank, &days); err != nil {
			t.Fatal(err)
		}
		if rank != w[0] || days != w[1] {
			t.Errorf("%s: rank %d, days %d; want %d, %d", sev, rank, days, w[0], w[1])
		}
	}
	// 23:30 local on 15 Nov is still 15 Nov; due 16 Nov means overdue from
	// midnight local on 17 Nov (22:00 UTC on the 16th).
	var day time.Time
	var dueAt time.Time
	err := pool.QueryRow(ctx, `SELECT action_local_date('2026-11-15T21:30:00Z', 'Africa/Johannesburg'),
	                                  action_due_at('2026-11-16', 'Africa/Johannesburg')`).Scan(&day, &dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if day.Format("2006-01-02") != "2026-11-15" || !dueAt.Equal(time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC)) {
		t.Errorf("local date %s, due_at %s; want 2026-11-15 and 2026-11-16T22:00:00Z", day.Format("2006-01-02"), dueAt.UTC())
	}
}
