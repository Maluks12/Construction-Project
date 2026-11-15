package incidents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/deadlines"
	"construction_db/internal/regimes"
)

// The live states the site list can filter on.
var liveStates = []string{deadlines.StateOpen, deadlines.StateDueNow, deadlines.StateOverdue, deadlines.StateSatisfied, deadlines.StateSatisfiedLate}

// ListDeadlines returns a site's live deadlines, soonest due first. state
// filters on one of liveStates. Works on an archived site.
func (s *Service) ListDeadlines(ctx context.Context, orgID, userID, role, siteID, state string, limit int) ([]deadlines.Deadline, error) {
	if state != "" && !slices.Contains(liveStates, state) {
		return nil, ErrBadState
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}
	var out []deadlines.Deadline
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		all, err := deadlines.ListLiveForSite(ctx, tx, orgID, site.ID, time.Now())
		if err != nil {
			return err
		}
		out = []deadlines.Deadline{}
		for _, d := range all {
			if state == "" || d.State == state {
				out = append(out, d)
			}
			if len(out) == limit {
				break
			}
		}
		return nil
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list deadlines: %w", err)
	}
	return out, nil
}

// GetDeadline returns one deadline row with its duty's satisfactions.
func (s *Service) GetDeadline(ctx context.Context, orgID, userID, role, siteID, deadlineID string) (*DeadlineDetail, error) {
	var out *DeadlineDetail
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = detail(ctx, tx, orgID, site.ID, deadlineID)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get deadline: %w", err)
	}
	return out, nil
}

func detail(ctx context.Context, db DBTX, orgID, siteID, deadlineID string) (*DeadlineDetail, error) {
	d, err := deadlines.GetForSite(ctx, db, orgID, siteID, deadlineID, time.Now())
	if isNotFound(err) {
		return nil, ErrDeadlineNotFound
	}
	if err != nil {
		return nil, err
	}
	sats, err := deadlines.ListSatisfactions(ctx, db, orgID, d.DutyID)
	if err != nil {
		return nil, err
	}
	for i := range sats {
		sats[i].RecordedAfterDue = deadlines.RecordedAfterDue(d.Unit, d.DueAt, sats[i])
	}
	return &DeadlineDetail{Deadline: d, Satisfactions: sats}, nil
}

// errLiveMoved: the live row was superseded between reading it and recording
// against it. The caller reads again once.
var errLiveMoved = errors.New("live deadline moved")

// Satisfy records that a deadline's duty was met. Any row of the duty may be
// named (a phone may hold a superseded id): the satisfaction is recorded
// against the duty's live row, and judged against its due time. Recording it
// can start GAR 8(2)-style clocks (a death after a notice), so the
// incident's latest facts are evaluated again. created is false for an
// identical repeat. Returns the live row.
func (s *Service) Satisfy(ctx context.Context, orgID, userID, role, siteID, deadlineID, satisfactionID string, req SatisfactionRequest) (*DeadlineDetail, bool, error) {
	if _, err := uuid.Parse(deadlineID); err != nil {
		return nil, false, ErrDeadlineNotFound
	}
	sid, err := uuid.Parse(satisfactionID)
	if err != nil {
		return nil, false, ErrBadSatisfactionID
	}
	if err := normaliseSatisfaction(&req, time.Now()); err != nil {
		return nil, false, err
	}

	var (
		out     *DeadlineDetail
		created bool
	)
	for attempt := 1; ; attempt++ {
		out, created, err = s.satisfyOnce(ctx, orgID, userID, role, siteID, deadlineID, sid.String(), req)
		if !(errors.Is(err, errIDTaken) || errors.Is(err, errLiveMoved)) || attempt == 2 {
			break
		}
	}
	switch {
	case errors.Is(err, errIDTaken):
		err = ErrSatisfactionIDReused
	case errors.Is(err, errLiveMoved):
		err = ErrDeadlineWithdrawn
	}
	if err = mapRule(err); isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("satisfy deadline: %w", err)
	}
	return out, created, nil
}

