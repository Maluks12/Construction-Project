package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/platform/database"
)

// teamOrg is a committed organisation for the team management rules (000054):
// two owners, two SHE officers, a supervisor on site, an invited and a removed
// member, a live site, a second live site and an archived one.
type teamOrg struct {
	org, owner1, owner2, she, she2, sup, invited, removed string
	site, site2, archived                                 string
}

func newTeamOrg(t *testing.T) teamOrg {
	t.Helper()
	ctx := context.Background()
	o := teamOrg{org: fresh(), owner1: fresh(), owner2: fresh(), she: fresh(), she2: fresh(), sup: fresh(),
		invited: fresh(), removed: fresh(), site: fresh(), site2: fresh(), archived: fresh()}
	type m struct{ id, role, status string }
	members := []m{{o.owner1, "owner", "active"}, {o.owner2, "owner", "active"}, {o.she, "she_officer", "active"},
		{o.she2, "she_officer", "active"}, {o.sup, "supervisor", "active"}, {o.invited, "supervisor", "invited"},
		{o.removed, "she_officer", "removed"}}
	run := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	run(`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Team Co', 'construction', ARRAY['construction'])`, o.org)
	for _, mb := range members {
		run(`INSERT INTO users (id, email, full_name) VALUES ($1::uuid, $1::text || '@team.test', 'Member ' || left($1::text, 4))`, mb.id)
		run(`INSERT INTO org_members (org_id, user_id, role, status) VALUES ($1, $2, $3, $4)`, o.org, mb.id, mb.role, mb.status)
	}
	for _, s := range []struct{ id, status string }{{o.site, "active"}, {o.site2, "active"}, {o.archived, "archived"}} {
		run(`INSERT INTO sites (id, org_id, regime_id, name, status) SELECT $1, $2, id, 'Team site', $3 FROM regulatory_regimes LIMIT 1`, s.id, o.org, s.status)
	}
	run(`INSERT INTO site_members (org_id, site_id, user_id) VALUES ($1, $2, $3)`, o.org, o.site, o.sup)
	run(`INSERT INTO invitations (org_id, user_id, token_hash, invited_by, expires_at) VALUES ($1, $2, $3, $4, now() + interval '7 days')`,
		o.org, o.invited, "team-"+o.invited, o.owner1)
	return o
}

// asActor runs sql as actor in o's organisation, with the reason and the new
// supervisor's sites (nil: not set), in a transaction of its own, committed
// only when keep.
func (o teamOrg) asActor(t *testing.T, actor, reason string, sites []string, keep bool, sql string, args ...any) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: o.org, UserID: actor}); err != nil {
		t.Fatal(err)
	}
	if err := setMemberChange(ctx, tx, reason, sites); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return err
	}
	if keep {
		return tx.Commit(ctx)
	}
	return nil
}

func setMemberChange(ctx context.Context, tx pgx.Tx, reason string, sites []string) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.member_reason', $1, true)`, reason); err != nil {
		return err
	}
	if sites != nil {
		if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.member_site_ids', $1::uuid[]::text, true)`, sites); err != nil {
			return err
		}
	}
	return nil
}

func constraintOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

func wantConstraint(t *testing.T, what string, err error, constraint string) {
	t.Helper()
	if constraintOf(err) != constraint {
		t.Errorf("%s: got %v, want constraint %s", what, err, constraint)
	}
}

func wantOK(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
	}
}

const (
	setRole   = `UPDATE org_members SET role = $3 WHERE org_id = $1 AND user_id = $2`
	setStatus = `UPDATE org_members SET status = $3 WHERE org_id = $1 AND user_id = $2`
	invite    = `UPDATE org_members SET status = 'invited', role = $3 WHERE org_id = $1 AND user_id = $2`
)

func adminValue(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := admin.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return s
}

