package incidents

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
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

// A device clock may run this far ahead of the server (as for inspection
// answers); the database allows the same.
const clockSkew = 5 * time.Minute

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	Clock clock.Clock // nil is the real time
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service) *Service {
	return &Service{pool: pool, sites: sitesSvc}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

// errIDTaken: an insert hit a primary key. Either a concurrent identical
// request committed first, or the id belongs to another organisation and RLS
// hides it. The caller looks again once.
var errIDTaken = errors.New("id taken")

// Submit stores version n of an incident under an id the device chose and
// computes its deadlines in the same transaction. Version 1 creates the
// incident. created is false for an identical repeat (200).
func (s *Service) Submit(ctx context.Context, orgID, userID, role, siteID, incidentID, version string, req SubmitVersionRequest) (*Incident, bool, error) {
	id, err := uuid.Parse(incidentID)
	if err != nil {
		return nil, false, ErrBadIncidentID
	}
	n, err := strconv.Atoi(version)
	if err != nil || n < 1 {
		return nil, false, ErrBadVersion
	}
	if err := normaliseVersion(&req, n, s.Clock.Now()); err != nil {
		return nil, false, err
	}

	var (
		inc     *Incident
		created bool
	)
	for attempt := 1; ; attempt++ {
		inc, created, err = s.submitOnce(ctx, orgID, userID, role, siteID, id.String(), n, req)
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
		return nil, false, fmt.Errorf("submit incident version: %w", err)
	}
	return inc, created, nil
}

func (s *Service) submitOnce(ctx context.Context, orgID, userID, role, siteID, id string, n int, req SubmitVersionRequest) (inc *Incident, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		site, ds := sr.Site, sr.Data

		// An amendment locks the incident before reading its deadlines, so a
		// satisfaction in progress finishes first and the plan sees it.
		read := getHeader
		if n > 1 {
			read = lockHeader
		}
		h, err := read(ctx, tx, orgID, site.ID, id)
		switch {
		case isNotFound(err):
			h = nil
			// The id may be an incident on another of the org's sites.
			if _, err := incidentSite(ctx, tx, orgID, id); err == nil {
				return ErrIDReused
			} else if !isNotFound(err) {
				return err
			}
		case err != nil:
			return err
		}

		// An identical repeat answers 200 before any other check, so a
		// retry after the site was archived (or the rules changed) still
		// succeeds.
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
			inc, err = s.view(ctx, tx, ds, orgID, h, n)
			return err
		}
		switch {
		case h == nil && n > 1:
			return ErrIncidentNotFound
		case h != nil && n > h.LatestVersion+1:
			return &NotNextError{LatestVersion: h.LatestVersion}
		case h != nil && h.RegimeID != sr.RegimeID:
			return fmt.Errorf("incident %s is under regime %s, its site under %s", id, h.RegimeID, sr.RegimeID)
		}

		archivedAt, err := siteArchivedAt(ctx, tx, orgID, site.ID)
		if err != nil {
			return err
		}
		if archivedAt != nil && req.OccurredAt.After(*archivedAt) {
			return ErrSiteArchived
		}
		if err := checkTypes(ds, req); err != nil {
			return err
		}
		if err := checkPhotos(ctx, tx, orgID, site.ID, req.EvidenceIDs); err != nil {
			return err
		}

		ref := deadlines.Ref{Kind: deadlines.SubjectIncident, ID: id}
		duties := []deadlines.Duty{}
		if h != nil {
			if duties, err = deadlines.Duties(ctx, tx, orgID, ref); err != nil {
				return err
			}
		}
		clocks, err := deadlines.Applicable(ds, factsOf(req.TaxonomyCodes, req.OccurredAt, req.EmployerAwareAt, req.People), duties)
		if err != nil {
			return err
		}
		steps := deadlines.Plan(clocks, duties)
		sub := deadlines.Subject{OrgID: orgID, SiteID: site.ID, Ref: ref, RegimeID: sr.RegimeID, Regime: ds.Regime, Version: n}

		// The database refuses this at commit too; checking first gives a
		// 403 before anything is written.
		if !isLead(role) {
			weakens, err := deadlines.Weakens(ctx, tx, ds, sub, duties, steps, s.Clock.Now())
			if err != nil {
				return err
			}
			if weakens {
				return ErrWeakeningNeedsLead
			}
		}

		if h == nil {
			err := insertHeader(ctx, tx, orgID, site.ID, sr.RegimeID, id, userID)
			if isUniqueViolation(err, "incidents_pkey") {
				return errIDTaken
			}
			if err != nil {
				return err
			}
		}
		err = insertVersion(ctx, tx, orgID, site.ID, id, n, userID, req)
		if isUniqueViolation(err, "incident_versions_pkey") {
			return errIDTaken // a concurrent submission of this version won
		}
		if err != nil {
			return err
		}
		if err := deadlines.Apply(ctx, tx, sub, steps); err != nil {
			return err
		}

		created = true
		if h, err = getHeader(ctx, tx, orgID, site.ID, id); err != nil {
			return err
		}
		inc, err = s.view(ctx, tx, ds, orgID, h, n)
		return err
	})
	return inc, created, err
}

