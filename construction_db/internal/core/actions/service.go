package actions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/checklists"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
)

// A device clock may run this far ahead of the server; the database allows
// the same, and refuses close-out photos older than the source by as much.
const clockSkew = 5 * time.Minute

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	// Clock is now for state and future checks; nil is the real time.
	Clock clock.Clock
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service) *Service {
	return &Service{pool: pool, sites: sitesSvc}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

// errRetry: an insert lost a race the checks couldn't see (a concurrent
// identical request, or a correction or amendment committed in between).
// The request runs once more, and its checks then see what happened.
var errRetry = errors.New("retry")

// racedError: the database refused an insert for a rule a commit made true
// after the checks ran (a correction completed, an amendment, the site
// archived). Retried once, the checks then name it; if the database refuses
// again, its own rule is the answer.
type racedError struct{ err error }

func (r *racedError) Error() string { return r.err.Error() }
func (r *racedError) Unwrap() error { return r.err }

// once runs fn, and again if it lost a race.
func once(fn func() error) error {
	err := fn()
	var raced *racedError
	if errors.Is(err, errRetry) || errors.As(err, &raced) {
		err = fn()
	}
	if errors.As(err, &raced) {
		return raced.err
	}
	return err
}

// Raise raises an action under an id the device chose, with its raise event
// (whose id is the action's). created is false for an identical repeat.
func (s *Service) Raise(ctx context.Context, orgID, userID, role, siteID, actionID string, req RaiseRequest) (*Action, bool, error) {
	id, err := uuid.Parse(actionID)
	if err != nil {
		return nil, false, ErrBadActionID
	}
	if err := normaliseRaise(&req, s.Clock.Now()); err != nil {
		return nil, false, err
	}
	var (
		a       *Action
		created bool
	)
	err = once(func() error {
		a, created, err = s.raiseOnce(ctx, orgID, userID, role, siteID, id.String(), req)
		return err
	})
	if errors.Is(err, errRetry) {
		err = ErrActionIDReused
	}
	return finish(a, created, err, "raise action")
}

func (s *Service) raiseOnce(ctx context.Context, orgID, userID, role, siteID, id string, req RaiseRequest) (a *Action, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		site, zone := sr.Site, sr.Data.Regime.TimeZone

		// An identical repeat answers 200 before any other check, so a retry
		// after the site was archived or the finding corrected still succeeds.
		if existing, err := s.view(ctx, tx, orgID, site.ID, id); err == nil {
			if !sameRaise(existing, userID, req) {
				return ErrActionIDReused
			}
			a = existing
			return nil
		} else if !isNotFound(err) {
			return err
		}
		if _, err := actionSite(ctx, tx, orgID, id); err == nil {
			return ErrActionIDReused // the id is an action on another site
		} else if !isNotFound(err) {
			return err
		}

		if site.Status == sites.StatusArchived {
			return ErrSiteArchived
		}
		severity, err := checkSource(ctx, tx, orgID, site.ID, req)
		if err != nil {
			return err
		}
		if ok, err := siteMember(ctx, tx, orgID, site.ID, req.OwnerID); err != nil {
			return err
		} else if !ok {
			return ErrOwnerNotEligible
		}
		day, def, err := dueDates(ctx, tx, req.ClientRaisedAt, zone, severity)
		if err != nil {
			return err
		}
		due := def
		if req.DueOn != nil {
			due = *req.DueOn
		}
		switch {
		case due < day:
			return ErrDueBeforeRaised
		case due > def && !isLead(role):
			return ErrLaterNeedsLead
		case due > def && req.DueReason == nil:
			return ErrDueReasonRequired
		}

		err = insertAction(ctx, tx, orgID, site.ID, id, userID, zone, severity, due, req)
		if isUniqueViolation(err, "corrective_actions_pkey") {
			return errRetry
		}
		// A correction, amendment or archiving committed since the checks
		// above: once more, and the checks say which.
		if isRule(err, "superseded by a correction") || isRule(err, "but the incident is at version") || isRule(err, "its site is archived") {
			return &racedError{err}
		}
		if err != nil {
			return err
		}
		err = insertEvent(ctx, tx, orgID, site.ID, newEvent{
			ID: id, ActionID: id, Seq: 1, Kind: KindRaised, ActorID: userID, ClientAt: req.ClientRaisedAt,
			DeviceID: req.DeviceID, Reason: req.DueReason, Status: StatusOpen, OwnerID: req.OwnerID,
			ResponsibleParty: req.ResponsibleParty, DueOn: due,
		})
		if isUniqueViolation(err, "corrective_action_events_id_key") {
			return ErrActionIDReused // already an event's id
		}
		if err != nil {
			return err
		}
		created = true
		a, err = s.view(ctx, tx, orgID, site.ID, id)
		return err
	})
	return a, created, err
}