func (s *Service) satisfyOnce(ctx context.Context, orgID, userID, role, siteID, deadlineID, id string, req SatisfactionRequest) (out *DeadlineDetail, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		named, err := deadlines.GetForSite(ctx, tx, orgID, sr.Site.ID, deadlineID, time.Now())
		if isNotFound(err) {
			return ErrDeadlineNotFound
		}
		if err != nil {
			return err
		}
		// Recording may add clocks computed in the incident's latest version,
		// so it queues with amendments (see lockHeader).
		if named.IncidentID != nil {
			if _, err := lockHeader(ctx, tx, orgID, sr.Site.ID, *named.IncidentID); err != nil {
				return err
			}
		}

		existing, duty, err := deadlines.GetSatisfaction(ctx, tx, orgID, id)
		switch {
		case err == nil && (duty != named.DutyID || !sameSatisfaction(existing, userID, req)):
			return ErrSatisfactionIDReused
		case err == nil:
			show := existing.DeadlineID // the duty was withdrawn since
			live, err := deadlines.GetLiveForDuty(ctx, tx, orgID, duty, time.Now())
			switch {
			case err == nil:
				show = live.ID
			case !isNotFound(err):
				return err
			}
			out, err = detail(ctx, tx, orgID, sr.Site.ID, show)
			return err
		case !isNotFound(err):
			return err
		}

		live, err := deadlines.GetLiveForDuty(ctx, tx, orgID, named.DutyID, time.Now())
		if isNotFound(err) {
			return ErrDeadlineWithdrawn
		}
		if err != nil {
			return err
		}
		if live.Satisfaction != nil {
			return ErrAlreadySatisfied
		}
		if req.SatisfiedAt.Before(live.BasisAt.Add(-clockSkew)) {
			return ErrSatisfiedBeforeBasis
		}
		if req.EvidenceID != nil {
			if err := checkPhotos(ctx, tx, orgID, sr.Site.ID, []string{*req.EvidenceID}); err != nil {
				return err
			}
		}

		err = deadlines.InsertSatisfaction(ctx, tx, orgID, sr.Site.ID, id, live.DutyID, live.ID, req.SatisfiedAt, req.Method,
			req.Reference, req.Note, req.EvidenceID, userID)
		switch {
		case isUniqueViolation(err, "deadline_satisfactions_pkey"):
			return errIDTaken
		case isRule(err, "record the satisfaction against the live deadline"):
			return errLiveMoved
		case err != nil:
			return err
		}
		if live.IncidentID != nil {
			if err := addClocksAfterSatisfaction(ctx, tx, sr.Data, sr.RegimeID, orgID, sr.Site.ID, *live.IncidentID, id); err != nil {
				return err
			}
		}
		created = true
		out, err = detail(ctx, tx, orgID, sr.Site.ID, live.ID)
		return err
	})
	return out, created, err
}

// addClocksAfterSatisfaction evaluates the incident's latest version again
// and adds any clock that applies now and didn't before (a death after the
// notice just recorded). Only additions: anything else needs a new version.
// The rows name the satisfaction, so no version lists them as its own.
func addClocksAfterSatisfaction(ctx context.Context, tx pgx.Tx, ds *regimes.DataSet, regimeID, orgID, siteID, incidentID, satisfactionID string) error {
	h, err := getHeader(ctx, tx, orgID, siteID, incidentID)
	if err != nil {
		return err
	}
	v, err := getVersion(ctx, tx, orgID, incidentID, h.LatestVersion)
	if err != nil {
		return err
	}
	people := make([]PersonInput, 0, len(v.People))
	for _, p := range v.People {
		people = append(people, p.PersonInput)
	}
	duties, err := deadlines.Duties(ctx, tx, orgID, incidentID)
	if err != nil {
		return err
	}
	clocks, err := deadlines.Applicable(ds, factsOf(v.Codes, v.OccurredAt, v.EmployerAwareAt, people), duties)
	if err != nil {
		return err
	}
	var adds []deadlines.Step
	for _, st := range deadlines.Plan(clocks, duties) {
		if st.Action == deadlines.Add {
			adds = append(adds, st)
		}
	}
	sub := deadlines.Subject{OrgID: orgID, SiteID: siteID, IncidentID: incidentID, RegimeID: regimeID, Regime: ds.Regime,
		Version: h.LatestVersion, AddedBySatisfaction: &satisfactionID}
	return deadlines.Apply(ctx, tx, sub, adds)
}