func ruleOf(ds *regimes.DataSet, key string) (regimes.DeadlineRule, bool) {
	i := slices.IndexFunc(ds.Deadlines, func(r regimes.DeadlineRule) bool { return r.Key == key })
	if i < 0 {
		return regimes.DeadlineRule{}, false
	}
	return ds.Deadlines[i], true
}

func factsOf(codes []string, occurred time.Time, aware *time.Time, people []PersonInput) deadlines.Facts {
	f := deadlines.Facts{TaxonomyCodes: codes, OccurredAt: occurred, EmployerAwareAt: aware}
	for _, p := range people {
		f.People = append(f.People, deadlines.Person{EmployeeOfOrg: p.EmployeeOfOrg, Injured: p.Injured, DiedAt: p.DiedAt})
	}
	return f
}

// checkTypes needs the regime: every code must be one of its types, and a
// death needs a reportable one (OHSA s24(1)(a): "any person dies").
func checkTypes(ds *regimes.DataSet, req SubmitVersionRequest) error {
	var unknown []string
	reportable := false
	for _, code := range req.TaxonomyCodes {
		i := slices.IndexFunc(ds.Taxonomy, func(t regimes.IncidentType) bool { return t.Key == code })
		if i < 0 {
			unknown = append(unknown, code)
			continue
		}
		reportable = reportable || ds.Taxonomy[i].Reportable
	}
	if len(unknown) > 0 {
		return &TypesError{Codes: unknown}
	}
	if !reportable && slices.ContainsFunc(req.People, func(p PersonInput) bool { return p.DiedAt != nil }) {
		return ErrDeathNotReportable
	}
	return nil
}

// checkPhotos distinguishes a photo still uploading (409, retry later) from
// one that never will be usable (400).
func checkPhotos(ctx context.Context, db DBTX, orgID, siteID string, ids []string) error {
	statuses, err := evidenceStatuses(ctx, db, orgID, siteID, ids)
	if err != nil {
		return err
	}
	var missing, rejected, pending []string
	for _, id := range ids {
		switch statuses[id] {
		case "":
			missing = append(missing, id)
		case evidence.StatusRejected:
			rejected = append(rejected, id)
		case evidence.StatusPending:
			pending = append(pending, id)
		}
	}
	switch {
	case len(missing) > 0:
		return &EvidenceError{Err: ErrEvidenceNotFound, IDs: missing}
	case len(rejected) > 0:
		return &EvidenceError{Err: ErrEvidenceRejected, IDs: rejected}
	case len(pending) > 0:
		return &EvidenceError{Err: ErrEvidenceNotVerified, IDs: pending}
	}
	return nil
}