// checkSource checks the finding or incident version and returns the
// action's severity.
func checkSource(ctx context.Context, db DBTX, orgID, siteID string, req RaiseRequest) (string, error) {
	if req.Finding != nil {
		f, err := getFinding(ctx, db, orgID, siteID, req.Finding.InspectionID, req.Finding.ItemKey)
		if isNotFound(err) {
			return "", ErrFindingNotFound
		}
		if err != nil {
			return "", err
		}
		if f.SupersededBy != nil {
			return "", ErrFindingSuperseded
		}
		if req.Severity != nil && *req.Severity != f.Severity {
			return "", ErrSeverityMismatch
		}
		return f.Severity, nil
	}
	latest, err := incidentLatest(ctx, db, orgID, siteID, req.Incident.IncidentID)
	if isNotFound(err) {
		return "", ErrIncidentNotFound
	}
	if err != nil {
		return "", err
	}
	switch {
	case req.Incident.Version > latest:
		return "", ErrIncidentNotFound
	case req.Incident.Version < latest:
		return "", &NotLatestError{LatestVersion: latest}
	}
	return *req.Severity, nil
}

// sameRaise: a retry must match what the first raise recorded, which its
// raise event holds (the owner and due date may have moved since).
func sameRaise(a *Action, by string, req RaiseRequest) bool {
	r := a.Events[0]
	switch {
	case req.Finding != nil && (a.InspectionID == nil || *a.InspectionID != req.Finding.InspectionID || *a.ItemKey != req.Finding.ItemKey):
		return false
	case req.Incident != nil && (a.IncidentID == nil || *a.IncidentID != req.Incident.IncidentID || *a.IncidentVersion != req.Incident.Version):
		return false
	case req.Severity != nil && *req.Severity != a.Severity:
		return false
	case req.DueOn == nil && r.DueOn != a.DefaultDueOn, req.DueOn != nil && *req.DueOn != r.DueOn:
		return false
	}
	return a.RaisedBy == by && a.Title == req.Title && equalPtr(a.Description, req.Description) &&
		r.OwnerID == req.OwnerID && equalPtr(r.ResponsibleParty, req.ResponsibleParty) && equalPtr(r.Reason, req.DueReason) &&
		a.ClientRaisedAt.Equal(req.ClientRaisedAt) && equalPtr(a.DeviceID, req.DeviceID)
}

// view reads an action with its events and close-outs, judged now.
func (s *Service) view(ctx context.Context, db DBTX, orgID, siteID, id string) (*Action, error) {
	h, err := getHeader(ctx, db, orgID, siteID, id, s.Clock.Now())
	if err != nil {
		return nil, err
	}
	a := &Action{ActionHeader: *h}
	if a.Events, err = listEvents(ctx, db, orgID, id); err != nil {
		return nil, err
	}
	if a.Closeouts, err = listCloseouts(ctx, db, orgID, h); err != nil {
		return nil, err
	}
	return a, nil
}

// Get returns an action with its history. It works on an archived site.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, actionID string) (*Action, error) {
	var a *Action
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		a, err = s.view(ctx, tx, orgID, site.ID, actionID)
		if isNotFound(err) {
			return ErrActionNotFound
		}
		return err
	})
	a, _, err = finish(a, false, err, "get action")
	return a, err
}

// List is a site's register; ListAll is the same across every site the
// caller can open (siteID ""). The query is checked before the site.
func (s *Service) List(ctx context.Context, orgID, userID, role, siteID, state, owner, source string, limit int) ([]ActionHeader, error) {
	f, err := filterOf(userID, role, state, owner, source, limit)
	if err != nil {
		return nil, err
	}
	var out []ActionHeader
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if siteID != "" {
			site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
			if err != nil {
				return err
			}
			f.SiteID = site.ID
		}
		out, err = listHeaders(ctx, tx, orgID, f, s.Clock.Now())
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	return out, nil
}