// The rules on org_members, each refused by the database whatever path
// writes the row (D1-D3, D6, D7).
func TestMemberRules(t *testing.T) {
	o := newTeamOrg(t)
	const why = "Left the company"

	t.Run("someone acts, and gives a reason", func(t *testing.T) {
		ctx := context.Background()
		err := database.WithTenant(ctx, pool, database.Tenant{OrgID: o.org}, func(tx pgx.Tx) error {
			if err := setMemberChange(ctx, tx, why, nil); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, setStatus, o.org, o.she, "suspended")
			return err
		})
		wantConstraint(t, "no actor", err, "org_members_actor")
		for _, r := range []string{"", "   ", strings.Repeat("x", 501)} {
			wantConstraint(t, "reason "+r[:min(len(r), 3)], o.asActor(t, o.owner1, r, nil, false, setStatus, o.org, o.she, "suspended"), "org_members_reason")
		}
		wantOK(t, "500 characters", o.asActor(t, o.owner1, strings.Repeat("x", 500), nil, false, setStatus, o.org, o.she, "suspended"))
	})

	t.Run("who may change whom", func(t *testing.T) {
		wantConstraint(t, "self", o.asActor(t, o.owner1, why, nil, false, setStatus, o.org, o.owner1, "suspended"), "org_members_self")
		wantConstraint(t, "SHE officer demotes an owner", o.asActor(t, o.she, why, nil, false, setRole, o.org, o.owner1, "she_officer"), "org_members_actor")
		wantConstraint(t, "SHE officer suspends an owner", o.asActor(t, o.she, why, nil, false, setStatus, o.org, o.owner1, "suspended"), "org_members_actor")
		wantConstraint(t, "SHE officer grants owner", o.asActor(t, o.she, why, nil, false, setRole, o.org, o.sup, "owner"), "org_members_actor")
		wantConstraint(t, "supervisor suspends", o.asActor(t, o.sup, why, nil, false, setStatus, o.org, o.she2, "suspended"), "org_members_actor")
		wantConstraint(t, "a removed member acts", o.asActor(t, o.removed, why, nil, false, setStatus, o.org, o.she2, "suspended"), "org_members_actor")
		wantOK(t, "SHE officer suspends a SHE officer", o.asActor(t, o.she, why, nil, false, setStatus, o.org, o.she2, "suspended"))
		wantOK(t, "SHE officer makes a SHE officer a claims handler", o.asActor(t, o.she, why, nil, false, setRole, o.org, o.she2, "claims_handler"))
		wantOK(t, "owner grants owner", o.asActor(t, o.owner1, why, nil, false, setRole, o.org, o.she2, "owner"))
		wantOK(t, "owner demotes owner", o.asActor(t, o.owner1, why, nil, false, setRole, o.org, o.owner2, "she_officer"))
	})

	t.Run("statuses", func(t *testing.T) {
		wantConstraint(t, "removed to active", o.asActor(t, o.owner1, why, nil, false, setStatus, o.org, o.removed, "active"), "org_members_transition")
		wantConstraint(t, "removed to suspended", o.asActor(t, o.owner1, why, nil, false, setStatus, o.org, o.removed, "suspended"), "org_members_transition")
		wantConstraint(t, "invited to suspended", o.asActor(t, o.owner1, why, nil, false, setStatus, o.org, o.invited, "suspended"), "org_members_transition")
		wantConstraint(t, "active to invited", o.asActor(t, o.owner1, why, nil, false, setStatus, o.org, o.she, "invited"), "org_members_transition")
		wantConstraint(t, "accepting changes the role", o.asActor(t, o.invited, "", nil, false,
			`UPDATE org_members SET status = 'active', role = 'owner' WHERE org_id = $1 AND user_id = $2`, o.org, o.invited), "org_members_transition")
		wantOK(t, "accepting", o.asActor(t, o.invited, "", nil, false, setStatus, o.org, o.invited, "active"))
		// Invitations need no reason; the sender may grant the role.
		wantConstraint(t, "SHE officer invites back as owner", o.asActor(t, o.she, "", nil, false, invite, o.org, o.removed, "owner"), "org_members_actor")
		wantOK(t, "SHE officer invites back as supervisor", o.asActor(t, o.she, "", nil, false, invite, o.org, o.removed, "supervisor"))
	})

	t.Run("never deleted", func(t *testing.T) {
		ctx := context.Background()
		tx := beginAs(t, pool, o.org)
		defer tx.Rollback(ctx)
		refused(t, tx, "42501", "", `DELETE FROM org_members WHERE user_id = $1`, o.she)
		atx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer atx.Rollback(ctx)
		refused(t, atx, "23514", "never deleted", `DELETE FROM org_members WHERE user_id = $1`, o.she)
		refused(t, atx, "23514", "", `TRUNCATE org_members CASCADE`)
	})
}

