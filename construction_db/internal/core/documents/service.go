package documents

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
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/files"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

const clockSkew = 5 * time.Minute

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	files *files.Service
	reg   *regimes.Registry
	// Clock is now for state and future checks; nil is the real time.
	Clock clock.Clock
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service, filesSvc *files.Service, reg *regimes.Registry) *Service {
	return &Service{pool: pool, sites: sitesSvc, files: filesSvc, reg: reg}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

// Where says which register a request is about: the vault, or a site's H&S
// file (SiteID is the URL's site).
type Where struct {
	Scope  string
	SiteID string
}

// target is a resolved register: the scope, the site (H&S file) and the
// regime whose types apply.
type target struct {
	scope  string
	siteID *string
	ds     *regimes.DataSet
}

// resolve finds the register: the vault (its routes admit only owners and
// SHE officers, D20), or a site's H&S file, for anyone who can see the site.
func (s *Service) resolve(ctx context.Context, tx pgx.Tx, orgID, userID, role string, w Where, regimeCode *string) (*target, error) {
	if w.Scope == ScopeOrg {
		if regimeCode == nil {
			return &target{scope: ScopeOrg}, nil
		}
		ds, ok := s.reg.Get(*regimeCode)
		if !ok {
			return nil, ErrBadRegime
		}
		return &target{scope: ScopeOrg, ds: ds}, nil
	}
	if regimeCode != nil {
		return nil, ErrRegimeForSite
	}
	sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, w.SiteID)
	if err != nil {
		return nil, err
	}
	return &target{scope: ScopeSite, siteID: &sr.Site.ID, ds: sr.Data}, nil
}

// Register lists a register's documents with each required type's
// readiness. The vault names the regime whose types to check (regimeCode);
// with one regime loaded it may be left out.
func (s *Service) Register(ctx context.Context, orgID, userID, role string, w Where, regimeCode string) (*Register, error) {
	var rc *string
	if w.Scope == ScopeOrg {
		if regimeCode == "" {
			if codes := s.reg.Codes(); len(codes) == 1 {
				regimeCode = codes[0]
			}
		}
		rc = &regimeCode
	}
	var out *Register
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		t, err := s.resolve(ctx, tx, orgID, userID, role, w, rc)
		if err != nil {
			return err
		}
		docs, err := listDocs(ctx, tx, orgID, t.scope, t.siteID, "")
		if err != nil {
			return err
		}
		now := s.Clock.Now()
		for i := range docs {
			s.setState(&docs[i].Current, docs[i].RegimeCode, t.scope, now)
		}
		out = &Register{RegimeCode: t.ds.Regime.Code, Documents: docs, Readiness: readiness(t.ds, t.scope, docs)}
		return nil
	})
	if err = finish(err, "document register"); err != nil {
		return nil, err
	}
	return out, nil
}

// readiness: per type of the scope, present if a document of it has a
// current unexpired version, expired if all have expired, else missing.
func readiness(ds *regimes.DataSet, scope string, docs []DocumentSummary) []Readiness {
	out := []Readiness{}
	for _, t := range ds.DocumentTypes {
		if t.Scope != scope {
			continue
		}
		r := Readiness{TypeKey: t.Key, Label: t.Label, Required: t.Required, Status: StatusMissing, DocumentIDs: []string{}}
		for _, d := range docs {
			if d.TypeKey != t.Key || d.RegimeCode != ds.Regime.Code || d.Retraction != nil {
				continue
			}
			r.DocumentIDs = append(r.DocumentIDs, d.ID)
			if d.Current.State == StateExpired {
				if r.Status == StatusMissing {
					r.Status = StatusExpired
				}
			} else {
				r.Status = StatusPresent
			}
		}
		out = append(out, r)
	}
	return out
}