func filterOf(userID, role, state, owner, source string, limit int) (listFilter, error) {
	f := listFilter{State: state, Source: source, UserID: userID, Lead: isLead(role), Limit: limit}
	switch {
	case state != "" && !slices.Contains(States, state):
		return f, ErrBadState
	case source != "" && !slices.Contains(Sources, source):
		return f, ErrBadSource
	case limit < 1 || limit > 200:
		return f, ErrBadLimit
	}
	switch owner {
	case "":
	case "me":
		f.Owner = userID
	default:
		u, err := uuid.Parse(owner)
		if err != nil {
			return f, ErrBadOwner
		}
		f.Owner = u.String()
	}
	return f, nil
}

// Event records one change to an action under an id the device chose.
// The action id is read as a UUID (malformed: not found) and used in its
// canonical form, so a retry in another case is still a repeat. Order of
// checks: site scope, a repeat of the event id (200), the action (locked), closed or cancelled, who may, then the request against the
// action's state. created is false for an identical repeat.
func (s *Service) Event(ctx context.Context, orgID, userID, role, siteID, actionID, eventID string, req EventRequest) (*Action, bool, error) {
	aid, err := uuid.Parse(actionID)
	if err != nil {
		return nil, false, ErrActionNotFound
	}
	eid, err := uuid.Parse(eventID)
	if err != nil {
		return nil, false, ErrBadEventID
	}
	if err := normaliseEvent(&req, s.Clock.Now()); err != nil {
		return nil, false, err
	}
	var (
		a       *Action
		created bool
	)
	err = once(func() error {
		a, created, err = s.eventOnce(ctx, orgID, userID, role, siteID, aid.String(), eid.String(), req)
		return err
	})
	if errors.Is(err, errRetry) {
		err = ErrEventIDReused
	}
	return finish(a, created, err, "record action event")
}

func (s *Service) eventOnce(ctx context.Context, orgID, userID, role, siteID, actionID, eventID string, req EventRequest) (a *Action, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if e, err := getEvent(ctx, tx, orgID, eventID); err == nil {
			if e.SiteID != site.ID || e.ActionID != actionID || !sameEvent(e, userID, req) {
				return ErrEventIDReused
			}
			a, err = s.view(ctx, tx, orgID, site.ID, actionID)
			return err
		} else if !isNotFound(err) {
			return err
		}

		l, err := lockAction(ctx, tx, orgID, site.ID, actionID)
		if isNotFound(err) {
			return ErrActionNotFound
		}
		if err != nil {
			return err
		}
		ev, err := s.plan(ctx, tx, orgID, site.ID, userID, role, l, req)
		if err != nil {
			return err
		}
		ev.ID, ev.ActionID, ev.Seq, ev.ActorID, ev.ClientAt, ev.DeviceID = eventID, l.ID, l.EventCount+1, userID, req.ClientAt, req.DeviceID
		err = insertEvent(ctx, tx, orgID, site.ID, *ev)
		if isUniqueViolation(err, "corrective_action_events_id_key") {
			return errRetry
		}
		if err != nil {
			return err
		}
		created = true
		a, err = s.view(ctx, tx, orgID, site.ID, l.ID)
		return err
	})
	return a, created, err
}