// D7: into supervisor names its sites exactly; out of supervisor, or removed,
// clears them; suspended keeps them. Each change's history row records the
// sites before and after, and site_member_history every row added or removed.
func TestMemberSiteScoping(t *testing.T) {
	o := newTeamOrg(t)
	const why = "Moved to site work"
	intoSup := func(sites []string) error {
		return o.asActor(t, o.owner1, why, sites, true, setRole, o.org, o.she2, "supervisor")
	}
	wantConstraint(t, "no site_ids", intoSup(nil), "org_members_site_ids")
	wantConstraint(t, "an archived site", intoSup([]string{o.site, o.archived}), "org_members_site_ids")
	wantConstraint(t, "another org's site", intoSup([]string{fresh()}), "org_members_site_ids")
	wantOK(t, "two live sites", intoSup([]string{o.site2, o.site, o.site}))

	sites := func(user string) string {
		return adminValue(t, `SELECT coalesce(array_agg(site_id ORDER BY site_id), '{}')::text FROM site_members WHERE user_id = $1`, user)
	}
	lastHistory := func(user string) string {
		return adminValue(t, `SELECT concat_ws('|', prev_role, role, reason, prev_site_ids::text, site_ids::text, changed_by)
			FROM member_history WHERE org_id = $1 AND user_id = $2 ORDER BY id DESC LIMIT 1`, o.org, user)
	}
	both := adminValue(t, `SELECT array_agg(s ORDER BY s)::text FROM unnest($1::uuid[]) s`, []string{o.site, o.site2})
	if got := sites(o.she2); got != both {
		t.Errorf("new supervisor's sites %s, want %s", got, both)
	}
	if got, want := lastHistory(o.she2), "she_officer|supervisor|"+why+"|{}|"+both+"|"+o.owner1; got != want {
		t.Errorf("history %s, want %s", got, want)
	}

	// Suspended keeps the sites; reinstated, they're as they were.
	wantOK(t, "suspend", o.asActor(t, o.owner1, why, nil, true, setStatus, o.org, o.sup, "suspended"))
	if got := sites(o.sup); got != "{"+o.site+"}" {
		t.Errorf("suspended supervisor's sites %s", got)
	}
	wantOK(t, "reinstate", o.asActor(t, o.owner1, why, nil, true, setStatus, o.org, o.sup, "active"))

	// Out of supervisor: cleared, and recorded.
	wantOK(t, "out of supervisor", o.asActor(t, o.she, why, nil, true, setRole, o.org, o.sup, "she_officer"))
	if got := sites(o.sup); got != "{}" {
		t.Errorf("former supervisor keeps sites %s", got)
	}
	if got, want := lastHistory(o.sup), "supervisor|she_officer|"+why+"|{"+o.site+"}|{}|"+o.she; got != want {
		t.Errorf("history %s, want %s", got, want)
	}
	// Removed: cleared too.
	wantOK(t, "remove", o.asActor(t, o.owner1, why, nil, true, setStatus, o.org, o.she2, "removed"))
	if got := sites(o.she2); got != "{}" {
		t.Errorf("removed member keeps sites %s", got)
	}

	got := strings.Join(historyValues(t, `SELECT change || ':' || coalesce(changed_by::text, '-') FROM site_member_history
		WHERE org_id = $1 AND user_id = $2 ORDER BY id`, o.org, o.sup), ",")
	if want := "added:-,removed:" + o.she; got != want {
		t.Errorf("site_member_history %s, want %s", got, want)
	}
}

