package reg4

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

const clockSkew = 5 * time.Minute

type Service struct {
	deps edition.Deps
}

func NewService(deps edition.Deps) *Service { return &Service{deps: deps} }

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.deps.Pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

var errIDTaken = errors.New("id taken")

// Submit stores version n of site work under an id the device chose, and
// plans its deadlines in the same transaction. created is false for an
// identical repeat (200).
func (s *Service) Submit(ctx context.Context, orgID, userID, role, siteID, workID, version string, req VersionRequest) (*SiteWork, bool, error) {
	id, err := uuid.Parse(workID)
	if err != nil {
		return nil, false, ErrBadID
	}
	n, err := strconv.Atoi(version)
	if err != nil || n < 1 {
		return nil, false, ErrBadVersion
	}
	if err := normalise(&req, n, s.deps.Clock.Now()); err != nil {
		return nil, false, err
	}
	var (
		out     *SiteWork
		created bool
	)
	for attempt := 1; ; attempt++ {
		out, created, err = s.submitOnce(ctx, orgID, userID, role, siteID, id.String(), n, req)
		if !errors.Is(err, errIDTaken) || attempt == 2 {
			break
		}
	}
	if errors.Is(err, errIDTaken) {
		err = ErrIDReused
	}
	if err = mapRule(err); isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("submit site work version: %w", err)
	}
	return out, created, nil
}

func (s *Service) submitOnce(ctx context.Context, orgID, userID, role, siteID, id string, n int, req VersionRequest) (out *SiteWork, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.deps.Sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		ds := sr.Data
		// An amendment locks the record before reading its deadlines, so a
		// satisfaction in progress finishes first and the plan sees it.
		read := getHeader
		if n > 1 {
			read = lockHeader
		}
		h, err := read(ctx, tx, orgID, sr.Site.ID, id)
		switch {
		case isNotFound(err):
			h = nil
			if other, err := onAnotherSite(ctx, tx, orgID, id); err != nil {
				return err
			} else if other {
				return ErrIDReused
			}
		case err != nil:
			return err
		}

		// An identical repeat answers 200 before any other check, so a retry
		// after the site was archived still succeeds.
		if h != nil && n <= h.LatestVersion {
			stored, err := getVersion(ctx, tx, orgID, id, n)
			if err != nil {
				return err
			}
			if !sameVersion(stored, userID, req) {
				if n == 1 {
					return ErrIDReused
				}
				return ErrVersionDiffers
			}
			out, err = s.view(ctx, tx, ds, orgID, h, n)
			return err
		}
		switch {
		case h == nil && n > 1:
			return ErrNotFound
		case h != nil && n > h.LatestVersion+1:
			return &NotNextError{LatestVersion: h.LatestVersion}
		case h != nil && h.RegimeID != sr.RegimeID:
			return fmt.Errorf("site work %s is under regime %s, its site under %s", id, h.RegimeID, sr.RegimeID)
		}

		archivedAt, err := siteArchivedAt(ctx, tx, orgID, sr.Site.ID)
		if err != nil {
			return err
		}
		if archivedAt != nil && req.ClientSubmittedAt.After(*archivedAt) {
			return ErrSiteArchived
		}
		var unknown []string
		for _, f := range req.Facts {
			if _, ok := ds.SiteWork.Fact(f); !ok {
				unknown = append(unknown, f)
			}
		}
		if len(unknown) > 0 {
			return &FactsError{Facts: unknown}
		}

		ref := deadlines.Ref{Kind: deadlines.SubjectSiteWork, ID: id}
		duties := []deadlines.Duty{}
		if h != nil {
			if duties, err = deadlines.Duties(ctx, tx, orgID, ref); err != nil {
				return err
			}
		}
		starts, err := deadlines.StartOfDay(req.StartsOn, ds.Regime.TimeZone)
		if err != nil {
			return err
		}
		clocks, err := deadlines.ApplicableSiteWork(ds, deadlines.SiteWorkFacts{Facts: req.Facts, StartsAt: starts, Cancelled: req.Status == "cancelled"})
		if err != nil {
			return err
		}
		steps := deadlines.Plan(clocks, duties)
		sub := deadlines.Subject{OrgID: orgID, SiteID: sr.Site.ID, Ref: ref, RegimeID: sr.RegimeID, Regime: ds.Regime, Version: n}
		// The database refuses this at commit too (000046); checking first
		// gives a 403 before anything is written.
		if !isLead(role) {
			weakens, err := deadlines.Weakens(ctx, tx, ds, sub, duties, steps, s.deps.Clock.Now())
			if err != nil {
				return err
			}
			if weakens {
				return ErrWeakeningNeedsLead
			}
		}

		if h == nil {
			err := insertHeader(ctx, tx, orgID, sr.Site.ID, sr.RegimeID, id, userID)
			if isUniqueViolation(err, "site_work_pkey") {
				return errIDTaken
			}
			if err != nil {
				return err
			}
		}
		err = insertVersion(ctx, tx, orgID, sr.Site.ID, id, n, userID, req)
		if isUniqueViolation(err, "site_work_versions_pkey") {
			return errIDTaken
		}
		if err != nil {
			return err
		}
		if err := deadlines.Apply(ctx, tx, sub, steps); err != nil {
			return err
		}
		created = true
		if h, err = getHeader(ctx, tx, orgID, sr.Site.ID, id); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, ds, orgID, h, n)
		return err
	})
	return out, created, err
}

