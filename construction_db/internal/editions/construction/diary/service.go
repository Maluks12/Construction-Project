package diary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/evidence"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/database"
)

// The edition whose sites have a diary (Rev 4 §5.2).
const edition = "construction"

// A device clock may run this far ahead of the server; the database allows
// the same.
const clockSkew = 5 * time.Minute

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service) *Service {
	return &Service{pool: pool, sites: sitesSvc}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

// site resolves the site in tx and its regime's zone. A site whose regime
// isn't this edition's is not found (construction_db/CLAUDE.md).
func (s *Service) site(ctx context.Context, tx pgx.Tx, orgID, userID, role, siteID string) (id, zone string, err error) {
	sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
	if err != nil {
		return "", "", err
	}
	if sr.Data.Regime.Edition != edition {
		return "", "", sites.ErrSiteNotFound
	}
	return sr.Site.ID, sr.Data.Regime.TimeZone, nil
}

// errRetry: an insert lost a race on its primary key to an identical
// request; once more, and the repeat check answers it.
var errRetry = errors.New("retry")

func once(fn func() error) error {
	if err := fn(); !errors.Is(err, errRetry) {
		return err
	}
	return fn()
}

func parseDate(date string) (string, error) {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return "", ErrBadDate
	}
	return d.Format(time.DateOnly), nil
}

// PutRevision saves a whole diary under a revision id the device chose
// (created true), creating the day's diary with its first revision. An
// identical repeat answers 200 before any other check. The diary row is
// locked before the closing checks, as the database's trigger does.
func (s *Service) PutRevision(ctx context.Context, orgID, userID, role, siteID, date, revisionID string, req RevisionRequest) (*Diary, bool, error) {
	id, err := uuid.Parse(revisionID)
	if err != nil {
		return nil, false, ErrBadRevisionID
	}
	if date, err = parseDate(date); err != nil {
		return nil, false, err
	}
	if err := normaliseRevision(&req, time.Now()); err != nil {
		return nil, false, err
	}
	var (
		d       *Diary
		created bool
	)
	err = once(func() error {
		d, created, err = s.putRevisionOnce(ctx, orgID, userID, role, siteID, date, id.String(), req)
		return err
	})
	if errors.Is(err, errRetry) {
		err = ErrRevisionIDReused
	}
	d, err = finish(d, err, "save diary")
	return d, created, err
}

func (s *Service) putRevisionOnce(ctx context.Context, orgID, userID, role, siteID, date, id string, req RevisionRequest) (d *Diary, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, zone, err := s.site(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		days, err := dayOf(date, zone)
		if err != nil {
			return err
		}
		// Sections are checked against the day, so after the site.
		if err := checkSections(&req.Sections, days); err != nil {
			return err
		}
		sections, err := json.Marshal(req.Sections)
		if err != nil {
			return err
		}

		if existing, err := getRevision(ctx, tx, orgID, id); err == nil {
			if existing.SiteID != site || existing.DiaryDate != date || !sameRevision(existing, userID, req) {
				return ErrRevisionIDReused
			}
			d, err = view(ctx, tx, orgID, site, date)
			return err
		} else if !isNotFound(err) {
			return err
		}

		now := time.Now()
		after := !req.ClientSaved.Before(days.closes)
		switch {
		case days.opens.After(now.Add(clockSkew)):
			return ErrDayNotBegun
		case req.ClientSaved.Before(days.opens):
			return ErrSavedBeforeDay
		case after && req.LateReason == nil:
			return ErrLateReason
		case !after && req.LateReason != nil:
			return ErrNotLate
		case !after && !now.Before(days.closes.Add(GraceAfterClose)):
			return ErrPastGrace
		}

		err = ensureHeader(ctx, tx, orgID, site, date, zone, userID)
		if isRule(err, "the day began after its site was archived") {
			return ErrDayAfterArchive
		}
		if err != nil {
			return err
		}
		h, err := lockHeader(ctx, tx, orgID, site, date)
		if err != nil {
			return err
		}
		if after && h.RevisionCount > 0 {
			return ErrDiaryClosed
		}
		if b := req.BaseRevisionID; b != nil {
			if ok, err := isRevisionOf(ctx, tx, site, date, *b); err != nil {
				return err
			} else if !ok {
				return ErrUnknownBase
			}
		}
		if err := checkPhotos(ctx, tx, orgID, site, req.EvidenceIDs); err != nil {
			return err
		}
		err = insertRevision(ctx, tx, orgID, site, date, id, userID, req, sections)
		if isUniqueViolation(err, "site_diary_revisions_pkey") {
			return errRetry
		}
		if err != nil {
			return err
		}
		created = true
		d, err = view(ctx, tx, orgID, site, date)
		return err
	})
	return d, created, err
}