// Create saves a new document with its first version (one transaction: the
// database refuses a document without a version at commit). An identical
// repeat answers 200.
func (s *Service) Create(ctx context.Context, orgID, userID, role string, w Where, docID string, req CreateRequest) (*Document, bool, error) {
	var (
		out     *Document
		created bool
	)
	err := s.retryOnce(func() error {
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			var err error
			out, created, err = s.CreateTx(ctx, tx, orgID, userID, role, w, docID, req)
			return err
		})
	})
	if err = finish(err, "create document"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// CreateTx is Create in the caller's transaction (ONBOARD's bulk
// documents). Losing a race for the document's id is IsRetry.
func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, orgID, userID, role string, w Where, docID string, req CreateRequest) (*Document, bool, error) {
	id, err := uuid.Parse(docID)
	if err != nil {
		return nil, false, ErrBadDocumentID
	}
	if !textOK(&req.Title, 200) {
		return nil, false, ErrBadTitle
	}
	if err := normaliseVersion(&req.Version, s.Clock.Now()); err != nil {
		return nil, false, err
	}
	if !isLead(role) {
		return nil, false, ErrCantWrite
	}
	if w.Scope == ScopeOrg && req.RegimeCode == nil {
		return nil, false, ErrBadRegime
	}
	t, err := s.resolve(ctx, tx, orgID, userID, role, w, req.RegimeCode)
	if err != nil {
		return nil, false, err
	}
	dt, ok := t.ds.DocumentType(t.scope, req.TypeKey)
	if !ok {
		return nil, false, ErrBadType
	}
	if err := checkAgainstType(dt, req.Version); err != nil {
		return nil, false, err
	}
	if existing, err := getDoc(ctx, tx, orgID, t.scope, t.siteID, id.String()); err == nil {
		first, err := listVersions(ctx, tx, orgID, existing.ID)
		if err != nil {
			return nil, false, err
		}
		if existing.TypeKey != req.TypeKey || existing.Title != req.Title || existing.CreatedBy != userID ||
			!sameVersion(&first[0], userID, req.Version) {
			return nil, false, ErrIDReused
		}
		out, err := s.view(ctx, tx, orgID, t, existing.ID)
		return out, false, err
	} else if !isNotFound(err) {
		return nil, false, err
	}
	if found, err := docAnywhere(ctx, tx, orgID, id.String()); err != nil {
		return nil, false, err
	} else if found {
		return nil, false, ErrIDReused
	}
	if err := files.Attachable(ctx, tx, orgID, req.Version.FileID, t.scope+"_document", t.siteID); err != nil {
		return nil, false, err
	}
	err = insertDoc(ctx, tx, orgID, id.String(), t.scope, t.siteID, t.ds.Regime.Code, req.TypeKey, req.Title, userID)
	if isUniqueViolation(err, "documents_pkey") {
		return nil, false, errRetry
	}
	if err != nil {
		return nil, false, err
	}
	if err := versionErr(insertVersion(ctx, tx, orgID, id.String(), t.scope, t.siteID, 1, userID, req.Version)); err != nil {
		return nil, false, err
	}
	out, err := s.view(ctx, tx, orgID, t, id.String())
	return out, true, err
}

// IsRetry: a ...Tx call lost a race for its record's id to a request that
// committed meanwhile; run the transaction again and it is a repeat.
func IsRetry(err error) bool { return errors.Is(err, errRetry) }

// IsClientError reports whether err is the caller's mistake (a 4xx of this
// package): what a batch reports per item.
func IsClientError(err error) bool { return isSentinel(err) }

