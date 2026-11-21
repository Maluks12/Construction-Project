package deadlines

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
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

// A device clock may run this far ahead of the server; the database allows the same.
const clockSkew = 5 * time.Minute

// SubjectHooks are a subject package's part in recording a satisfaction.
type SubjectHooks interface {
	// LockForSatisfaction takes the lock the subject's new versions take,
	// so a satisfaction and an amendment queue.
	LockForSatisfaction(ctx context.Context, tx pgx.Tx, orgID, siteID, subjectID string) error
	// Satisfied runs after the satisfaction is inserted, in the same transaction.
	Satisfied(ctx context.Context, tx pgx.Tx, s Satisfied) error
}

type Satisfied struct {
	Data                                               *regimes.DataSet
	RegimeID, OrgID, SiteID, SubjectID, SatisfactionID string
}

// Service serves the site deadline endpoints for every subject.
type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	hooks map[SubjectKind]SubjectHooks
	Clock clock.Clock
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service) *Service {
	return &Service{pool: pool, sites: sitesSvc, hooks: map[SubjectKind]SubjectHooks{}}
}

// Register gives the service a subject's hooks. Called while building the router.
func (s *Service) Register(kind SubjectKind, h SubjectHooks) { s.hooks[kind] = h }

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

var liveStates = []string{StateOpen, StateDueNow, StateOverdue, StateSatisfied, StateSatisfiedLate}

// ListDeadlines returns a site's live deadlines, soonest due first, filtered
// by state and subject type. Works on an archived site.
func (s *Service) ListDeadlines(ctx context.Context, orgID, userID, role, siteID, state, subject string, limit int) ([]Deadline, error) {
	if state != "" && !slices.Contains(liveStates, state) {
		return nil, ErrBadState
	}
	if subject != "" && !slices.Contains(SubjectKinds, subject) {
		return nil, ErrBadSubjectType
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}
	var out []Deadline
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		all, err := ListLiveForSite(ctx, tx, orgID, site.ID, s.Clock.Now())
		if err != nil {
			return err
		}
		out = []Deadline{}
		for _, d := range all {
			if (state == "" || d.State == state) && (subject == "" || string(d.SubjectType) == subject) {
				out = append(out, d)
			}
			if len(out) == limit {
				break
			}
		}
		return nil
	})
	if isSentinel(err) || errors.Is(err, sites.ErrSiteNotFound) {
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
		out, err = s.detail(ctx, tx, orgID, site.ID, deadlineID)
		return err
	})
	if isSentinel(err) || errors.Is(err, sites.ErrSiteNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get deadline: %w", err)
	}
	return out, nil
}

func (s *Service) detail(ctx context.Context, db DBTX, orgID, siteID, deadlineID string) (*DeadlineDetail, error) {
	d, err := GetForSite(ctx, db, orgID, siteID, deadlineID, s.Clock.Now())
	if isNotFound(err) {
		return nil, ErrDeadlineNotFound
	}
	if err != nil {
		return nil, err
	}
	sats, err := ListSatisfactions(ctx, db, orgID, d.DutyID)
	if err != nil {
		return nil, err
	}
	for i := range sats {
		sats[i].RecordedAfterDue = RecordedAfterDue(d.Unit, d.DueAt, sats[i])
	}
	return &DeadlineDetail{Deadline: d, Satisfactions: sats}, nil
}

var (
	// errIDTaken: the satisfaction's primary key was hit. The caller looks again once.
	errIDTaken = errors.New("id taken")
	// errLiveMoved: the live row was superseded between reading it and recording against it.
	errLiveMoved = errors.New("live deadline moved")
)