// Get returns site work as of a version (the latest by default), with every
// deadline row. Works on an archived site.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, workID, version string) (*SiteWork, error) {
	if _, err := uuid.Parse(workID); err != nil {
		return nil, ErrNotFound
	}
	n := 0
	if version != "" {
		v, err := strconv.Atoi(version)
		if err != nil || v < 1 {
			return nil, ErrBadVersion
		}
		n = v
	}
	var out *SiteWork
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.deps.Sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		h, err := getHeader(ctx, tx, orgID, sr.Site.ID, workID)
		if isNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if n == 0 {
			n = h.LatestVersion
		}
		if n > h.LatestVersion {
			return ErrVersionNotFound
		}
		out, err = s.view(ctx, tx, sr.Data, orgID, h, n)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get site work: %w", err)
	}
	return out, nil
}

// List returns the site's site work, the next start first.
func (s *Service) List(ctx context.Context, orgID, userID, role, siteID, status string, limit int) ([]Summary, error) {
	if status != "" && !slices.Contains(Statuses, status) {
		return nil, ErrBadStatus
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}
	var out []Summary
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.deps.Sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = listSummaries(ctx, tx, orgID, site.ID, status, s.deps.Clock.Now(), limit)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list site work: %w", err)
	}
	return out, nil
}

// Facts is the site regime's catalogue for the form.
func (s *Service) Facts(ctx context.Context, orgID, userID, role, siteID string) (*Catalogue, error) {
	ds, err := s.deps.Sites.Regime(ctx, orgID, userID, role, siteID)
	if err != nil {
		if isSentinel(err) {
			return nil, err
		}
		return nil, fmt.Errorf("site work facts: %w", err)
	}
	c := &Catalogue{Facts: []Fact{}, Rules: []Rule{}}
	for _, f := range ds.SiteWork.Facts {
		c.Facts = append(c.Facts, Fact{Key: f.Key, Label: f.Label, SourceRef: f.SourceRef})
	}
	for _, r := range ds.SiteWork.Rules {
		d, ok := ruleOf(ds, r.RuleKey)
		if !ok {
			continue
		}
		var caveat *string
		if d.Caveat != "" {
			caveat = &d.Caveat
		}
		c.Rules = append(c.Rules, Rule{RuleKey: r.RuleKey, Label: d.Label, Period: d.Period, Unit: d.Unit, DutyHolder: d.DutyHolder,
			Caveat: caveat, SourceRef: d.SourceRef, WhenAny: r.WhenAny, UnlessFacts: r.UnlessFacts, UnlessRules: r.UnlessRules})
	}
	return c, nil
}

func ruleOf(ds *regimes.DataSet, key string) (regimes.DeadlineRule, bool) {
	i := slices.IndexFunc(ds.Deadlines, func(r regimes.DeadlineRule) bool { return r.Key == key })
	if i < 0 {
		return regimes.DeadlineRule{}, false
	}
	return ds.Deadlines[i], true
}

func (s *Service) view(ctx context.Context, db DBTX, ds *regimes.DataSet, orgID string, h *header, n int) (*SiteWork, error) {
	v, err := getVersion(ctx, db, orgID, h.ID, n)
	if isNotFound(err) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := deadlines.ListForSubject(ctx, db, orgID, deadlines.Ref{Kind: deadlines.SubjectSiteWork, ID: h.ID}, s.deps.Clock.Now())
	if err != nil {
		return nil, err
	}
	out := &SiteWork{ID: h.ID, SiteID: h.SiteID, CreatedBy: h.CreatedBy, CreatedAt: h.CreatedAt, LatestVersion: h.LatestVersion,
		Version: v.Version, Changes: changesIn(rows, n), Deadlines: rows}
	out.Version.Facts = []Fact{}
	for _, k := range v.FactKeys {
		f, _ := ds.SiteWork.Fact(k)
		out.Version.Facts = append(out.Version.Facts, Fact{Key: k, Label: f.Label, SourceRef: f.SourceRef})
	}
	for _, d := range rows {
		if d.ComputedInVersion == n && d.OverdueWhenComputed {
			out.Version.LateWhenRecorded = true
		}
	}
	return out, nil
}