// plan checks a request against the locked action and returns the event
// to record (without its id, sequence, actor and times).
func (s *Service) plan(ctx context.Context, db DBTX, orgID, siteID, userID, role string, l *locked, req EventRequest) (*newEvent, error) {
	switch {
	case l.Status == StatusCancelled:
		return nil, ErrActionCancelled
	case l.Status == StatusClosed && req.Kind != DoReopen:
		return nil, ErrActionClosed
	}
	lead := isLead(role)
	ev := &newEvent{Kind: kindOf[req.Kind], Reason: req.Reason, Status: l.Status, OwnerID: l.OwnerID,
		ResponsibleParty: l.ResponsibleParty, DueOn: l.DueOn}

	switch req.Kind {
	case DoReassign:
		if !lead && userID != l.RaisedBy {
			return nil, ErrCantReassign
		}
		if *req.OwnerID == l.OwnerID && equalPtr(req.ResponsibleParty, l.ResponsibleParty) {
			return nil, ErrNothingChanges
		}
		if ok, err := siteMember(ctx, db, orgID, siteID, *req.OwnerID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrOwnerNotEligible
		}
		ev.OwnerID, ev.ResponsibleParty = *req.OwnerID, req.ResponsibleParty

	case DoMoveDue:
		due := *req.DueOn
		switch {
		case due > l.DueOn && !lead:
			return nil, ErrLaterNeedsLead
		case due < l.DueOn && !lead && userID != l.RaisedBy && userID != l.OwnerID:
			return nil, ErrCantMoveEarlier
		case due == l.DueOn:
			return nil, ErrNothingChanges
		case due > l.DueOn && req.Reason == nil:
			return nil, ErrMoveLaterReason
		}
		today, err := localDate(ctx, db, req.ClientAt, l.TimeZone)
		if err != nil {
			return nil, err
		}
		if due < today {
			return nil, ErrDueInPast
		}
		ev.DueOn = due

	case DoVerify, DoReject:
		if !lead {
			return nil, ErrNeedsLead
		}
		if l.Status != StatusAwaiting {
			return nil, ErrNotAwaiting
		}
		if l.PendingCloseoutID == nil || *req.CloseoutID != *l.PendingCloseoutID {
			return nil, ErrNotPending
		}
		ev.CloseoutID = req.CloseoutID
		if req.Kind == DoReject {
			ev.Status = StatusOpen
			break
		}
		by, err := closeoutSubmitter(ctx, db, orgID, *req.CloseoutID)
		if err != nil {
			return nil, err
		}
		self := by == userID
		ev.Status, ev.SelfVerified = StatusClosed, &self

	case DoReopen:
		if !lead {
			return nil, ErrNeedsLead
		}
		if l.Status != StatusClosed {
			return nil, ErrNotClosed
		}
		ev.Status = StatusOpen

	case DoCancel:
		if !lead {
			return nil, ErrNeedsLead
		}
		ev.Status = StatusCancelled
	}
	return ev, nil
}

// sameEvent: a retry must ask for what the stored event recorded.
func sameEvent(e *storedEvent, by string, req EventRequest) bool {
	if e.ActorID != by || e.Kind != kindOf[req.Kind] || !e.ClientAt.Equal(req.ClientAt) ||
		!equalPtr(e.DeviceID, req.DeviceID) || !equalPtr(e.Reason, req.Reason) {
		return false
	}
	switch req.Kind {
	case DoReassign:
		return e.OwnerID == *req.OwnerID && equalPtr(e.ResponsibleParty, req.ResponsibleParty)
	case DoMoveDue:
		return e.DueOn == *req.DueOn
	case DoVerify, DoReject:
		return equalPtr(e.CloseoutID, req.CloseoutID)
	}
	return true
}

// CloseOut records a close-out with its photos under an id the device chose,
// and the closeout_submitted event (whose id is the close-out's).
func (s *Service) CloseOut(ctx context.Context, orgID, userID, role, siteID, actionID, closeoutID string, req CloseoutRequest) (*Action, bool, error) {
	aid, err := uuid.Parse(actionID)
	if err != nil {
		return nil, false, ErrActionNotFound
	}
	cid, err := uuid.Parse(closeoutID)
	if err != nil {
		return nil, false, ErrBadCloseoutID
	}
	if err := normaliseCloseout(&req, s.Clock.Now()); err != nil {
		return nil, false, err
	}
	var (
		a       *Action
		created bool
	)
	err = once(func() error {
		a, created, err = s.closeOutOnce(ctx, orgID, userID, role, siteID, aid.String(), cid.String(), req)
		return err
	})
	if errors.Is(err, errRetry) {
		err = ErrCloseoutIDReused
	}
	return finish(a, created, err, "close out action")
}