// Satisfy records that a deadline's duty was met. Any row of the duty may be
// named (a phone may hold a superseded id): the satisfaction is recorded
// against the duty's live row and judged against its due time. The subject's
// hooks queue it with amendments and may add clocks (a death after a
// notice). created is false for an identical repeat. Returns the live row.
func (s *Service) Satisfy(ctx context.Context, orgID, userID, role, siteID, deadlineID, satisfactionID string, req SatisfactionRequest) (*DeadlineDetail, bool, error) {
	if _, err := uuid.Parse(deadlineID); err != nil {
		return nil, false, ErrDeadlineNotFound
	}
	sid, err := uuid.Parse(satisfactionID)
	if err != nil {
		return nil, false, ErrBadSatisfactionID
	}
	if err := normaliseSatisfaction(&req, s.Clock.Now()); err != nil {
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
	if err = mapRule(err); isSentinel(err) || errors.Is(err, sites.ErrSiteNotFound) {
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
		now := s.Clock.Now()
		named, err := GetForSite(ctx, tx, orgID, sr.Site.ID, deadlineID, now)
		if isNotFound(err) {
			return ErrDeadlineNotFound
		}
		if err != nil {
			return err
		}
		hooks, subjectID, err := s.hooksFor(named)
		if err != nil {
			return err
		}
		if err := hooks.LockForSatisfaction(ctx, tx, orgID, sr.Site.ID, subjectID); err != nil {
			return err
		}

		existing, duty, err := GetSatisfaction(ctx, tx, orgID, id)
		switch {
		case err == nil && (duty != named.DutyID || !sameSatisfaction(existing, userID, req)):
			return ErrSatisfactionIDReused
		case err == nil:
			show := existing.DeadlineID // the duty was withdrawn since
			live, err := GetLiveForDuty(ctx, tx, orgID, duty, now)
			switch {
			case err == nil:
				show = live.ID
			case !isNotFound(err):
				return err
			}
			out, err = s.detail(ctx, tx, orgID, sr.Site.ID, show)
			return err
		case !isNotFound(err):
			return err
		}

		live, err := GetLiveForDuty(ctx, tx, orgID, named.DutyID, now)
		if isNotFound(err) {
			return ErrDeadlineWithdrawn
		}
		if err != nil {
			return err
		}
		if live.Satisfaction != nil {
			return ErrAlreadySatisfied
		}
		// A before-deadline (a notice) is met before its basis, so only an
		// after-deadline has a lower bound.
		if live.Direction == "after" && req.SatisfiedAt.Before(live.BasisAt.Add(-clockSkew)) {
			return ErrSatisfiedBeforeBasis
		}
		if req.EvidenceID != nil {
			if err := checkEvidence(ctx, tx, orgID, sr.Site.ID, *req.EvidenceID); err != nil {
				return err
			}
		}
		if req.DocumentID != nil {
			if err := checkProof(ctx, tx, orgID, sr.Site.ID, live, *req.DocumentID, *req.DocumentVersion); err != nil {
				return err
			}
		}

		err = InsertSatisfaction(ctx, tx, orgID, sr.Site.ID, NewSatisfaction{
			ID: id, DutyID: live.DutyID, DeadlineID: live.ID, SatisfiedAt: req.SatisfiedAt, Method: req.Method,
			Reference: req.Reference, Note: req.Note, Evidence: req.EvidenceID, Document: req.DocumentID,
			DocumentVersion: req.DocumentVersion, RecordedBy: userID})
		switch {
		case isUniqueViolation(err, "deadline_satisfactions_pkey"):
			return errIDTaken
		case isRule(err, "record the satisfaction against the live deadline"):
			return errLiveMoved
		case err != nil:
			return err
		}
		if err := hooks.Satisfied(ctx, tx, Satisfied{Data: sr.Data, RegimeID: sr.RegimeID, OrgID: orgID, SiteID: sr.Site.ID,
			SubjectID: subjectID, SatisfactionID: id}); err != nil {
			return err
		}
		created = true
		out, err = s.detail(ctx, tx, orgID, sr.Site.ID, live.ID)
		return err
	})
	return out, created, err
}

func (s *Service) hooksFor(d Deadline) (SubjectHooks, string, error) {
	id := d.IncidentID
	if d.SubjectType == SubjectSiteWork {
		id = d.SiteWorkID
	}
	h, ok := s.hooks[d.SubjectType]
	if !ok || id == nil {
		return nil, "", fmt.Errorf("deadline %s: no hooks for subject %q", d.ID, d.SubjectType)
	}
	return h, *id, nil
}

// checkProof: a version of a document in this site's H&S file, of one of the
// deadline's proof types, neither retracted. The database checks the same.
func checkProof(ctx context.Context, db DBTX, orgID, siteID string, d Deadline, docID string, version int) error {
	var (
		typeKey          string
		docGone, verGone bool
	)
	err := db.QueryRow(ctx, `
		SELECT x.type_key, x.retracted_at IS NOT NULL, v.retracted_at IS NOT NULL
		FROM documents x JOIN document_versions v ON v.document_id = x.id
		WHERE x.org_id = $1 AND x.site_id = $2 AND x.id = $3 AND v.version = $4`, orgID, siteID, docID, version).
		Scan(&typeKey, &docGone, &verGone)
	switch {
	case isNotFound(err):
		return ErrDocumentNotFound
	case err != nil:
		return fmt.Errorf("proof document: %w", err)
	case !slices.Contains(d.ProofDocumentTypes, typeKey):
		return ErrNotProof
	case docGone || verGone:
		return ErrDocumentRetracted
	}
	return nil
}

