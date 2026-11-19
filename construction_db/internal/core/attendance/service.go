package attendance

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
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/sites"
	"construction_db/internal/core/workers"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

const clockSkew = 5 * time.Minute

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	reg   *regimes.Registry
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service, reg *regimes.Registry) *Service {
	return &Service{pool: pool, sites: sitesSvc, reg: reg}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

// errRetry: an insert lost a race (its id, the worker's identity key, or the
// worker's open sign-in); once more, and the checks see the winner.
var errRetry = errors.New("retry")

func once(fn func() error) error {
	if err := fn(); !errors.Is(err, errRetry) {
		return err
	}
	return fn()
}

// Standing is the 409 for a double tap: the sign-in already standing.
type Standing struct{ SignInID string }

func (e *Standing) Error() string { return ErrAlreadySignedIn.Error() }

// SignIn signs a worker in (D11). Order of checks: the body, the site, an
// identical repeat (200, before anything else), the worker (or the
// on-the-spot entry), a sign-in already standing, the paper sheet.
func (s *Service) SignIn(ctx context.Context, orgID, userID, role, siteID, signInID string, req SignInRequest) (*SignIn, bool, error) {
	id, err := uuid.Parse(signInID)
	if err != nil {
		return nil, false, ErrBadSignInID
	}
	wid, err := uuid.Parse(req.WorkerID)
	if err != nil {
		return nil, false, ErrBadWorkerID
	}
	req.WorkerID = wid.String()
	now := time.Now()
	switch {
	case !slices.Contains(Methods, req.Method):
		return nil, false, ErrBadMethod
	case req.ClientSignedInAt.IsZero():
		return nil, false, ErrNoSignedInAt
	case req.ClientSignedInAt.After(now.Add(clockSkew)):
		return nil, false, ErrSignedInFuture
	case req.DeviceID != nil && (len(*req.DeviceID) == 0 || len(*req.DeviceID) > 200):
		return nil, false, ErrBadDeviceID
	}
	if req.NewWorker != nil {
		if ps := workers.ValidateWorker(*req.NewWorker); len(ps) > 0 {
			return nil, false, &workers.Problem{FieldProblem: workers.FieldProblem{Field: "new_worker." + ps[0].Field, Error: ps[0].Error}}
		}
	}
	req.ClientSignedInAt = micro(req.ClientSignedInAt)
	var (
		out     *SignIn
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
			if err != nil {
				return err
			}
			site := sr.Site.ID
			if existing, err := getSignIn(ctx, tx, orgID, id.String()); err == nil {
				if !sameSignIn(existing, site, userID, req) {
					return ErrIDReused
				}
				out = existing
				return s.warn(ctx, tx, orgID, out)
			} else if !isNotFound(err) {
				return err
			}
			if found, err := signInAnywhere(ctx, tx, orgID, id.String()); err != nil {
				return err
			} else if found {
				return ErrIDReused
			}

			var w *workers.Worker
			matched := false
			if req.NewWorker != nil {
				w, matched, err = workers.MatchOrCreate(ctx, tx, orgID, userID, req.WorkerID, *req.NewWorker)
				// Another phone added the same person a moment ago: retrying
				// matches them.
				if isUniqueViolation(err, "workers_identity") {
					return errRetry
				}
				if isUniqueViolation(err, "workers_pkey") {
					return workers.ErrIDReused
				}
			} else {
				w, err = workers.GetWorker(ctx, tx, orgID, req.WorkerID)
				if isNotFound(err) {
					return workers.ErrWorkerNotFound
				}
			}
			if err != nil {
				return err
			}
			if other, err := covering(ctx, tx, site, w.ID, req.ClientSignedInAt); err != nil {
				return err
			} else if other != "" {
				return &Standing{SignInID: other}
			}
			if req.Method == MethodPaper {
				if ok, err := hasSheet(ctx, tx, site, req.ClientSignedInAt, sr.Data.Regime.TimeZone); err != nil {
					return err
				} else if !ok {
					return ErrNeedsSheet
				}
			}
			err = insertSignIn(ctx, tx, orgID, site, id.String(), w.ID, userID, sr.Data.Regime.TimeZone, matched, req)
			switch {
			case isUniqueViolation(err, "site_sign_ins_pkey"), isRule(err, "already signed in at that time"):
				return errRetry
			case isRule(err, "paper sign-in needs a photo"):
				return ErrNeedsSheet
			case isRule(err, "signed in after its site was archived"):
				return ErrSiteArchived
			case err != nil:
				return err
			}
			created = true
			if out, err = getSignIn(ctx, tx, orgID, id.String()); err != nil {
				return err
			}
			return s.warn(ctx, tx, orgID, out)
		})
	})
	if errors.Is(err, errRetry) {
		err = ErrIDReused
	}
	if err = finish(err, "sign in"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

func (s *Service) warn(ctx context.Context, tx pgx.Tx, orgID string, si *SignIn) error {
	w, err := workers.WarningsFor(ctx, tx, s.reg, orgID, si.WorkerID, time.Now())
	si.Warnings = w
	return err
}

// sameSignIn: the same request again. An on-the-spot entry that matched an
// existing worker stored that worker, not the id the phone made up.
func sameSignIn(si *SignIn, siteID, by string, req SignInRequest) bool {
	sameWorker := si.WorkerID == req.WorkerID || (req.NewWorker != nil && si.MatchedExisting)
	return sameWorker && si.SiteID == siteID && si.RecordedBy == by && si.Method == req.Method &&
		si.ClientSignedInAt.Equal(req.ClientSignedInAt) && equalPtr(si.DeviceID, req.DeviceID)
}

// SignOut records the sign-out, once (D12). Repeating it is 200.
func (s *Service) SignOut(ctx context.Context, orgID, userID, role, siteID, signInID string, req SignOutRequest) (*SignIn, bool, error) {
	if !slices.Contains(Methods, req.Method) {
		return nil, false, ErrBadMethod
	}
	switch {
	case req.ClientSignedOutAt.IsZero():
		return nil, false, ErrNoSignedOutAt
	case req.ClientSignedOutAt.After(time.Now().Add(clockSkew)):
		return nil, false, ErrSignedOutFuture
	}
	req.ClientSignedOutAt = micro(req.ClientSignedOutAt)
	var (
		out     *SignIn
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		si, err := s.load(ctx, tx, orgID, userID, role, siteID, signInID)
		if err != nil {
			return err
		}
		switch {
		case si.Retraction != nil:
			return ErrRetracted
		case si.SignOut != nil:
			if !si.SignOut.ClientSignedOutAt.Equal(req.ClientSignedOutAt) || si.SignOut.Method != req.Method {
				return ErrAlreadySignedOut
			}
			out = si
			return s.warn(ctx, tx, orgID, out)
		case req.ClientSignedOutAt.Before(si.ClientSignedInAt):
			return ErrSignOutBeforeIn
		case req.ClientSignedOutAt.After(si.ClientSignedInAt.Add(OpenWindow)):
			return ErrSignOutTooLate
		}
		err = signOut(ctx, tx, orgID, si.ID, userID, req)
		switch {
		case isRule(err, "signed out after a later sign-in"):
			return ErrSignOutAfterNext
		case isRule(err, "already signed out"):
			return ErrAlreadySignedOut
		case err != nil:
			return err
		}
		created = true
		if out, err = getSignIn(ctx, tx, orgID, si.ID); err != nil {
			return err
		}
		return s.warn(ctx, tx, orgID, out)
	})
	if err = finish(err, "sign out"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// Retract withdraws a sign-in recorded by mistake ("different person"): its
// recorder or a lead, with a reason. Repeating it is 200.
func (s *Service) Retract(ctx context.Context, orgID, userID, role, siteID, signInID string, req RetractRequest) (*SignIn, bool, error) {
	if !textOK(req.Reason, 500) {
		return nil, false, ErrBadReason
	}
	var (
		out     *SignIn
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		si, err := s.load(ctx, tx, orgID, userID, role, siteID, signInID)
		if err != nil {
			return err
		}
		if r := si.Retraction; r != nil {
			if r.Reason != req.Reason {
				return ErrAlreadyRetracted
			}
			out = si
			return nil
		}
		if si.RecordedBy != userID && !isLead(role) {
			return ErrCantRetract
		}
		if err := retract(ctx, tx, orgID, si.ID, userID, req.Reason); err != nil {
			return err
		}
		created = true
		out, err = getSignIn(ctx, tx, orgID, si.ID)
		return err
	})
	if err = finish(err, "retract sign-in"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

func (s *Service) load(ctx context.Context, tx pgx.Tx, orgID, userID, role, siteID, signInID string) (*SignIn, error) {
	site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
	if err != nil {
		return nil, err
	}
	si, err := getSignIn(ctx, tx, orgID, signInID)
	if isNotFound(err) || (err == nil && si.SiteID != site.ID) {
		return nil, ErrSignInNotFound
	}
	return si, err
}

// AddSheet adds a photo of a signed paper sheet for a local day (verified
// evidence of the site). Paper sign-ins of that day need one.
func (s *Service) AddSheet(ctx context.Context, orgID, userID, role, siteID, date, evidenceID string) (*Sheet, bool, error) {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return nil, false, ErrBadDate
	}
	eid, err := uuid.Parse(evidenceID)
	if err != nil {
		return nil, false, ErrBadEvidenceID
	}
	var (
		out     *Sheet
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
			if err != nil {
				return err
			}
			if out, err = getSheet(ctx, tx, orgID, site.ID, date, eid.String()); err == nil {
				return nil
			} else if !isNotFound(err) {
				return err
			}
			st, err := evidenceStatus(ctx, tx, orgID, site.ID, eid.String())
			switch {
			case isNotFound(err):
				return ErrEvidenceNotFound
			case err != nil:
				return err
			case st == evidence.StatusRejected:
				return ErrEvidenceRejected
			case st != evidence.StatusVerified:
				return ErrEvidenceNotVerified
			}
			err = insertSheet(ctx, tx, orgID, site.ID, date, eid.String(), userID)
			if isUniqueViolation(err, "site_sign_in_sheets_pkey") {
				return errRetry
			}
			if err != nil {
				return err
			}
			created = true
			out, err = getSheet(ctx, tx, orgID, site.ID, date, eid.String())
			return err
		})
	})
	if err = finish(err, "add sign-in sheet"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// Day is a site's register for one local day (today in the site's zone when
// date is empty): sign-ins with warnings, sheets, who on the roster didn't
// sign in, overlaps with other sites, and the headcount.
func (s *Service) Day(ctx context.Context, orgID, userID, role, siteID, date string) (*Day, error) {
	if date != "" {
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return nil, ErrBadDate
		}
	}
	var d *Day
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if date == "" {
			loc, err := time.LoadLocation(sr.Data.Regime.TimeZone)
			if err != nil {
				return err
			}
			date = time.Now().In(loc).Format(time.DateOnly)
		}
		site := sr.Site.ID
		d = &Day{SiteID: site, Date: date}
		if d.SignIns, err = listSignIns(ctx, tx, orgID, site, date); err != nil {
			return err
		}
		for i := range d.SignIns {
			if err := s.warn(ctx, tx, orgID, &d.SignIns[i]); err != nil {
				return err
			}
		}
		if d.Sheets, err = listSheets(ctx, tx, orgID, site, date); err != nil {
			return err
		}
		if d.Absent, err = absent(ctx, tx, site, date, sr.Data.Regime.TimeZone, sr.Data.Reminders.RosterLapseDays); err != nil {
			return err
		}
		if d.Overlaps, err = overlaps(ctx, tx, orgID, site, date); err != nil {
			return err
		}
		d.Headcount, err = headcount(ctx, tx, site, date)
		return err
	})
	if err = finish(err, "sign-in day"); err != nil {
		return nil, err
	}
	return d, nil
}