func (s *Service) closeOutOnce(ctx context.Context, orgID, userID, role, siteID, actionID, id string, req CloseoutRequest) (a *Action, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if c, err := getCloseout(ctx, tx, orgID, id); err == nil {
			if c.SiteID != site.ID || c.ActionID != actionID || !sameCloseout(c, userID, req) {
				return ErrCloseoutIDReused
			}
			a, err = s.view(ctx, tx, orgID, site.ID, actionID)
			return err
		} else if !isNotFound(err) {
			return err
		}

		l, err := lockAction(ctx, tx, orgID, site.ID, actionID)
		if isNotFound(err) {
			return ErrActionNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case l.Status == StatusCancelled:
			return ErrActionCancelled
		case l.Status == StatusClosed:
			return ErrActionClosed
		case !isLead(role) && userID != l.OwnerID:
			return ErrCantCloseOut
		case l.Status == StatusAwaiting:
			return ErrAlreadyAwaiting
		}
		if err := checkCloseoutPhotos(ctx, tx, orgID, site.ID, l, req.EvidenceIDs); err != nil {
			return err
		}

		err = insertCloseout(ctx, tx, orgID, site.ID, id, l.ID, userID, req)
		if isUniqueViolation(err, "corrective_action_closeouts_pkey") {
			return errRetry
		}
		if err != nil {
			return err
		}
		err = insertEvent(ctx, tx, orgID, site.ID, newEvent{
			ID: id, ActionID: l.ID, Seq: l.EventCount + 1, Kind: KindCloseoutSubmitted, ActorID: userID,
			ClientAt: req.ClientSubmittedAt, DeviceID: req.DeviceID, Status: StatusAwaiting, OwnerID: l.OwnerID,
			ResponsibleParty: l.ResponsibleParty, DueOn: l.DueOn, CloseoutID: &id,
		})
		if isUniqueViolation(err, "corrective_action_events_id_key") {
			return ErrCloseoutIDReused // already an event's id
		}
		if err != nil {
			return err
		}
		created = true
		a, err = s.view(ctx, tx, orgID, site.ID, l.ID)
		return err
	})
	return a, created, err
}

// checkCloseoutPhotos: verified evidence on the site (pending is 409:
// retry once it is sealed), not older than the source, and not one of the
// source's own photos (D5). The database refuses the same.
func checkCloseoutPhotos(ctx context.Context, db DBTX, orgID, siteID string, l *locked, ids []string) error {
	found, err := photos(ctx, db, orgID, siteID, ids)
	if err != nil {
		return err
	}
	own, err := sourcePhotos(ctx, db, orgID, l, ids)
	if err != nil {
		return err
	}
	var missing, rejected, pending, old, source []string
	for _, id := range ids {
		p, ok := found[id]
		switch {
		case !ok:
			missing = append(missing, id)
		case p.Status == evidence.StatusRejected:
			rejected = append(rejected, id)
		case p.Status == evidence.StatusPending:
			pending = append(pending, id)
		case p.CapturedAt.Before(l.SourceAt.Add(-clockSkew)):
			old = append(old, id)
		case own[id]:
			source = append(source, id)
		}
	}
	for _, c := range []struct {
		ids []string
		err error
	}{{missing, ErrEvidenceNotFound}, {rejected, ErrEvidenceRejected}, {old, ErrEvidenceTooOld}, {source, ErrEvidenceIsSource}, {pending, ErrEvidenceNotVerified}} {
		if len(c.ids) > 0 {
			return &EvidenceError{Err: c.err, IDs: c.ids}
		}
	}
	return nil
}

func sameCloseout(c *storedCloseout, by string, req CloseoutRequest) bool {
	return c.SubmittedBy == by && equalPtr(c.Note, req.Note) && c.ClientSubmittedAt.Equal(req.ClientSubmittedAt) &&
		equalPtr(c.DeviceID, req.DeviceID) && slices.Equal(c.EvidenceIDs, slices.Sorted(slices.Values(req.EvidenceIDs)))
}

// finish maps an error to a sentinel or wraps it, once per endpoint.
func finish(a *Action, created bool, err error, prefix string) (*Action, bool, error) {
	if err = mapRule(err); isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", prefix, err)
	}
	return a, created, nil
}