// normaliseVersion checks what can be checked without the database, and
// puts the request in the form Postgres returns it, so a retry compares
// equal: microsecond times in UTC, canonical UUIDs.
func normaliseVersion(req *SubmitVersionRequest, n int, now time.Time) error {
	switch {
	case len(req.TaxonomyCodes) == 0:
		return ErrNoTypes
	case len(req.TaxonomyCodes) > 10:
		return ErrTooManyTypes
	}
	seen := map[string]bool{}
	for _, c := range req.TaxonomyCodes {
		if seen[c] {
			return ErrRepeatedTypes
		}
		seen[c] = true
	}
	if req.OccurredAt.IsZero() {
		return ErrNoOccurredAt
	}
	req.OccurredAt = micro(req.OccurredAt)
	if req.OccurredAt.After(now.Add(clockSkew)) {
		return ErrOccurredInFuture
	}
	if req.EmployerAwareAt != nil {
		at := micro(*req.EmployerAwareAt)
		req.EmployerAwareAt = &at
		if at.Before(req.OccurredAt) {
			return ErrAwareBeforeIncident
		}
		if at.After(now.Add(clockSkew)) {
			return ErrAwareInFuture
		}
	}
	if req.ClientSubmittedAt.IsZero() {
		return ErrNoSubmittedAt
	}
	req.ClientSubmittedAt = micro(req.ClientSubmittedAt)
	if req.ClientSubmittedAt.After(now.Add(clockSkew)) {
		return ErrSubmittedInFuture
	}
	if !textOK(req.Location, 500) {
		return ErrBadLocation
	}
	if !textOK(req.Description, 5000) {
		return ErrBadDescription
	}
	if d := req.DeviceID; d != nil && (len(*d) == 0 || len(*d) > 200 || !utf8.ValidString(*d)) {
		return ErrBadDeviceID
	}
	switch {
	case n == 1 && req.AmendReason != nil:
		return ErrReasonOnFirst
	case n > 1 && !textOK(req.AmendReason, 500), n > 1 && req.AmendReason == nil:
		return ErrReasonRequired
	}

	if len(req.People) > 50 {
		return ErrTooManyPeople
	}
	if req.People == nil {
		req.People = []PersonInput{}
	}
	for i := range req.People {
		p := &req.People[i]
		if !textOK(p.Name, 200) || !textOK(p.Occupation, 200) || !textOK(p.EmployerName, 200) ||
			!textOK(p.BodyPart, 200) || !textOK(p.InjuryDescription, 2000) {
			return ErrBadPerson
		}
		if p.Treatment != nil && !slices.Contains(Treatments, *p.Treatment) {
			return ErrBadTreatment
		}
		if !p.Injured && (p.InjuryDescription != nil || p.BodyPart != nil || p.Treatment != nil) {
			return ErrInjuryNotInjured
		}
		if p.EmployeeOfOrg && p.EmployerName != nil {
			return ErrEmployerOfEmployee
		}
		if p.DiedAt != nil {
			died := micro(*p.DiedAt)
			p.DiedAt = &died
			switch {
			case !p.Injured:
				return ErrDeathNotInjured
			case died.Before(req.OccurredAt):
				return ErrDeathBeforeIncident
			case died.After(now.Add(clockSkew)):
				return ErrDeathInFuture
			}
		}
	}

	if len(req.EvidenceIDs) > 20 {
		return ErrTooManyPhotos
	}
	if req.EvidenceIDs == nil {
		req.EvidenceIDs = []string{}
	}
	photos := map[string]bool{}
	for i, raw := range req.EvidenceIDs {
		e, err := uuid.Parse(raw)
		if err != nil {
			return ErrBadEvidenceIDs
		}
		req.EvidenceIDs[i] = e.String()
		if photos[e.String()] {
			return ErrRepeatedEvidence
		}
		photos[e.String()] = true
	}
	return nil
}

func micro(t time.Time) time.Time { return t.Truncate(time.Microsecond).UTC() }

// textOK: absent, or 1 to max characters of valid UTF-8 (the columns count
// characters).
func textOK(s *string, max int) bool {
	if s == nil {
		return true
	}
	n := utf8.RuneCountInString(*s)
	return utf8.ValidString(*s) && n >= 1 && n <= max && !strings.ContainsRune(*s, 0)
}