// changesIn reads what version n did from the rows themselves.
func changesIn(rows []deadlines.Deadline, n int) Changes {
	c := Changes{Added: []string{}, Superseded: []string{}, Withdrawn: []string{}, Carried: []string{}}
	replaced, recomputed := map[string]bool{}, map[string]bool{}
	for _, d := range rows {
		if d.ClosedInVersion != nil && *d.ClosedInVersion == n && d.ClosedAs != nil && *d.ClosedAs == "superseded" && d.SupersededBy != nil {
			replaced[*d.SupersededBy] = true
		}
		// A holiday's replacement is computed in the latest version but isn't its doing.
		if d.ClosedAs != nil && *d.ClosedAs == "recomputed" && d.SupersededBy != nil {
			recomputed[*d.SupersededBy] = true
		}
	}
	for _, d := range rows {
		switch {
		case recomputed[d.ID]:
			if d.ClosedAs == nil {
				c.Carried = append(c.Carried, d.ID)
			}
		case d.ClosedInVersion != nil && *d.ClosedInVersion == n && d.ClosedAs != nil && *d.ClosedAs == "superseded":
			c.Superseded = append(c.Superseded, d.ID)
		case d.ClosedInVersion != nil && *d.ClosedInVersion == n && d.ClosedAs != nil && *d.ClosedAs == "withdrawn":
			c.Withdrawn = append(c.Withdrawn, d.ID)
		case d.ComputedInVersion == n && !replaced[d.ID]:
			c.Added = append(c.Added, d.ID)
		case d.ComputedInVersion < n && (d.ClosedInVersion == nil || *d.ClosedInVersion > n) && (d.ClosedAs == nil || *d.ClosedAs != "recomputed"):
			c.Carried = append(c.Carried, d.ID)
		}
	}
	return c
}

func normalise(req *VersionRequest, n int, now time.Time) error {
	if l := utf8.RuneCountInString(req.Description); !cleanText(req.Description) || l < 1 || l > 500 {
		return ErrBadDescription
	}
	start, err := time.Parse(time.DateOnly, req.StartsOn)
	if err != nil {
		return ErrBadStartsOn
	}
	if req.EndsOn != nil {
		end, err := time.Parse(time.DateOnly, *req.EndsOn)
		if err != nil || end.Before(start) {
			return ErrBadEndsOn
		}
	}
	if req.Facts == nil {
		req.Facts = []string{}
	}
	for i, f := range req.Facts {
		if slices.Index(req.Facts, f) != i {
			return ErrRepeatedFacts
		}
	}
	if req.Status == "" {
		req.Status = "planned"
	}
	if !slices.Contains(Statuses, req.Status) {
		return ErrBadStatus
	}
	if n == 1 && req.Status != "planned" {
		return ErrFirstCancelled
	}
	if req.ClientSubmittedAt.IsZero() {
		return ErrNoSubmittedAt
	}
	req.ClientSubmittedAt = req.ClientSubmittedAt.Truncate(time.Microsecond).UTC()
	if req.ClientSubmittedAt.After(now.Add(clockSkew)) {
		return ErrSubmittedInFuture
	}
	if req.DeviceID != nil && (len(*req.DeviceID) < 1 || len(*req.DeviceID) > 200 || !cleanText(*req.DeviceID)) {
		return ErrBadDeviceID
	}
	switch {
	case n == 1 && req.AmendReason != nil:
		return ErrReasonOnFirst
	case n > 1 && (req.AmendReason == nil || utf8.RuneCountInString(*req.AmendReason) < 1 ||
		utf8.RuneCountInString(*req.AmendReason) > 500 || !cleanText(*req.AmendReason)):
		return ErrReasonRequired
	}
	return nil
}

// cleanText: valid UTF-8 without NUL, which Postgres text can't hold.
func cleanText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

// sameVersion: a retry must match everything the first submission recorded.
func sameVersion(v *storedVersion, by string, req VersionRequest) bool {
	return v.SubmittedBy == by && v.Description == req.Description && v.StartsOn == req.StartsOn && equalPtr(v.EndsOn, req.EndsOn) &&
		slices.Equal(v.FactKeys, req.Facts) && v.Status == req.Status && v.ClientSubmittedAt.Equal(req.ClientSubmittedAt) &&
		equalPtr(v.DeviceID, req.DeviceID) && equalPtr(v.AmendReason, req.AmendReason)
}

func mapRule(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case isRule(err, "is not the next version"):
		return ErrNotNextVersion
	case isRule(err, "only an owner or a SHE officer can withdraw"):
		return ErrWeakeningNeedsLead
	case isRule(err, "submitted after its site was archived"):
		return ErrSiteArchived
	case isRule(err, "client_submitted_at is in the future"):
		return ErrSubmittedInFuture
	case errors.As(err, &pgErr) && pgErr.Code == "22000" && strings.HasPrefix(pgErr.Message, "holiday calendar:"):
		return ErrStartOutsideCalendar
	}
	return err
}

func isRule(err error, fragment string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, fragment)
}

func isSentinel(err error) bool {
	if errors.Is(err, sites.ErrSiteNotFound) {
		return true
	}
	for _, s := range Sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
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

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