// mapRule turns a rule the database enforced into the API's error, for the
// few that a race can reach past the checks above.
func mapRule(err error) error {
	switch {
	case isRule(err, "the finding is superseded by a correction"):
		return ErrFindingSuperseded
	case isRule(err, "but the incident is at version"):
		return ErrNotLatestVersion
	case isRule(err, "its site is archived"):
		return ErrSiteArchived
	}
	return err
}

func isRule(err error, fragment string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, fragment)
}

func isSentinel(err error) bool {
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

var sentinels = []error{
	sites.ErrSiteNotFound,
	ErrActionNotFound, ErrBadActionID, ErrActionIDReused, ErrSiteArchived, ErrOneSource, ErrBadFinding, ErrFindingNotFound,
	ErrFindingSuperseded, ErrBadIncident, ErrIncidentNotFound, ErrNotLatestVersion, ErrBadSeverity, ErrSeverityRequired,
	ErrSeverityMismatch, ErrBadTitle, ErrBadDescription, ErrBadOwnerID, ErrOwnerNotEligible, ErrBadParty, ErrBadDueOn,
	ErrDueBeforeRaised, ErrDueInPast, ErrLaterNeedsLead, ErrDueReasonRequired, ErrNoRaisedAt, ErrRaisedInFuture, ErrBadDeviceID,
	ErrBadEventID, ErrEventIDReused, ErrBadKind, ErrNoClientAt, ErrClientAtInFuture, ErrBadReason, ErrReasonRequired,
	ErrMoveLaterReason, ErrUnexpectedField, ErrNothingChanges, ErrBadCloseoutRef, ErrActionClosed, ErrActionCancelled,
	ErrNotAwaiting, ErrNotPending, ErrNotClosed, ErrCantReassign, ErrCantMoveEarlier, ErrNeedsLead,
	ErrBadCloseoutID, ErrCloseoutIDReused, ErrAlreadyAwaiting, ErrCantCloseOut, ErrBadNote, ErrNoPhotos, ErrTooManyPhotos,
	ErrBadEvidenceIDs, ErrRepeatedEvidence, ErrNoSubmittedAt, ErrSubmittedInFuture,
	ErrEvidenceNotFound, ErrEvidenceRejected, ErrEvidenceNotVerified, ErrEvidenceTooOld, ErrEvidenceIsSource,
	ErrBadState, ErrBadSource, ErrBadOwner, ErrBadLimit,
}

// normaliseRaise checks what can be checked without the database, and puts
// the request in the form Postgres returns it, so a retry compares equal.
func normaliseRaise(req *RaiseRequest, now time.Time) error {
	if (req.Finding == nil) == (req.Incident == nil) {
		return ErrOneSource
	}
	if f := req.Finding; f != nil {
		id, err := uuid.Parse(f.InspectionID)
		if err != nil || f.ItemKey == "" || len(f.ItemKey) > 200 {
			return ErrBadFinding
		}
		f.InspectionID = id.String()
	}
	if i := req.Incident; i != nil {
		id, err := uuid.Parse(i.IncidentID)
		if err != nil || i.Version < 1 {
			return ErrBadIncident
		}
		i.IncidentID = id.String()
	}
	if req.Severity != nil && !slices.ContainsFunc(checklists.Severities, func(l checklists.SeverityLevel) bool { return l.Key == *req.Severity }) {
		return ErrBadSeverity
	}
	if req.Incident != nil && req.Severity == nil {
		return ErrSeverityRequired
	}
	if !textOK(&req.Title, 200) {
		return ErrBadTitle
	}
	if !textOK(req.Description, 2000) {
		return ErrBadDescription
	}
	owner, err := uuid.Parse(req.OwnerID)
	if err != nil {
		return ErrBadOwnerID
	}
	req.OwnerID = owner.String()
	if !textOK(req.ResponsibleParty, 200) {
		return ErrBadParty
	}
	if req.DueOn != nil {
		d, err := time.Parse(time.DateOnly, *req.DueOn)
		if err != nil {
			return ErrBadDueOn
		}
		s := d.Format(time.DateOnly)
		req.DueOn = &s
	}
	if !textOK(req.DueReason, 500) {
		return ErrDueReasonRequired
	}
	if req.ClientRaisedAt.IsZero() {
		return ErrNoRaisedAt
	}
	req.ClientRaisedAt = micro(req.ClientRaisedAt)
	if req.ClientRaisedAt.After(now.Add(clockSkew)) {
		return ErrRaisedInFuture
	}
	return checkDevice(req.DeviceID)
}

func normaliseEvent(req *EventRequest, now time.Time) error {
	if !slices.Contains(Requests, req.Kind) {
		return ErrBadKind
	}
	if req.ClientAt.IsZero() {
		return ErrNoClientAt
	}
	req.ClientAt = micro(req.ClientAt)
	if req.ClientAt.After(now.Add(clockSkew)) {
		return ErrClientAtInFuture
	}
	if !textOK(req.Reason, 500) {
		return ErrBadReason
	}
	if err := checkDevice(req.DeviceID); err != nil {
		return err
	}
	// Each kind takes only its own fields.
	unexpected := false
	switch req.Kind {
	case DoReassign:
		unexpected = req.DueOn != nil || req.CloseoutID != nil
		if req.OwnerID == nil {
			return ErrBadOwnerID
		}
		owner, err := uuid.Parse(*req.OwnerID)
		if err != nil {
			return ErrBadOwnerID
		}
		o := owner.String()
		req.OwnerID = &o
		if !textOK(req.ResponsibleParty, 200) {
			return ErrBadParty
		}
	case DoMoveDue:
		unexpected = req.OwnerID != nil || req.ResponsibleParty != nil || req.CloseoutID != nil
		if req.DueOn == nil {
			return ErrBadDueOn
		}
		d, err := time.Parse(time.DateOnly, *req.DueOn)
		if err != nil {
			return ErrBadDueOn
		}
		s := d.Format(time.DateOnly)
		req.DueOn = &s
	case DoVerify, DoReject:
		unexpected = req.OwnerID != nil || req.ResponsibleParty != nil || req.DueOn != nil
		if req.CloseoutID == nil {
			return ErrBadCloseoutRef
		}
		c, err := uuid.Parse(*req.CloseoutID)
		if err != nil {
			return ErrBadCloseoutRef
		}
		cs := c.String()
		req.CloseoutID = &cs
	default: // reopen, cancel
		unexpected = req.OwnerID != nil || req.ResponsibleParty != nil || req.DueOn != nil || req.CloseoutID != nil
	}
	if unexpected {
		return ErrUnexpectedField
	}
	if (req.Kind == DoReject || req.Kind == DoReopen || req.Kind == DoCancel) && req.Reason == nil {
		return ErrReasonRequired
	}
	return nil
}

func normaliseCloseout(req *CloseoutRequest, now time.Time) error {
	if !textOK(req.Note, 2000) {
		return ErrBadNote
	}
	switch {
	case len(req.EvidenceIDs) == 0:
		return ErrNoPhotos
	case len(req.EvidenceIDs) > 20:
		return ErrTooManyPhotos
	}
	seen := map[string]bool{}
	for i, raw := range req.EvidenceIDs {
		e, err := uuid.Parse(raw)
		if err != nil {
			return ErrBadEvidenceIDs
		}
		if seen[e.String()] {
			return ErrRepeatedEvidence
		}
		seen[e.String()] = true
		req.EvidenceIDs[i] = e.String()
	}
	if req.ClientSubmittedAt.IsZero() {
		return ErrNoSubmittedAt
	}
	req.ClientSubmittedAt = micro(req.ClientSubmittedAt)
	if req.ClientSubmittedAt.After(now.Add(clockSkew)) {
		return ErrSubmittedInFuture
	}
	return checkDevice(req.DeviceID)
}

func checkDevice(d *string) error {
	if d != nil && (len(*d) == 0 || len(*d) > 200 || !utf8.ValidString(*d)) {
		return ErrBadDeviceID
	}
	return nil
}

func micro(t time.Time) time.Time { return t.Truncate(time.Microsecond).UTC() }

// textOK: absent, or 1 to max characters of valid UTF-8 without NUL (the
// columns count characters).
func textOK(s *string, max int) bool {
	if s == nil {
		return true
	}
	n := utf8.RuneCountInString(*s)
	return utf8.ValidString(*s) && n >= 1 && n <= max && !strings.ContainsRune(*s, 0)
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