// sameVersion: a retry must match everything the first submission recorded.
func sameVersion(v *storedVersion, by string, req SubmitVersionRequest) bool {
	if v.SubmittedBy != by || !slices.Equal(v.Codes, req.TaxonomyCodes) || !v.OccurredAt.Equal(req.OccurredAt) ||
		!equalTime(v.EmployerAwareAt, req.EmployerAwareAt) || !equalPtr(v.Location, req.Location) ||
		!equalPtr(v.Description, req.Description) || !v.ClientSubmittedAt.Equal(req.ClientSubmittedAt) ||
		!equalPtr(v.DeviceID, req.DeviceID) || !equalPtr(v.AmendReason, req.AmendReason) ||
		len(v.People) != len(req.People) || len(v.Evidence) != len(req.EvidenceIDs) {
		return false
	}
	for i, p := range v.People {
		q := req.People[i]
		if !equalPtr(p.Name, q.Name) || !equalPtr(p.Occupation, q.Occupation) || p.EmployeeOfOrg != q.EmployeeOfOrg ||
			!equalPtr(p.EmployerName, q.EmployerName) || p.Injured != q.Injured ||
			!equalPtr(p.InjuryDescription, q.InjuryDescription) || !equalPtr(p.BodyPart, q.BodyPart) ||
			!equalPtr(p.Treatment, q.Treatment) || !equalTime(p.DiedAt, q.DiedAt) {
			return false
		}
	}
	stored := make([]string, 0, len(v.Evidence))
	for _, e := range v.Evidence {
		stored = append(stored, e.EvidenceID)
	}
	return slices.Equal(slices.Sorted(slices.Values(stored)), slices.Sorted(slices.Values(req.EvidenceIDs)))
}

// view builds the incident as of version n.
func (s *Service) view(ctx context.Context, db DBTX, ds *regimes.DataSet, orgID string, h *header, n int) (*Incident, error) {
	v, err := getVersion(ctx, db, orgID, h.ID, n)
	if err != nil {
		return nil, err
	}
	inc := &Incident{ID: h.ID, SiteID: h.SiteID, ReportedBy: h.ReportedBy, CreatedAt: h.CreatedAt, LatestVersion: h.LatestVersion,
		Version: v.Version}
	inc.Version.Types, inc.Reportable = typesOf(ds, v.Codes)
	if inc.Deadlines, err = deadlines.ListForSubject(ctx, db, orgID, deadlines.Ref{Kind: deadlines.SubjectIncident, ID: h.ID}, s.Clock.Now()); err != nil {
		return nil, err
	}
	inc.Changes = changesIn(inc.Deadlines, n)
	if inc.WitnessStatements, err = listStatements(ctx, db, orgID, h.ID); err != nil {
		return nil, err
	}
	return inc, nil
}

func typesOf(ds *regimes.DataSet, codes []string) ([]IncidentType, bool) {
	out := make([]IncidentType, 0, len(codes))
	reportable := false
	for _, c := range codes {
		t := IncidentType{Key: c, Label: c}
		if i := slices.IndexFunc(ds.Taxonomy, func(x regimes.IncidentType) bool { return x.Key == c }); i >= 0 {
			t.Label, t.Reportable = ds.Taxonomy[i].Label, ds.Taxonomy[i].Reportable
		}
		reportable = reportable || t.Reportable
		out = append(out, t)
	}
	return out, reportable
}

// changesIn reads what version n did from the rows themselves: rows it
// closed, rows it computed (replacements of the ones it superseded, or
// added), and rows computed earlier that were still live after it. A row a
// satisfaction added in version n's time is not version n's doing.
func changesIn(rows []deadlines.Deadline, n int) DeadlineChanges {
	c := DeadlineChanges{Added: []string{}, Superseded: []string{}, Withdrawn: []string{}, Carried: []string{}}
	replacement := map[string]bool{}
	for _, d := range rows {
		if d.ClosedInVersion != nil && *d.ClosedInVersion == n {
			if *d.ClosedAs == "superseded" {
				c.Superseded = append(c.Superseded, d.ID)
				replacement[*d.SupersededBy] = true
			} else {
				c.Withdrawn = append(c.Withdrawn, d.ID)
			}
		}
	}
	for _, d := range rows {
		switch {
		case d.ComputedInVersion == n && d.AddedBySatisfactionID != nil:
		case d.ComputedInVersion == n && !replacement[d.ID]:
			c.Added = append(c.Added, d.ID)
		case d.ComputedInVersion < n && (d.ClosedInVersion == nil || *d.ClosedInVersion > n):
			c.Carried = append(c.Carried, d.ID)
		}
	}
	return c
}