// Roster lists a site's roster: who is expected (live entries), or every
// entry ever with all set.
func (s *Service) Roster(ctx context.Context, orgID, userID, role, siteID string, all bool) ([]RosterEntry, error) {
	var out []RosterEntry
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		site, ds := sr.Site, sr.Data
		out, err = listRoster(ctx, tx, orgID, site.ID, !all)
		markExpected(out, ds, time.Now())
		return err
	})
	if err = finish(err, "roster"); err != nil {
		return nil, err
	}
	return out, nil
}

// AddToRoster puts a register worker on the site's roster by hand.
func (s *Service) AddToRoster(ctx context.Context, orgID, userID, role, siteID, workerID string) (*RosterEntry, bool, error) {
	var (
		out     *RosterEntry
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		site, ds := sr.Site, sr.Data
		if _, err := workers.GetWorker(ctx, tx, orgID, workerID); isNotFound(err) {
			return workers.ErrWorkerNotFound
		} else if err != nil {
			return err
		}
		if out, err = liveRosterEntry(ctx, tx, orgID, site.ID, workerID); err == nil {
			out.Expected = expectedNow(*out, ds, time.Now())
			return nil
		} else if !isNotFound(err) {
			return err
		}
		if err := addToRoster(ctx, tx, orgID, site.ID, workerID, userID); err != nil {
			return err
		}
		created = true
		out, err = liveRosterEntry(ctx, tx, orgID, site.ID, workerID)
		if err == nil {
			out.Expected = expectedNow(*out, ds, time.Now())
		}
		return err
	})
	if err = finish(err, "add to roster"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// RemoveFromRoster ends a worker's live roster entry, with a reason.
func (s *Service) RemoveFromRoster(ctx context.Context, orgID, userID, role, siteID, workerID string, req RemoveRequest) (*RosterEntry, error) {
	if !textOK(req.Reason, 500) {
		return nil, ErrBadReason
	}
	var out *RosterEntry
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		site, ds := sr.Site, sr.Data
		e, err := liveRosterEntry(ctx, tx, orgID, site.ID, workerID)
		if isNotFound(err) {
			return ErrNotOnRoster
		}
		if err != nil {
			return err
		}
		if err := removeFromRoster(ctx, tx, orgID, e.ID, userID, req.Reason); err != nil {
			return err
		}
		all, err := listRoster(ctx, tx, orgID, site.ID, false)
		for i := range all {
			if all[i].ID == e.ID {
				out = &all[i]
				out.Expected = expectedNow(*out, ds, time.Now())
			}
		}
		return err
	})
	if err = finish(err, "remove from roster"); err != nil {
		return nil, err
	}
	return out, nil
}