func normaliseRevision(req *RevisionRequest, now time.Time) error {
	if b := req.BaseRevisionID; b != nil {
		u, err := uuid.Parse(*b)
		if err != nil {
			return ErrBadBase
		}
		*b = u.String()
	}
	w := req.Weather
	switch {
	case w == nil || len(w.Conditions) == 0:
		return ErrNoWeather
	case !optText(w.Note, 500):
		return ErrBadWeatherNote
	case req.Headcount == nil || *req.Headcount < 0 || *req.Headcount > 10000:
		return ErrNoHeadcount
	case !text(req.WorkDone, 4000):
		return ErrBadWorkDone
	case req.ClientSaved.IsZero():
		return ErrNoSavedAt
	case req.ClientSaved.After(now.Add(clockSkew)):
		return ErrSavedInFuture
	case req.LateReason != nil && !text(*req.LateReason, 500):
		return ErrLateReason
	case !deviceOK(req.DeviceID):
		return ErrBadDeviceID
	}
	seen := map[string]bool{}
	for _, c := range w.Conditions {
		if !slices.Contains(Conditions, c) || seen[c] {
			return ErrBadCondition
		}
		seen[c] = true
	}
	req.ClientSaved = micro(req.ClientSaved)
	var err error
	req.EvidenceIDs, err = normaliseEvidence(req.EvidenceIDs)
	return err
}

// normaliseEvidence: canonical UUIDs, each once, sorted (the order a stored
// revision reads back in).
func normaliseEvidence(in []string) ([]string, error) {
	if len(in) > 50 {
		return nil, ErrBadEvidenceIDs
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		u, err := uuid.Parse(s)
		if err != nil || slices.Contains(out, u.String()) {
			return nil, ErrBadEvidenceIDs
		}
		out = append(out, u.String())
	}
	slices.Sort(out)
	return out, nil
}

// checkPhotos: verified evidence on the site. Pending is 409 (retry once it
// is sealed); missing or another site's, and rejected, are 400.
func checkPhotos(ctx context.Context, db DBTX, orgID, siteID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	found, err := evidenceStatuses(ctx, db, orgID, siteID, ids)
	if err != nil {
		return err
	}
	var missing, rejected, pending []string
	for _, id := range ids {
		switch found[id] {
		case "":
			missing = append(missing, id)
		case evidence.StatusRejected:
			rejected = append(rejected, id)
		case evidence.StatusPending:
			pending = append(pending, id)
		}
	}
	for _, c := range []struct {
		ids []string
		err error
	}{{missing, ErrEvidenceNotFound}, {rejected, ErrEvidenceRejected}, {pending, ErrEvidenceNotVerified}} {
		if len(c.ids) > 0 {
			return &EvidenceError{Err: c.err, IDs: c.ids}
		}
	}
	return nil
}

func sameRevision(r *Revision, by string, req RevisionRequest) bool {
	return r.SavedBy == by && equalPtr(r.BaseRevisionID, req.BaseRevisionID) && slices.Equal(r.Weather.Conditions, req.Weather.Conditions) &&
		equalPtr(r.Weather.Note, req.Weather.Note) && r.Headcount == *req.Headcount && r.WorkDone == req.WorkDone &&
		sameSections(r.Sections, req.Sections) && slices.Equal(r.EvidenceIDs, req.EvidenceIDs) &&
		r.ClientSavedAt.Equal(req.ClientSaved) && equalPtr(r.LateReason, req.LateReason) && equalPtr(r.DeviceID, req.DeviceID)
}

// sameSections compares by their JSON, the form they are stored in.
func sameSections(a, b Sections) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