// Get returns an incident as of a version (the latest when version is "").
// It works on an archived site.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, incidentID, version string) (*Incident, error) {
	n := 0
	if version != "" {
		v, err := strconv.Atoi(version)
		if err != nil || v < 1 {
			return nil, ErrBadVersion
		}
		n = v
	}
	var inc *Incident
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		h, err := getHeader(ctx, tx, orgID, sr.Site.ID, incidentID)
		if isNotFound(err) {
			return ErrIncidentNotFound
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
		inc, err = s.view(ctx, tx, sr.Data, orgID, h, n)
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get incident: %w", err)
	}
	return inc, nil
}

// List is the site's incident register, latest occurrence first. typ keeps
// incidents whose latest version has that type; reportable and open filter
// on "true"/"false". The query is checked before the site, as for other lists.
func (s *Service) List(ctx context.Context, orgID, userID, role, siteID, typ, reportable, open string, limit int) ([]IncidentSummary, error) {
	if reportable != "" && reportable != "true" && reportable != "false" {
		return nil, ErrBadReportable
	}
	if open != "" && open != "true" && open != "false" {
		return nil, ErrBadOpen
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}
	var out []IncidentSummary
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if typ != "" && !slices.ContainsFunc(sr.Data.Taxonomy, func(t regimes.IncidentType) bool { return t.Key == typ }) {
			return ErrBadType
		}
		rows, err := listSummaries(ctx, tx, orgID, sr.Site.ID, s.Clock.Now())
		if err != nil {
			return err
		}
		out = []IncidentSummary{}
		for _, r := range rows {
			r.Types, r.Reportable = typesOf(sr.Data, r.Codes)
			switch {
			case typ != "" && !slices.Contains(r.Codes, typ),
				reportable != "" && strconv.FormatBool(r.Reportable) != reportable,
				open != "" && strconv.FormatBool(r.OpenDeadlines > 0) != open:
				continue
			}
			out = append(out, r.IncidentSummary)
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
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	return out, nil
}

// mapRule turns a rule the database enforced into the API's error, for the
// few that a race can reach past the checks above.
func mapRule(err error) error {
	switch {
	case isRule(err, "is not the next version"):
		return ErrNotNextVersion
	case isRule(err, "only an owner or a SHE officer can withdraw"):
		return ErrWeakeningNeedsLead
	case isRule(err, "occurred after its site was archived"):
		return ErrSiteArchived
	case deadlines.IsBeforeCalendar(err):
		return ErrBeforeCalendar
	}
	return err
}

// isRule: err is a check_violation raised by one of the guards, whose
// message contains fragment.
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
	sites.ErrSiteNotFound, ErrIncidentNotFound, ErrBadIncidentID, ErrBadVersion, ErrVersionNotFound, ErrSiteArchived,
	ErrIDReused, ErrVersionDiffers, ErrNotNextVersion, ErrBadDeviceID, ErrNoTypes, ErrTooManyTypes, ErrRepeatedTypes,
	ErrUnknownTypes, ErrNoOccurredAt, ErrOccurredInFuture, ErrAwareBeforeIncident, ErrAwareInFuture, ErrAwareRequired,
	ErrNoSubmittedAt, ErrSubmittedInFuture, ErrBadLocation, ErrBadDescription, ErrTooManyPeople, ErrBadPerson,
	ErrBadTreatment, ErrInjuryNotInjured, ErrDeathNotInjured, ErrEmployerOfEmployee, ErrDeathBeforeIncident,
	ErrDeathInFuture, ErrDeathNotReportable, ErrBadEvidenceIDs, ErrRepeatedEvidence, ErrTooManyPhotos, ErrReasonRequired,
	ErrReasonOnFirst, ErrWeakeningNeedsLead, ErrBeforeCalendar, ErrEvidenceNotFound, ErrEvidenceRejected, ErrEvidenceNotVerified, ErrBadType,
	ErrBadReportable, ErrBadOpen, ErrBadLimit,
	ErrBadStatementID, ErrStatementIDReused, ErrBadWitnessName, ErrBadWitnessContact, ErrBadStatement, ErrNoTakenAt,
	ErrTakenInFuture, ErrBadSupersedes, ErrStatementCorrected, ErrBadEvidenceID,
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

func equalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