func finish(err error, prefix string) error {
	if err == nil || isSentinel(err) {
		return err
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func isSentinel(err error) bool {
	var st *Standing
	var p *workers.Problem
	if errors.As(err, &st) || errors.As(err, &p) || errors.Is(err, sites.ErrSiteNotFound) ||
		errors.Is(err, workers.ErrWorkerNotFound) || errors.Is(err, workers.ErrIDReused) {
		return true
	}
	return slices.ContainsFunc(Sentinels, func(s error) bool { return errors.Is(err, s) })
}

// Sentinels are the errors the handlers send; TestAPIDoc checks each is quoted.
var Sentinels = []error{
	ErrSignInNotFound, ErrBadSignInID, ErrBadWorkerID, ErrBadMethod, ErrNoSignedInAt, ErrSignedInFuture, ErrNoSignedOutAt,
	ErrSignedOutFuture, ErrSignOutBeforeIn, ErrSignOutTooLate, ErrSignOutAfterNext, ErrBadDeviceID, ErrBadReason, ErrBadDate,
	ErrBadEvidenceID, ErrIDReused, ErrAlreadySignedIn, ErrAlreadySignedOut, ErrAlreadyRetracted, ErrRetracted, ErrNeedsSheet,
	ErrSiteArchived, ErrCantRetract, ErrEvidenceNotFound, ErrEvidenceRejected, ErrEvidenceNotVerified, ErrNotOnRoster,
}

func micro(t time.Time) time.Time { return t.Truncate(time.Microsecond).UTC() }

func textOK(s string, max int) bool {
	n := utf8.RuneCountInString(s)
	return utf8.ValidString(s) && n >= 1 && n <= max && !strings.ContainsRune(s, 0) && strings.TrimSpace(s) != ""
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// expectedNow: a live entry added by hand, or one whose worker signed in on
// the site within the regime's roster_lapse_days, as of today in its zone.
// site_absent (000032) applies the same rule to any day.
func expectedNow(e RosterEntry, ds *regimes.DataSet, now time.Time) bool {
	if e.RemovedAt != nil {
		return false
	}
	if e.AddedVia == "manual" {
		return true
	}
	loc, err := time.LoadLocation(ds.Regime.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	since := now.In(loc).AddDate(0, 0, -ds.Reminders.RosterLapseDays).Format(time.DateOnly)
	return e.LastSignInOn != nil && *e.LastSignInOn >= since
}

func markExpected(es []RosterEntry, ds *regimes.DataSet, now time.Time) {
	for i := range es {
		es[i].Expected = expectedNow(es[i], ds, now)
	}
}