// AddVersion adds version n (2 and up) of a document. An identical repeat
// answers 200; another version n, or a number that isn't next, is 409.
func (s *Service) AddVersion(ctx context.Context, orgID, userID, role string, w Where, docID, number string, req VersionRequest) (*Document, bool, error) {
	n, err := strconv.Atoi(number)
	if err != nil || n < 2 {
		return nil, false, ErrBadVersion
	}
	if err := normaliseVersion(&req, s.Clock.Now()); err != nil {
		return nil, false, err
	}
	if !isLead(role) {
		return nil, false, ErrCantWrite
	}
	var (
		out     *Document
		created bool
	)
	err = s.retryOnce(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			t, d, err := s.resolveDoc(ctx, tx, orgID, userID, role, w, docID)
			if err != nil {
				return err
			}
			versions, err := listVersions(ctx, tx, orgID, d.ID)
			if err != nil {
				return err
			}
			if n <= len(versions) {
				if !sameVersion(&versions[n-1], userID, req) {
					return ErrVersionTaken
				}
				out, err = s.view(ctx, tx, orgID, t, d.ID)
				return err
			}
			if d.Retraction != nil {
				return ErrRetracted
			}
			if n != d.LatestVersion+1 {
				return ErrNotNextVersion
			}
			dt, ok := t.ds.DocumentType(t.scope, d.TypeKey)
			if !ok {
				return ErrBadType
			}
			if err := checkAgainstType(dt, req); err != nil {
				return err
			}
			if err := files.Attachable(ctx, tx, orgID, req.FileID, t.scope+"_document", t.siteID); err != nil {
				return err
			}
			if err := versionErr(insertVersion(ctx, tx, orgID, d.ID, t.scope, t.siteID, n, userID, req)); err != nil {
				return err
			}
			created = true
			out, err = s.view(ctx, tx, orgID, t, d.ID)
			return err
		})
	})
	if err = finish(err, "add document version"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// versionErr maps a lost race: the same number saved first (the retry then
// answers as a repeat or 409), or a number no longer next.
func versionErr(err error) error {
	switch {
	case err == nil:
		return nil
	case isUniqueViolation(err, "document_versions_pkey"), isRule(err, "is not the next version"):
		return errRetry
	case isUniqueViolation(err, "document_versions_file_id_key"):
		return ErrFileUsed
	case isRule(err, "retracted; no version can be added"):
		return ErrRetracted
	}
	return err
}

func (s *Service) resolveDoc(ctx context.Context, tx pgx.Tx, orgID, userID, role string, w Where, docID string) (*target, *DocumentSummary, error) {
	t, err := s.resolve(ctx, tx, orgID, userID, role, w, nil)
	if err != nil {
		return nil, nil, err
	}
	d, err := getDoc(ctx, tx, orgID, t.scope, t.siteID, docID)
	if isNotFound(err) {
		return nil, nil, ErrDocumentNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if t.ds == nil {
		ds, ok := s.reg.Get(d.RegimeCode)
		if !ok {
			return nil, nil, ErrRegimeNotFound
		}
		t.ds = ds
	}
	return t, d, nil
}

// Get is a document with every version, oldest first.
func (s *Service) Get(ctx context.Context, orgID, userID, role string, w Where, docID string) (*Document, error) {
	var out *Document
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		t, d, err := s.resolveDoc(ctx, tx, orgID, userID, role, w, docID)
		if err != nil {
			return err
		}
		out, err = s.view(ctx, tx, orgID, t, d.ID)
		return err
	})
	if err = finish(err, "get document"); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) view(ctx context.Context, tx pgx.Tx, orgID string, t *target, id string) (*Document, error) {
	d, err := getDoc(ctx, tx, orgID, t.scope, t.siteID, id)
	if err != nil {
		return nil, err
	}
	out := &Document{DocumentSummary: *d}
	if out.Versions, err = listVersions(ctx, tx, orgID, id); err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	s.setState(&out.Current, d.RegimeCode, t.scope, now)
	for i := range out.Versions {
		switch v := &out.Versions[i]; {
		case v.Retraction != nil:
			v.State = StateRetracted
		case v.Version == d.Current.Version:
			v.State = out.Current.State
		default:
			v.State = StateSuperseded
		}
	}
	return out, nil
}

// View logs a view of a version's file and returns a short-lived URL.
func (s *Service) View(ctx context.Context, orgID, userID, role string, w Where, docID, number string, client evidence.Client) (*evidence.ViewURL, error) {
	n, err := strconv.Atoi(number)
	if err != nil || n < 1 {
		return nil, ErrVersionNotFound
	}
	var f *files.File
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		_, d, err := s.resolveDoc(ctx, tx, orgID, userID, role, w, docID)
		if err != nil {
			return err
		}
		versions, err := listVersions(ctx, tx, orgID, d.ID)
		if err != nil {
			return err
		}
		if n > len(versions) {
			return ErrVersionNotFound
		}
		f, err = files.LogView(ctx, tx, orgID, userID, role, versions[n-1].FileID, client)
		return err
	})
	if err = finish(err, "view document"); err != nil {
		return nil, err
	}
	return s.files.Presign(ctx, f)
}