func checkEvidence(ctx context.Context, db DBTX, orgID, siteID, id string) error {
	var status string
	err := db.QueryRow(ctx, `SELECT status FROM evidence WHERE org_id = $1 AND site_id = $2 AND id = $3`, orgID, siteID, id).Scan(&status)
	switch {
	case isNotFound(err):
		return &EvidenceError{Err: ErrEvidenceNotFound, IDs: []string{id}}
	case err != nil:
		return fmt.Errorf("evidence status: %w", err)
	case status == evidence.StatusRejected:
		return &EvidenceError{Err: ErrEvidenceRejected, IDs: []string{id}}
	case status == evidence.StatusPending:
		return &EvidenceError{Err: ErrEvidenceNotVerified, IDs: []string{id}}
	}
	return nil
}

func normaliseSatisfaction(req *SatisfactionRequest, now time.Time) error {
	if req.SatisfiedAt.IsZero() {
		return ErrNoSatisfiedAt
	}
	req.SatisfiedAt = req.SatisfiedAt.Truncate(time.Microsecond).UTC()
	if req.SatisfiedAt.After(now.Add(clockSkew)) {
		return ErrSatisfiedInFuture
	}
	if !slices.Contains(Methods, req.Method) {
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
	if (req.DocumentID == nil) != (req.DocumentVersion == nil) {
		return ErrDocumentPair
	}
	if req.DocumentID != nil {
		d, err := uuid.Parse(*req.DocumentID)
		if err != nil {
			return ErrBadDocumentID
		}
		canonical := d.String()
		req.DocumentID = &canonical
		if *req.DocumentVersion < 1 {
			return ErrDocumentNotFound
		}
	}
	return nil
}

func textOK(s *string, max int) bool {
	if s == nil {
		return true
	}
	n := utf8.RuneCountInString(*s)
	return utf8.ValidString(*s) && n >= 1 && n <= max && !strings.ContainsRune(*s, 0)
}

func sameSatisfaction(s *Satisfaction, by string, req SatisfactionRequest) bool {
	return s.RecordedBy == by && s.SatisfiedAt.Equal(req.SatisfiedAt) && s.Method == req.Method &&
		equalPtr(s.Reference, req.Reference) && equalPtr(s.Note, req.Note) && equalPtr(s.EvidenceID, req.EvidenceID) &&
		equalPtr(s.DocumentID, req.DocumentID) && equalPtr(s.DocumentVersion, req.DocumentVersion)
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
	if !textOK(&req.Reason, 500) {
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
		named, err := GetForSite(ctx, tx, orgID, site.ID, deadlineID, s.Clock.Now())
		if isNotFound(err) {
			return ErrDeadlineNotFound
		}
		if err != nil {
			return err
		}
		sat, duty, err := GetSatisfaction(ctx, tx, orgID, satisfactionID)
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
			err := InsertRetraction(ctx, tx, orgID, site.ID, satisfactionID, duty, userID, req.Reason)
			if isUniqueViolation(err, "deadline_satisfaction_retractions_pkey") {
				return ErrRetractionDiffers // a concurrent retraction won; the client retries and sees it
			}
			if err != nil {
				return err
			}
			created = true
		}
		out, err = s.detail(ctx, tx, orgID, site.ID, deadlineID)
		return err
	})
	if isSentinel(err) || errors.Is(err, sites.ErrSiteNotFound) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("retract satisfaction: %w", err)
	}
	return out, created, nil
}

// mapRule turns a rule the database enforced into the API's error, for those
// a race can reach past the checks above.
func mapRule(err error) error {
	switch {
	case isRule(err, "already satisfied"):
		return ErrAlreadySatisfied
	case isRule(err, "is not proof of this deadline"):
		return ErrNotProof
	case isRule(err, "that document version is retracted"):
		return ErrDocumentRetracted
	case IsBeforeCalendar(err):
		return ErrBeforeCalendar
	}
	return err
}

// IsBeforeCalendar: deadline_due needed a day before the holiday calendar
// starts (000015). A day after its end is a calendar to extend, so a 500.
func IsBeforeCalendar(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22000" && strings.HasPrefix(pgErr.Message, "holiday calendar:") &&
		strings.HasSuffix(pgErr.Message, "is before it")
}

func isRule(err error, fragment string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, fragment)
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
