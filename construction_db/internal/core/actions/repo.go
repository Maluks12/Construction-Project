package actions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// headerSQL selects ActionHeader for the actions a WHERE clause picks. $1 is
// the time the state is judged at. The state (D8) is worked out here only,
// so the register and get can't disagree: a closed action is closed_late
// when the close-out it was verified on was submitted (device time) at or
// after the due time.
const headerSQL = `
	SELECT a.id AS id, a.site_id AS site_id, CASE WHEN a.inspection_id IS NOT NULL THEN 'finding' ELSE 'incident' END AS source,
	       a.inspection_id AS inspection_id, a.item_key AS item_key,
	       (SELECT c.id FROM inspections c WHERE c.corrects_id = a.inspection_id AND c.status = 'completed') AS source_corrected_by,
	       a.incident_id AS incident_id, a.incident_version AS incident_version,
	       (SELECT i.latest_version FROM incidents i WHERE i.id = a.incident_id) AS incident_latest_version,
	       action_review(a.id) AS review,
	       a.severity AS severity, a.title AS title, a.description AS description, a.raised_by AS raised_by,
	       a.client_raised_at AS client_raised_at, a.raised_at AS raised_at, a.device_id AS device_id, a.time_zone AS time_zone,
	       a.source_at AS source_at, a.default_due_on::text AS default_due_on, a.status AS status,
	       CASE
	           WHEN a.status = 'cancelled' THEN 'cancelled'
	           WHEN a.status = 'closed' AND v.client_submitted_at >= a.due_at THEN 'closed_late'
	           WHEN a.status = 'closed' THEN 'closed'
	           WHEN a.status = 'awaiting_verification' THEN 'awaiting_verification'
	           WHEN $1 >= a.due_at THEN 'overdue'
	           ELSE 'open'
	       END AS state,
	       a.owner_id AS owner_id,
	       action_site_member(a.org_id, a.site_id, a.owner_id) AS owner_eligible,
	       a.responsible_party AS responsible_party, a.due_on::text AS due_on, a.due_at AS due_at,
	       a.pending_closeout_id AS pending_closeout_id, a.awaiting_since AS awaiting_since, v.recorded_at AS closed_at
	FROM corrective_actions a
	LEFT JOIN LATERAL (
	    SELECT e.recorded_at, c.client_submitted_at
	    FROM corrective_action_events e JOIN corrective_action_closeouts c ON c.id = e.closeout_id
	    WHERE e.action_id = a.id AND e.kind = 'verified'
	    ORDER BY e.seq DESC LIMIT 1
	) v ON a.status = 'closed'
`

