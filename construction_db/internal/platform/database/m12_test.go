package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// action_escalation_step (000038) is the step notifications_due escalates an
// overdue action to: the highest escalation_N it reminds at the same moment.
func TestEscalationStepMatchesReminders(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx) // a SHE officer and an owner to escalate to
	act, ev := fresh(), fresh()
	must(t, tx, 1, actionOnFindingSQL, act, rlsOrgA, rlsSiteA, rlsInspA, "item", "medium", rlsUserA, 0)
	must(t, tx, 1, raiseOwnSQL, ev, rlsOrgA, rlsSiteA, act, rlsUserA, 0)

	var dueOn time.Time
	var deadline time.Time
	if err := tx.QueryRow(ctx, `SELECT due_on, due_at FROM corrective_actions WHERE id = $1`, act).Scan(&dueOn, &deadline); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		at   time.Duration
		want int
	}{
		{-time.Hour, 0},
		{0, 1},
		{23 * time.Hour, 1},
		{24 * time.Hour, 2},
		{3*24*time.Hour - time.Minute, 2},
		{3 * 24 * time.Hour, 3},
		{10 * 24 * time.Hour, 3},
	} {
		now := deadline.Add(c.at)
		var step int
		if err := tx.QueryRow(ctx, `SELECT action_escalation_step($1, $2, 'Africa/Johannesburg', ARRAY[0,1,3])`, now, dueOn).Scan(&step); err != nil {
			t.Fatal(err)
		}
		reminded := 0
		for th := range dueFor(dueAt(t, tx, now), act) {
			if n := map[string]int{"escalation_1": 1, "escalation_2": 2, "escalation_3": 3}[th]; n > reminded {
				reminded = n
			}
		}
		if step != c.want || step != reminded {
			t.Errorf("due_at%+v: step %d, notifications_due's highest %d, want %d", c.at, step, reminded, c.want)
		}
	}
}

// site_diary_replay_as_of replays only the revisions received by as_of, and
// at 'infinity' it is site_diary_replay, which the header guard holds the
// stored diary to.
func TestDiaryReplayAsOf(t *testing.T) {
	ctx := context.Background()
	site := freshSiteC(t)
	date := time.Now().In(johannesburg()).Format(time.DateOnly)
	r1, r2, r3 := fresh(), fresh(), fresh()
	received := map[string]time.Time{}
	for _, r := range []struct {
		id   string
		base any
		ago  time.Duration
	}{
		{r1, nil, 3 * time.Minute},
		{r2, r1, 2 * time.Minute},
		{r3, r1, time.Minute}, // based on r1, not the head: a conflict
	} {
		if err := asC(ctx, func(tx pgx.Tx) error {
			return diaryC(ctx, tx, site, date, r.id, r.base, time.Now().Add(-r.ago), nil)
		}); err != nil {
			t.Fatal(err)
		}
		var at time.Time
		if err := admin.QueryRow(ctx, `SELECT server_received_at FROM site_diary_revisions WHERE id = $1`, r.id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		received[r.id] = at
	}

	type replay struct {
		head      *string
		conflicts []string
		revisions int
	}
	asOf := func(at any) replay {
		var r replay
		if err := admin.QueryRow(ctx, `SELECT head::text, conflicts::text[], revisions FROM site_diary_replay_as_of($1, $2, $3)`,
			site, date, at).Scan(&r.head, &r.conflicts, &r.revisions); err != nil {
			t.Fatal(err)
		}
		return r
	}
	head := func(r replay) string {
		if r.head == nil {
			return "none"
		}
		return *r.head
	}

	if r := asOf(received[r1].Add(-time.Microsecond)); r.head != nil || r.revisions != 0 {
		t.Errorf("before the first revision: %+v", r)
	}
	if r := asOf(received[r1]); head(r) != r1 || r.revisions != 1 {
		t.Errorf("as of r1: head %s, %d revisions", head(r), r.revisions)
	}
	if r := asOf(received[r2]); head(r) != r2 || r.revisions != 2 || len(r.conflicts) != 0 {
		t.Errorf("as of r2: head %s, conflicts %v, %d revisions", head(r), r.conflicts, r.revisions)
	}
	last := asOf("infinity")
	if head(last) != r3 || len(last.conflicts) != 2 || last.revisions != 3 {
		t.Errorf("at infinity: head %s, conflicts %v, %d revisions", head(last), last.conflicts, last.revisions)
	}

	var same bool
	if err := admin.QueryRow(ctx, `
		SELECT (r.head, r.conflicts, r.revisions) IS NOT DISTINCT FROM (d.head_revision_id, d.conflict_revision_ids, d.revision_count)
		       AND (r.head, r.conflicts, r.late, r.revisions) IS NOT DISTINCT FROM (i.head, i.conflicts, i.late, i.revisions)
		FROM site_diary_replay($1, $2) r, site_diary_replay_as_of($1, $2, 'infinity') i, site_diaries d
		WHERE d.site_id = $1 AND d.diary_date = $2`, site, date).Scan(&same); err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("site_diary_replay, the as-of replay at infinity and the stored diary disagree")
	}
}