// setState: the current version's expiry against today in the regime's
// zone; expiring within the first reminder lead time (company documents'
// for the vault, the worker list's for the H&S file).
func (s *Service) setState(v *Version, regimeCode, scope string, now time.Time) {
	v.State = StateValid
	ds, ok := s.reg.Get(regimeCode)
	if !ok || v.ExpiresOn == nil {
		return
	}
	loc, err := time.LoadLocation(ds.Regime.TimeZone)
	if err != nil {
		return
	}
	exp, err := time.ParseInLocation(time.DateOnly, *v.ExpiresOn, loc)
	if err != nil {
		return
	}
	lead := ds.Reminders.WorkerExpiryDays
	if scope == ScopeOrg {
		lead = ds.Reminders.CompanyDocumentDays
	}
	y, m, d := now.In(loc).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch {
	case exp.Before(today):
		v.State = StateExpired
	case len(lead) > 0 && !exp.After(today.AddDate(0, 0, lead[0])):
		v.State = StateExpiring
	}
}

func checkAgainstType(dt regimes.DocumentType, v VersionRequest) error {
	switch {
	case dt.Expiry == regimes.ExpiryRequired && v.ExpiresOn == nil:
		return ErrExpiryNeeded
	case dt.Expiry == regimes.ExpiryNone && v.ExpiresOn != nil:
		return ErrExpiryNone
	case len(dt.Issuers) > 0 && (v.Issuer == nil || !slices.Contains(dt.Issuers, *v.Issuer)):
		return ErrBadIssuer
	}
	return nil
}

func normaliseVersion(v *VersionRequest, now time.Time) error {
	if _, err := uuid.Parse(v.FileID); err != nil {
		return ErrBadFileID
	}
	var issued, expires time.Time
	var err error
	switch {
	case !textOK(v.Issuer, 200):
		return ErrBadIssuer
	case !textOK(v.Reference, 200):
		return ErrBadReference
	case v.ClientUploadedAt.IsZero():
		return ErrNoUploadedAt
	case v.ClientUploadedAt.After(now.Add(clockSkew)):
		return ErrUploadedInFuture
	case v.DeviceID != nil && (len(*v.DeviceID) == 0 || len(*v.DeviceID) > 200):
		return ErrBadDeviceID
	}
	if v.IssuedOn != nil {
		if issued, err = time.Parse(time.DateOnly, *v.IssuedOn); err != nil {
			return ErrBadDate
		}
	}
	if v.ExpiresOn != nil {
		if expires, err = time.Parse(time.DateOnly, *v.ExpiresOn); err != nil {
			return ErrBadDate
		}
		if v.IssuedOn != nil && expires.Before(issued) {
			return ErrBadDate
		}
	}
	v.ClientUploadedAt = v.ClientUploadedAt.Truncate(time.Microsecond).UTC()
	return nil
}

func sameVersion(v *Version, by string, r VersionRequest) bool {
	return v.FileID == r.FileID && v.UploadedBy == by && equalPtr(v.Issuer, r.Issuer) && equalPtr(v.Reference, r.Reference) &&
		equalPtr(v.IssuedOn, r.IssuedOn) && equalPtr(v.ExpiresOn, r.ExpiresOn) && v.ClientUploadedAt.Equal(r.ClientUploadedAt) &&
		equalPtr(v.DeviceID, r.DeviceID)
}

var errRetry = errors.New("retry")

func (s *Service) retryOnce(fn func() error) error {
	if err := fn(); !errors.Is(err, errRetry) {
		return err
	}
	err := fn()
	if errors.Is(err, errRetry) {
		return ErrVersionTaken
	}
	return err
}