// D4, D5: suspension and removal sign the member out of the organisation at
// once and cancel what was queued to email them; a role change does neither.
// Removing an invited member withdraws the invitation.
func TestMemberStatusEffects(t *testing.T) {
	o := newTeamOrg(t)
	ctx := context.Background()
	session := func(user string) string {
		return adminValue(t, `INSERT INTO sessions (user_id, org_id, expires_at) VALUES ($1, $2, now() + interval '30 days') RETURNING id::text`, user, o.org)
	}
	email := func(user string) string {
		n := adminValue(t, `INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, title)
			VALUES ($1, $2, 'competency_expiry', 'competency', gen_random_uuid(), 'on_day', 'Expiry') RETURNING id::text`, o.org, user)
		if _, err := admin.Exec(ctx, `INSERT INTO notification_emails (notification_id, org_id, to_address) VALUES ($1, $2, 'x@team.test')`, n, o.org); err != nil {
			t.Fatal(err)
		}
		return n
	}
	revoked := func(id string) bool {
		return adminValue(t, `SELECT (revoked_at IS NOT NULL)::text FROM sessions WHERE id = $1`, id) == "true"
	}
	emailStatus := func(n string) string {
		return adminValue(t, `SELECT status FROM notification_emails WHERE notification_id = $1`, n)
	}

	s1, e1 := session(o.she), email(o.she)
	wantOK(t, "role change", o.asActor(t, o.owner1, "Promoted", nil, true, setRole, o.org, o.she, "claims_handler"))
	if revoked(s1) || emailStatus(e1) != "pending" {
		t.Errorf("a role change revoked the session (%v) or cancelled the email (%s)", revoked(s1), emailStatus(e1))
	}
	wantOK(t, "suspend", o.asActor(t, o.owner1, "Investigation", nil, true, setStatus, o.org, o.she, "suspended"))
	if !revoked(s1) || emailStatus(e1) != "cancelled" {
		t.Errorf("after suspension: session revoked %v, email %s", revoked(s1), emailStatus(e1))
	}

	s2, e2 := session(o.sup), email(o.sup)
	wantOK(t, "remove", o.asActor(t, o.owner1, "Left", nil, true, setStatus, o.org, o.sup, "removed"))
	if !revoked(s2) || emailStatus(e2) != "cancelled" {
		t.Errorf("after removal: session revoked %v, email %s", revoked(s2), emailStatus(e2))
	}

	wantOK(t, "remove invited", o.asActor(t, o.owner1, "Not joining", nil, true, setStatus, o.org, o.invited, "removed"))
	if got := adminValue(t, `SELECT (revoked_at IS NOT NULL)::text FROM invitations WHERE user_id = $1`, o.invited); got != "true" {
		t.Error("removing an invited member left the invitation open")
	}
}

// D3, forced: two owners demote each other at once. The second waits on the
// organisation's row, then counts with a fresh snapshot, sees the first's
// demotion and is refused: the organisation keeps one active owner.
func TestLastOwnerRace(t *testing.T) {
	o := newTeamOrg(t)
	ctx := context.Background()

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(ctx)
	if err := database.SetTenant(ctx, first, database.Tenant{OrgID: o.org, UserID: o.owner1}); err != nil {
		t.Fatal(err)
	}
	if err := setMemberChange(ctx, first, "Stepping back", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec(ctx, setRole, o.org, o.owner2, "she_officer"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- func() error {
			tx, err := pool.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: o.org, UserID: o.owner2}); err != nil {
				return err
			}
			if err := setMemberChange(ctx, tx, "Stepping back too", nil); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, setRole+` /* second demotion */`, o.org, o.owner1, "she_officer"); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}()
	}()
	waitingOn(t, "second demotion")
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wantConstraint(t, "the second demotion", <-done, "org_members_last_owner")
	if got := adminValue(t, `SELECT count(*)::text FROM org_members WHERE org_id = $1 AND role = 'owner' AND status = 'active'`, o.org); got != "1" {
		t.Fatalf("%s active owners after the race, want 1", got)
	}

	// Sequentially, the last owner can't step down either.
	wantConstraint(t, "the last owner steps down", o.asActor(t, o.owner1, "Retiring", nil, false, setRole, o.org, o.owner1, "she_officer"),
		"org_members_last_owner")
}

// D3: the organisation lock (FOR NO KEY UPDATE) doesn't hold up foreign-key
// checks on the organisation (FOR KEY SHARE): a site is created while a
// membership change is open.
func TestMemberLockAllowsForeignKeys(t *testing.T) {
	o := newTeamOrg(t)
	ctx := context.Background()
	held, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if err := database.SetTenant(ctx, held, database.Tenant{OrgID: o.org, UserID: o.owner1}); err != nil {
		t.Fatal(err)
	}
	if err := setMemberChange(ctx, held, "Holding the lock", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := held.Exec(ctx, setStatus, o.org, o.she, "suspended"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err = database.WithTenant(ctx, pool, database.Tenant{OrgID: o.org, UserID: o.owner2}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO sites (org_id, regime_id, name) SELECT $1, id, 'During the change' FROM regulatory_regimes LIMIT 1`, o.org)
		return err
	})
	if err != nil {
		t.Fatalf("a site insert during a membership change: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("the site insert took %v: it waited on the organisation lock", d)
	}
}