// PutAddendum adds to a closed diary under an id the device chose. An
// identical repeat answers 200 before any other check.
func (s *Service) PutAddendum(ctx context.Context, orgID, userID, role, siteID, date, addendumID string, req AddendumRequest) (*Addendum, bool, error) {
	id, err := uuid.Parse(addendumID)
	if err != nil {
		return nil, false, ErrBadAddendumID
	}
	if date, err = parseDate(date); err != nil {
		return nil, false, err
	}
	now := time.Now()
	switch {
	case !text(req.Reason, 500):
		return nil, false, ErrBadReason
	case !optText(req.Note, 4000):
		return nil, false, ErrBadNote
	case req.ClientAdded.IsZero():
		return nil, false, ErrNoAddedAt
	case req.ClientAdded.After(now.Add(clockSkew)):
		return nil, false, ErrAddedInFuture
	case !deviceOK(req.DeviceID):
		return nil, false, ErrBadDeviceID
	}
	req.ClientAdded = micro(req.ClientAdded)
	if req.EvidenceIDs, err = normaliseEvidence(req.EvidenceIDs); err != nil {
		return nil, false, err
	}
	if req.Note == nil && req.Sections.empty() && len(req.EvidenceIDs) == 0 {
		return nil, false, ErrAddsNothing
	}
	var (
		a       *Addendum
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			site, zone, err := s.site(ctx, tx, orgID, userID, role, siteID)
			if err != nil {
				return err
			}
			days, err := dayOf(date, zone)
			if err != nil {
				return err
			}
			if err := checkSections(&req.Sections, days); err != nil {
				return err
			}
			sections, err := json.Marshal(req.Sections)
			if err != nil {
				return err
			}
			if existing, err := getAddendum(ctx, tx, orgID, id.String()); err == nil {
				if existing.SiteID != site || existing.DiaryDate != date || !sameAddendum(existing, userID, req) {
					return ErrAddendumIDReused
				}
				a = existing
				return nil
			} else if !isNotFound(err) {
				return err
			}
			h, _, err := getHeader(ctx, tx, orgID, site, date)
			if isNotFound(err) {
				return ErrDiaryNotFound
			}
			if err != nil {
				return err
			}
			if !h.Closed {
				return ErrDiaryOpen
			}
			if err := checkPhotos(ctx, tx, orgID, site, req.EvidenceIDs); err != nil {
				return err
			}
			err = insertAddendum(ctx, tx, orgID, site, date, id.String(), userID, req, sections)
			if isUniqueViolation(err, "site_diary_addenda_pkey") {
				return errRetry
			}
			if err != nil {
				return err
			}
			created = true
			a, err = getAddendum(ctx, tx, orgID, id.String())
			return err
		})
	})
	if errors.Is(err, errRetry) {
		err = ErrAddendumIDReused
	}
	if err = mapRule(err); isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("add to diary: %w", err)
	}
	return a, created, nil
}

func sameAddendum(a *Addendum, by string, req AddendumRequest) bool {
	return a.AddedBy == by && a.Reason == req.Reason && equalPtr(a.Note, req.Note) && sameSections(a.Sections, req.Sections) &&
		slices.Equal(a.EvidenceIDs, req.EvidenceIDs) && a.ClientAddedAt.Equal(req.ClientAdded) && equalPtr(a.DeviceID, req.DeviceID)
}

// Get returns a day's diary: its current revision, conflict, addenda, and
// the day's toolbox talks and incidents by id. Works on archived sites.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, date string) (*Diary, error) {
	date, err := parseDate(date)
	if err != nil {
		return nil, err
	}
	var d *Diary
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, _, err := s.site(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		d, err = view(ctx, tx, orgID, site, date)
		if isNotFound(err) {
			return ErrDiaryNotFound
		}
		return err
	})
	return finish(d, err, "get diary")
}

// Revisions returns every revision of a day's diary in arrival order, for
// showing a conflict side by side.
func (s *Service) Revisions(ctx context.Context, orgID, userID, role, siteID, date string) ([]Revision, error) {
	date, err := parseDate(date)
	if err != nil {
		return nil, err
	}
	var out []Revision
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, _, err := s.site(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = listRevisions(ctx, tx, orgID, site, date)
		if err == nil && len(out) == 0 {
			return ErrDiaryNotFound
		}
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("diary revisions: %w", err)
	}
	return out, nil
}