func finish(err error, prefix string) error {
	if err == nil || isSentinel(err) {
		return err
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// IsSentinel: an error the handlers send as itself.
func IsSentinel(err error) bool { return isSentinel(err) }

func isSentinel(err error) bool {
	if files.IsSentinel(err) {
		return true
	}
	return slices.ContainsFunc(Sentinels, func(s error) bool { return errors.Is(err, s) })
}

// Sentinels are the errors the handlers send; TestAPIDoc checks each is quoted.
var Sentinels = []error{
	ErrDocumentNotFound, ErrVersionNotFound, ErrRegimeNotFound, ErrBadDocumentID, ErrBadVersion, ErrBadRegime, ErrRegimeForSite,
	ErrBadType, ErrBadTitle, ErrBadIssuer, ErrBadReference, ErrBadDate, ErrExpiryNeeded, ErrExpiryNone, ErrNoUploadedAt,
	ErrUploadedInFuture, ErrBadDeviceID, ErrBadFileID, ErrCantWrite, ErrIDReused, ErrVersionTaken, ErrNotNextVersion, ErrFileUsed,
	ErrBadReason, ErrCantRetract, ErrRetracted, ErrAlreadyRetracted, ErrNotCurrent, ErrOnlyVersion,
}

func textOK(s *string, max int) bool {
	if s == nil {
		return true
	}
	n := utf8.RuneCountInString(*s)
	return utf8.ValidString(*s) && n >= 1 && n <= max && !strings.ContainsRune(*s, 0) && strings.TrimSpace(*s) != ""
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Retract withdraws a whole document entered in error: an owner or SHE
// officer, with a reason, once. It stays in the register, marked; readiness
// and reminders ignore it. Repeating it with the same reason answers 200.
func (s *Service) Retract(ctx context.Context, orgID, userID, role string, w Where, docID string, req RetractRequest) (*Document, bool, error) {
	if !textOK(&req.Reason, 500) {
		return nil, false, ErrBadReason
	}
	if !isLead(role) {
		return nil, false, ErrCantRetract
	}
	var (
		out     *Document
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		t, d, err := s.resolveDoc(ctx, tx, orgID, userID, role, w, docID)
		if err != nil {
			return err
		}
		if r := d.Retraction; r != nil {
			if r.Reason != req.Reason {
				return ErrAlreadyRetracted
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE documents SET retracted_by = $3, retract_reason = $4 WHERE org_id = $1 AND id = $2`,
				orgID, d.ID, userID, req.Reason); err != nil {
				return err
			}
			created = true
		}
		out, err = s.view(ctx, tx, orgID, t, d.ID)
		return err
	})
	if err = finish(err, "retract document"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// RetractVersion withdraws a document's current version entered in error,
// so the previous one is current again. Never the only standing version:
// retract the document instead. The database checks the same, under the
// document's row lock.
func (s *Service) RetractVersion(ctx context.Context, orgID, userID, role string, w Where, docID, number string, req RetractRequest) (*Document, bool, error) {
	n, err := strconv.Atoi(number)
	if err != nil || n < 1 {
		return nil, false, ErrVersionNotFound
	}
	if !textOK(&req.Reason, 500) {
		return nil, false, ErrBadReason
	}
	if !isLead(role) {
		return nil, false, ErrCantRetract
	}
	var (
		out     *Document
		created bool
	)
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		t, d, err := s.resolveDoc(ctx, tx, orgID, userID, role, w, docID)
		if err != nil {
			return err
		}
		versions, err := listVersions(ctx, tx, orgID, d.ID)
		if err != nil {
			return err
		}
		if n > len(versions) {
			return ErrVersionNotFound
		}
		v := versions[n-1]
		switch {
		case v.Retraction != nil && v.Retraction.Reason != req.Reason:
			return ErrAlreadyRetracted
		case v.Retraction != nil:
		case d.Retraction != nil:
			return ErrRetracted
		case n != d.Current.Version:
			return ErrNotCurrent
		default:
			standing := 0
			for _, x := range versions {
				if x.Retraction == nil {
					standing++
				}
			}
			if standing < 2 {
				return ErrOnlyVersion
			}
			_, err := tx.Exec(ctx, `UPDATE document_versions SET retracted_by = $4, retract_reason = $5
				WHERE org_id = $1 AND document_id = $2 AND version = $3`, orgID, d.ID, n, userID, req.Reason)
			switch {
			case isRule(err, "only the current version can be retracted"):
				return ErrNotCurrent
			case isRule(err, "the only standing version"):
				return ErrOnlyVersion
			case err != nil:
				return err
			}
			created = true
		}
		out, err = s.view(ctx, tx, orgID, t, d.ID)
		return err
	})
	if err = finish(err, "retract document version"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}