func scanHeader(row pgx.Row) (*ActionHeader, error) {
	var h ActionHeader
	err := row.Scan(&h.ID, &h.SiteID, &h.Source, &h.InspectionID, &h.ItemKey, &h.SourceCorrectedBy,
		&h.IncidentID, &h.IncidentVersion, &h.IncidentLatestVersion, &h.Review,
		&h.Severity, &h.Title, &h.Description, &h.RaisedBy, &h.ClientRaisedAt, &h.RaisedAt, &h.DeviceID, &h.TimeZone,
		&h.SourceAt, &h.DefaultDueOn, &h.Status, &h.State,
		&h.OwnerID, &h.OwnerEligible, &h.ResponsibleParty, &h.DueOn, &h.DueAt, &h.PendingCloseoutID, &h.AwaitingSince, &h.ClosedAt)
	if err != nil {
		return nil, err
	}
	h.ClientRaisedAt, h.RaisedAt, h.SourceAt, h.DueAt = h.ClientRaisedAt.UTC(), h.RaisedAt.UTC(), h.SourceAt.UTC(), h.DueAt.UTC()
	h.AwaitingSince, h.ClosedAt = utcPtr(h.AwaitingSince), utcPtr(h.ClosedAt)
	return &h, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func getHeader(ctx context.Context, db DBTX, orgID, siteID, id string, now time.Time) (*ActionHeader, error) {
	h, err := scanHeader(db.QueryRow(ctx, headerSQL+` WHERE a.org_id = $2 AND a.site_id = $3 AND a.id = $4`, now, orgID, siteID, id))
	if err != nil {
		return nil, fmt.Errorf("get action: %w", err)
	}
	return h, nil
}

// actionSite finds an action anywhere in the organisation, so an id used on
// another site is a conflict, not a new action.
func actionSite(ctx context.Context, db DBTX, orgID, id string) (string, error) {
	var site string
	err := db.QueryRow(ctx, `SELECT site_id FROM corrective_actions WHERE org_id = $1 AND id = $2`, orgID, id).Scan(&site)
	if err != nil {
		return "", fmt.Errorf("action site: %w", err)
	}
	return site, nil
}

// locked is what the event and close-out checks need, read under the
// action's row lock.
type locked struct {
	ID, Status, OwnerID, RaisedBy, DueOn, TimeZone string
	ResponsibleParty, PendingCloseoutID            *string
	InspectionID, ItemKey, IncidentID              *string
	EventCount                                     int
	ClientRaisedAt, SourceAt, DueAt                time.Time
}

// lockAction takes the lock an event insert takes (FOR NO KEY UPDATE, as in
// corrective_action_events_insert), so the checks below see the state the
// event will be applied to.
func lockAction(ctx context.Context, db DBTX, orgID, siteID, id string) (*locked, error) {
	var l locked
	err := db.QueryRow(ctx, `
		SELECT id, status, owner_id, raised_by, due_on::text, time_zone, responsible_party, pending_closeout_id,
		       inspection_id, item_key, incident_id, event_count, client_raised_at, source_at, due_at
		FROM corrective_actions WHERE org_id = $1 AND site_id = $2 AND id = $3
		FOR NO KEY UPDATE`, orgID, siteID, id).Scan(
		&l.ID, &l.Status, &l.OwnerID, &l.RaisedBy, &l.DueOn, &l.TimeZone, &l.ResponsibleParty, &l.PendingCloseoutID,
		&l.InspectionID, &l.ItemKey, &l.IncidentID, &l.EventCount, &l.ClientRaisedAt, &l.SourceAt, &l.DueAt)
	if err != nil {
		return nil, fmt.Errorf("lock action: %w", err)
	}
	return &l, nil
}

func listEvents(ctx context.Context, db DBTX, orgID, actionID string) ([]Event, error) {
	rows, err := db.Query(ctx, `
		SELECT id, seq, kind, actor_id, client_at, recorded_at, device_id, reason, status, owner_id, responsible_party,
		       due_on::text, closeout_id, self_verified
		FROM corrective_action_events WHERE org_id = $1 AND action_id = $2 ORDER BY seq`, orgID, actionID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Seq, &e.Kind, &e.ActorID, &e.ClientAt, &e.RecordedAt, &e.DeviceID, &e.Reason, &e.Status,
			&e.OwnerID, &e.ResponsibleParty, &e.DueOn, &e.CloseoutID, &e.SelfVerified); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.ClientAt, e.RecordedAt = e.ClientAt.UTC(), e.RecordedAt.UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return out, nil
}

// listCloseouts reads an action's close-outs, oldest first, each with its
// photos (by evidence id) and what was decided.
func listCloseouts(ctx context.Context, db DBTX, orgID string, h *ActionHeader) ([]Closeout, error) {
	rows, err := db.Query(ctx, `
		SELECT c.id, c.note, c.submitted_by, c.client_submitted_at, c.submitted_at, c.device_id,
		       (SELECT e.kind FROM corrective_action_events e WHERE e.closeout_id = c.id AND e.kind IN ('verified', 'rejected')),
		       coalesce(array_agg(x.evidence_id::text ORDER BY x.evidence_id) FILTER (WHERE x.evidence_id IS NOT NULL), '{}'),
		       coalesce(array_agg(ev.client_captured_at ORDER BY x.evidence_id) FILTER (WHERE x.evidence_id IS NOT NULL), '{}')
		FROM corrective_action_closeouts c
		LEFT JOIN corrective_action_closeout_evidence x ON x.closeout_id = c.id
		LEFT JOIN evidence ev ON ev.id = x.evidence_id
		WHERE c.org_id = $1 AND c.action_id = $2
		GROUP BY c.id
		ORDER BY c.submitted_at, c.id`, orgID, h.ID)
	if err != nil {
		return nil, fmt.Errorf("list close-outs: %w", err)
	}
	defer rows.Close()
	out := []Closeout{}
	for rows.Next() {
		var c Closeout
		var ids []string
		var captured []time.Time
		if err := rows.Scan(&c.ID, &c.Note, &c.SubmittedBy, &c.ClientSubmittedAt, &c.SubmittedAt, &c.DeviceID, &c.Decision, &ids, &captured); err != nil {
			return nil, fmt.Errorf("scan close-out: %w", err)
		}
		c.ClientSubmittedAt, c.SubmittedAt = c.ClientSubmittedAt.UTC(), c.SubmittedAt.UTC()
		c.Late = !c.ClientSubmittedAt.Before(h.DueAt)
		c.RecordedAfterDue = !c.Late && !c.SubmittedAt.Before(h.DueAt)
		c.Evidence = []CloseoutEvidence{}
		for i, id := range ids {
			at := captured[i].UTC()
			c.Evidence = append(c.Evidence, CloseoutEvidence{EvidenceID: id, ClientCapturedAt: at, CapturedBeforeRaised: at.Before(h.ClientRaisedAt)})
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list close-outs: %w", err)
	}
	return out, nil
}

// listFilter picks actions for the register. Site "" means every site the
// caller can open: all of them for an owner or SHE officer, and the sites
// they were added to for a supervisor (the scope of GET /v1/sites).
type listFilter struct {
	SiteID, State, Owner, Source string
	// "true" or "false": only actions whose owner can (or can't) still act on them.
	OwnerEligible string
	UserID        string
	Lead          bool
	Limit         int
}

// listHeaders is the register: due soonest first, then oldest raised. The
// state filter uses the same expression the rows show.
func listHeaders(ctx context.Context, db DBTX, orgID string, f listFilter, now time.Time) ([]ActionHeader, error) {
	q := `SELECT * FROM (` + headerSQL + `
		WHERE a.org_id = $2
		  AND ($3 = '' OR a.site_id::text = $3)
		  AND ($4 OR EXISTS (SELECT 1 FROM site_members m WHERE m.site_id = a.site_id AND m.user_id = $5))
		  AND ($6 = '' OR a.owner_id::text = $6)
		  AND ($7 = '' OR ($7 = 'finding') = (a.inspection_id IS NOT NULL))
	) x
	WHERE ($8 = '' OR x.state = $8)
	  AND ($10 = '' OR x.owner_eligible = ($10 = 'true'))
	ORDER BY x.due_at, x.raised_at, x.id
	LIMIT $9`
	rows, err := db.Query(ctx, q, now, orgID, f.SiteID, f.Lead, f.UserID, f.Owner, f.Source, f.State, f.Limit, f.OwnerEligible)
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	defer rows.Close()
	out := []ActionHeader{}
	for rows.Next() {
		h, err := scanHeader(rows)
		if err != nil {
			return nil, fmt.Errorf("scan action: %w", err)
		}
		out = append(out, *h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	return out, nil
}

// finding is what raising an action on a finding needs.
type finding struct {
	Severity     string
	SupersededBy *string
}

func getFinding(ctx context.Context, db DBTX, orgID, siteID, inspectionID, itemKey string) (*finding, error) {
	var f finding
	err := db.QueryRow(ctx, `
		SELECT f.severity, (SELECT c.id FROM inspections c WHERE c.corrects_id = f.inspection_id AND c.status = 'completed')
		FROM inspection_findings f
		WHERE f.org_id = $1 AND f.site_id = $2 AND f.inspection_id = $3 AND f.item_key = $4`,
		orgID, siteID, inspectionID, itemKey).Scan(&f.Severity, &f.SupersededBy)
	if err != nil {
		return nil, fmt.Errorf("get finding: %w", err)
	}
	return &f, nil
}

// incidentLatest returns the incident's latest version, if it is on the site.
func incidentLatest(ctx context.Context, db DBTX, orgID, siteID, incidentID string) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT latest_version FROM incidents WHERE org_id = $1 AND site_id = $2 AND id = $3`,
		orgID, siteID, incidentID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("incident version: %w", err)
	}
	return n, nil
}

// siteMember and lead are the database's own rules (000018), asked so the
// API can answer before writing.
func siteMember(ctx context.Context, db DBTX, orgID, siteID, userID string) (bool, error) {
	var ok bool
	if err := db.QueryRow(ctx, `SELECT action_site_member($1, $2, $3)`, orgID, siteID, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("site member: %w", err)
	}
	return ok, nil
}

func lead(ctx context.Context, db DBTX, orgID, userID string) (bool, error) {
	var ok bool
	if err := db.QueryRow(ctx, `SELECT action_lead($1, $2)`, orgID, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("lead: %w", err)
	}
	return ok, nil
}

// dueDates asks the database (the one implementation) for the local date of
// at in the zone, and the severity's default due date from it.
func dueDates(ctx context.Context, db DBTX, at time.Time, zone, severity string) (day, def string, err error) {
	err = db.QueryRow(ctx, `SELECT action_local_date($1, $2)::text, (action_local_date($1, $2) + action_default_due_days($3))::text`,
		at, zone, severity).Scan(&day, &def)
	if err != nil {
		return "", "", fmt.Errorf("due dates: %w", err)
	}
	return day, def, nil
}

func localDate(ctx context.Context, db DBTX, at time.Time, zone string) (string, error) {
	var day string
	if err := db.QueryRow(ctx, `SELECT action_local_date($1, $2)::text`, at, zone).Scan(&day); err != nil {
		return "", fmt.Errorf("local date: %w", err)
	}
	return day, nil
}

func insertAction(ctx context.Context, db DBTX, orgID, siteID, id, userID, zone, severity, dueOn string, req RaiseRequest) error {
	var insp, item, inc any
	var version any
	if req.Finding != nil {
		insp, item = req.Finding.InspectionID, req.Finding.ItemKey
	} else {
		inc, version = req.Incident.IncidentID, req.Incident.Version
	}
	_, err := db.Exec(ctx, `
		INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, incident_id, incident_version, severity,
		                                title, description, raised_by, client_raised_at, device_id, time_zone, owner_id,
		                                responsible_party, due_on)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		id, orgID, siteID, insp, item, inc, version, severity, req.Title, req.Description, userID, req.ClientRaisedAt,
		req.DeviceID, zone, req.OwnerID, req.ResponsibleParty, dueOn)
	if err != nil {
		return fmt.Errorf("insert action: %w", err)
	}
	return nil
}

// newEvent is one event row to insert, with the state after it.
type newEvent struct {
	ID, ActionID, Kind, ActorID, Status, OwnerID, DueOn string
	Seq                                                 int
	ClientAt                                            time.Time
	DeviceID, Reason, ResponsibleParty, CloseoutID      *string
	SelfVerified                                        *bool
}

func insertEvent(ctx context.Context, db DBTX, orgID, siteID string, e newEvent) error {
	_, err := db.Exec(ctx, `
		INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, device_id, reason,
		                                      status, owner_id, responsible_party, due_on, closeout_id, self_verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		e.ID, orgID, siteID, e.ActionID, e.Seq, e.Kind, e.ActorID, e.ClientAt, e.DeviceID, e.Reason,
		e.Status, e.OwnerID, e.ResponsibleParty, e.DueOn, e.CloseoutID, e.SelfVerified)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// storedEvent is an event looked up by its id, for a repeat.
type storedEvent struct {
	Event
	ActionID string
	SiteID   string
}

func getEvent(ctx context.Context, db DBTX, orgID, id string) (*storedEvent, error) {
	var e storedEvent
	err := db.QueryRow(ctx, `
		SELECT id, seq, kind, actor_id, client_at, recorded_at, device_id, reason, status, owner_id, responsible_party,
		       due_on::text, closeout_id, self_verified, action_id, site_id
		FROM corrective_action_events WHERE org_id = $1 AND id = $2`, orgID, id).Scan(
		&e.ID, &e.Seq, &e.Kind, &e.ActorID, &e.ClientAt, &e.RecordedAt, &e.DeviceID, &e.Reason, &e.Status,
		&e.OwnerID, &e.ResponsibleParty, &e.DueOn, &e.CloseoutID, &e.SelfVerified, &e.ActionID, &e.SiteID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}
	return &e, nil
}

func insertCloseout(ctx context.Context, db DBTX, orgID, siteID, id, actionID, userID string, req CloseoutRequest) error {
	_, err := db.Exec(ctx, `
		INSERT INTO corrective_action_closeouts (id, org_id, site_id, action_id, note, submitted_by, client_submitted_at, device_id, evidence_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, orgID, siteID, actionID, req.Note, userID, req.ClientSubmittedAt, req.DeviceID, len(req.EvidenceIDs))
	if err != nil {
		return fmt.Errorf("insert close-out: %w", err)
	}
	for _, ev := range req.EvidenceIDs {
		if _, err := db.Exec(ctx, `INSERT INTO corrective_action_closeout_evidence (org_id, site_id, closeout_id, evidence_id) VALUES ($1, $2, $3, $4)`,
			orgID, siteID, id, ev); err != nil {
			return fmt.Errorf("insert close-out photo: %w", err)
		}
	}
	return nil
}

// storedCloseout is a close-out looked up by its id, for a repeat.
type storedCloseout struct {
	ActionID, SiteID, SubmittedBy string
	Note, DeviceID                *string
	ClientSubmittedAt             time.Time
	EvidenceIDs                   []string
}

func getCloseout(ctx context.Context, db DBTX, orgID, id string) (*storedCloseout, error) {
	var c storedCloseout
	err := db.QueryRow(ctx, `
		SELECT c.action_id, c.site_id, c.submitted_by, c.note, c.device_id, c.client_submitted_at,
		       coalesce(array_agg(x.evidence_id::text ORDER BY x.evidence_id) FILTER (WHERE x.evidence_id IS NOT NULL), '{}')
		FROM corrective_action_closeouts c LEFT JOIN corrective_action_closeout_evidence x ON x.closeout_id = c.id
		WHERE c.org_id = $1 AND c.id = $2
		GROUP BY c.id`, orgID, id).Scan(&c.ActionID, &c.SiteID, &c.SubmittedBy, &c.Note, &c.DeviceID, &c.ClientSubmittedAt, &c.EvidenceIDs)
	if err != nil {
		return nil, fmt.Errorf("get close-out: %w", err)
	}
	return &c, nil
}

func closeoutSubmitter(ctx context.Context, db DBTX, orgID, id string) (string, error) {
	var by string
	if err := db.QueryRow(ctx, `SELECT submitted_by FROM corrective_action_closeouts WHERE org_id = $1 AND id = $2`, orgID, id).Scan(&by); err != nil {
		return "", fmt.Errorf("close-out submitter: %w", err)
	}
	return by, nil
}

// photo is a candidate close-out photo as stored.
type photo struct {
	Status     string
	CapturedAt time.Time
}

func photos(ctx context.Context, db DBTX, orgID, siteID string, ids []string) (map[string]photo, error) {
	rows, err := db.Query(ctx, `SELECT id::text, status, client_captured_at FROM evidence WHERE org_id = $1 AND site_id = $2 AND id = ANY($3::uuid[])`,
		orgID, siteID, ids)
	if err != nil {
		return nil, fmt.Errorf("close-out photos: %w", err)
	}
	defer rows.Close()
	out := map[string]photo{}
	for rows.Next() {
		var id string
		var p photo
		if err := rows.Scan(&id, &p.Status, &p.CapturedAt); err != nil {
			return nil, fmt.Errorf("scan close-out photo: %w", err)
		}
		out[id] = p
	}
	return out, rows.Err()
}

// sourcePhotos returns which of ids are the source's own photos: the failed
// item's, or any version's of the incident.
func sourcePhotos(ctx context.Context, db DBTX, orgID string, l *locked, ids []string) (map[string]bool, error) {
	rows, err := db.Query(ctx, `
		SELECT evidence_id::text FROM inspection_response_evidence
		WHERE org_id = $1 AND inspection_id = $2 AND item_key = $3 AND evidence_id = ANY($5::uuid[])
		UNION
		SELECT evidence_id::text FROM incident_version_evidence
		WHERE org_id = $1 AND incident_id = $4 AND evidence_id = ANY($5::uuid[])`,
		orgID, l.InspectionID, l.ItemKey, l.IncidentID, ids)
	if err != nil {
		return nil, fmt.Errorf("source photos: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan source photo: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}