// List is a site's diaries, newest day first, between from and to
// (inclusive, either optional). The query is checked before the site.
func (s *Service) List(ctx context.Context, orgID, userID, role, siteID, from, to string, limit int) ([]DiarySummary, error) {
	var err error
	if from != "" {
		if from, err = parseDate(from); err != nil {
			return nil, ErrBadRange
		}
	}
	if to != "" {
		if to, err = parseDate(to); err != nil {
			return nil, ErrBadRange
		}
	}
	if from != "" && to != "" && from > to {
		return nil, ErrBadRange
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}
	var out []DiarySummary
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, _, err := s.site(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = listDiaries(ctx, tx, orgID, site, from, to, limit)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list diaries: %w", err)
	}
	return out, nil
}

// Firms lists the firm names used on the site before (D9).
func (s *Service) Firms(ctx context.Context, orgID, userID, role, siteID string) ([]string, error) {
	var out []string
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, _, err := s.site(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = firms(ctx, tx, orgID, site)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("diary firms: %w", err)
	}
	return out, nil
}

func view(ctx context.Context, db DBTX, orgID, siteID, date string) (*Diary, error) {
	h, head, err := getHeader(ctx, db, orgID, siteID, date)
	if err != nil {
		return nil, err
	}
	d := &Diary{DiaryHeader: *h}
	cur, err := getRevision(ctx, db, orgID, head)
	if err != nil {
		return nil, fmt.Errorf("head revision: %w", err)
	}
	d.Current = *cur
	if d.HeadcountSuggestedNow, err = headcountNow(ctx, db, siteID, date); err != nil {
		return nil, fmt.Errorf("headcount: %w", err)
	}
	if d.Addenda, err = listAddenda(ctx, db, orgID, siteID, date); err != nil {
		return nil, err
	}
	if d.ToolboxTalkIDs, d.IncidentIDs, err = linked(ctx, db, orgID, siteID, date, h.TimeZone); err != nil {
		return nil, err
	}
	return d, nil
}

func finish(d *Diary, err error, prefix string) (*Diary, error) {
	if err = mapRule(err); isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}
	return d, nil
}

// mapRule turns a rule the database enforced into the API's error, for the
// ones a race can reach past the checks above (midnight passing between the
// check and the insert, an archiving).
func mapRule(err error) error {
	switch {
	case isRule(err, "closed at midnight; add an addendum"):
		return ErrDiaryClosed
	case isRule(err, "received more than 48 hours after"):
		return ErrPastGrace
	case isRule(err, "is still open; edit it instead"):
		return ErrDiaryOpen
	case isRule(err, "the day began after its site was archived"):
		return ErrDayAfterArchive
	case isRule(err, "site_diary_revisions_late_reason"):
		return ErrLateReason
	}
	return err
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
	ErrDiaryNotFound, ErrBadDate, ErrDayNotBegun, ErrDayAfterArchive, ErrBadRevisionID, ErrRevisionIDReused, ErrBadBase,
	ErrUnknownBase, ErrNoWeather, ErrBadCondition, ErrBadWeatherNote, ErrNoHeadcount, ErrBadWorkDone, ErrNoSavedAt,
	ErrSavedInFuture, ErrSavedBeforeDay, ErrLateReason, ErrNotLate, ErrDiaryClosed, ErrPastGrace, ErrBadDeviceID,
	ErrBadAddendumID, ErrAddendumIDReused, ErrBadReason, ErrBadNote, ErrAddsNothing, ErrNoAddedAt, ErrAddedInFuture, ErrDiaryOpen,
	ErrBadEvidenceIDs, ErrEvidenceNotFound, ErrEvidenceRejected, ErrEvidenceNotVerified,
	ErrBadRowID, ErrBadText, ErrBadNumber, ErrBadChoice, ErrBadTime, ErrTwoOwnTeams, ErrTooManyRows, ErrMissingTime, ErrEndBeforeStart,
	ErrBadRange, ErrBadLimit,
}

func deviceOK(s *string) bool { return s == nil || (len(*s) >= 1 && len(*s) <= 200) }

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