func normaliseSatisfaction(req *SatisfactionRequest, now time.Time) error {
	if req.SatisfiedAt.IsZero() {
		return ErrNoSatisfiedAt
	}
	req.SatisfiedAt = micro(req.SatisfiedAt)
	if req.SatisfiedAt.After(now.Add(clockSkew)) {
		return ErrSatisfiedInFuture
	}
	if !slices.Contains(deadlines.Methods, req.Method) {
		return ErrBadMethod
	}
	if !textOK(req.Reference, 200) {
		return ErrBadReference
	}
	if !textOK(req.Note, 2000) {
		return ErrBadNote
	}
	if req.EvidenceID != nil {
		e, err := uuid.Parse(*req.EvidenceID)
		if err != nil {
			return ErrBadEvidenceID
		}
		canonical := e.String()
		req.EvidenceID = &canonical
	}
	return nil
}

func sameSatisfaction(s *deadlines.Satisfaction, by string, req SatisfactionRequest) bool {
	return s.RecordedBy == by && s.SatisfiedAt.Equal(req.SatisfiedAt) && s.Method == req.Method &&
		equalPtr(s.Reference, req.Reference) && equalPtr(s.Note, req.Note) && equalPtr(s.EvidenceID, req.EvidenceID)
}

// Retract takes a satisfaction back (an owner or SHE officer, with a reason).
// The satisfaction stays on record; the duty is open again. Retracting again
// with the same reason is 200 (created false).
func (s *Service) Retract(ctx context.Context, orgID, userID, role, siteID, deadlineID, satisfactionID string, req RetractRequest) (*DeadlineDetail, bool, error) {
	if !isLead(role) {
		return nil, false, ErrCantRetract
	}
	if _, err := uuid.Parse(deadlineID); err != nil {
		return nil, false, ErrDeadlineNotFound
	}
	if _, err := uuid.Parse(satisfactionID); err != nil {
		return nil, false, ErrSatisfactionNotFound
	}
	if n := len([]rune(req.Reason)); n < 1 || n > 500 {
		return nil, false, ErrBadReason
	}

	var (
		out     *DeadlineDetail
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		named, err := deadlines.GetForSite(ctx, tx, orgID, site.ID, deadlineID, time.Now())
		if isNotFound(err) {
			return ErrDeadlineNotFound
		}
		if err != nil {
			return err
		}
		sat, duty, err := deadlines.GetSatisfaction(ctx, tx, orgID, satisfactionID)
		if isNotFound(err) || (err == nil && duty != named.DutyID) {
			return ErrSatisfactionNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case sat.Retraction != nil && sat.Retraction.Reason != req.Reason:
			return ErrRetractionDiffers
		case sat.Retraction == nil:
			err := deadlines.InsertRetraction(ctx, tx, orgID, site.ID, satisfactionID, duty, userID, req.Reason)
			if isUniqueViolation(err, "deadline_satisfaction_retractions_pkey") {
				return ErrRetractionDiffers // a concurrent retraction won; the client retries and sees it
			}
			if err != nil {
				return err
			}
			created = true
		}
		out, err = detail(ctx, tx, orgID, site.ID, deadlineID)
		return err
	})
	if isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("retract satisfaction: %w", err)
	}
	return out, created, nil
}
