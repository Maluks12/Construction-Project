package toolbox

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

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

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

// errRetry: an insert lost a race on its primary key to an identical
// request; once more, and the repeat check answers it.
var errRetry = errors.New("retry")

func once(fn func() error) error {
	if err := fn(); !errors.Is(err, errRetry) {
		return err
	}
	return fn()
}

// Topics is the site regime's list (D3).
func (s *Service) Topics(ctx context.Context, orgID, userID, role, siteID string) ([]regimes.ToolboxTopic, error) {
	ds, err := s.sites.Regime(ctx, orgID, userID, role, siteID)
	if errors.Is(err, sites.ErrSiteNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("toolbox topics: %w", err)
	}
	return ds.ToolboxTopics, nil
}

// PutTalk creates a talk under an id the device chose (created true), or
// edits it. An identical repeat changes nothing. Once someone has signed,
// only ended_at can be set, once (the database enforces the same). The talk
// row is locked first, so an edit and a signature take turns.
func (s *Service) PutTalk(ctx context.Context, orgID, userID, role, siteID, talkID string, req TalkRequest) (*Talk, bool, error) {
	id, err := uuid.Parse(talkID)
	if err != nil {
		return nil, false, ErrBadTalkID
	}
	if err := normaliseTalk(&req, time.Now()); err != nil {
		return nil, false, err
	}
	var (
		t       *Talk
		created bool
	)
	err = once(func() error {
		t, created, err = s.putTalkOnce(ctx, orgID, userID, role, siteID, id.String(), req)
		return err
	})
	if errors.Is(err, errRetry) {
		err = ErrTalkIDReused
	}
	t, err = finish(t, err, "put talk")
	return t, created, err
}

func (s *Service) putTalkOnce(ctx context.Context, orgID, userID, role, siteID, id string, req TalkRequest) (t *Talk, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		sr, err := s.sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		row, err := rowOf(sr.Data, req)
		if err != nil {
			return err
		}
		existing, err := lockTalk(ctx, tx, orgID, sr.Site.ID, id)
		switch {
		case err == nil:
			if err := s.edit(ctx, tx, orgID, &existing.TalkHeader, row); err != nil {
				return err
			}
		case !isNotFound(err):
			return err
		default:
			if _, err := talkSite(ctx, tx, orgID, id); err == nil {
				return ErrTalkIDReused
			} else if !isNotFound(err) {
				return err
			}
			err = insertTalk(ctx, tx, orgID, sr.Site.ID, id, userID, sr.Data.Regime.TimeZone, row, req.DeviceID)
			if isUniqueViolation(err, "toolbox_talks_pkey") {
				return errRetry
			}
			if isRule(err, "started after its site was archived") {
				return ErrStartedAfterClose
			}
			if err != nil {
				return err
			}
			created = true
		}
		t, err = view(ctx, tx, orgID, sr.Site.ID, id)
		return err
	})
	return t, created, err
}

// edit applies row to a locked talk, if anything changes and it may.
func (s *Service) edit(ctx context.Context, tx pgx.Tx, orgID string, h *TalkHeader, row talkRow) error {
	have := talkRow{h.TopicKey, h.Topic, h.PresenterName, h.Notes, h.StartedAt, h.EndedAt}
	if sameRow(have, row) {
		return nil
	}
	signed, err := hasSignatures(ctx, tx, h.ID)
	if err != nil {
		return err
	}
	if signed {
		// Only setting the end time, for the first time.
		end := have
		end.EndedAt = row.EndedAt
		if have.EndedAt != nil || !sameRow(end, row) {
			return ErrTalkSigned
		}
		if late, err := signedAfter(ctx, tx, h.ID, *row.EndedAt); err != nil {
			return err
		} else if late {
			return ErrEndsBeforeSigned
		}
	}
	err = updateTalk(ctx, tx, orgID, h.ID, row)
	if isRule(err, "people have signed it") {
		return ErrTalkSigned
	}
	return err
}

// rowOf resolves the topic against the site's regime.
func rowOf(ds *regimes.DataSet, req TalkRequest) (talkRow, error) {
	r := talkRow{TopicKey: req.TopicKey, PresenterName: req.PresenterName, Notes: req.Notes, StartedAt: req.StartedAt, EndedAt: req.EndedAt}
	switch {
	case req.TopicKey != nil:
		topic, ok := ds.Topic(*req.TopicKey)
		if !ok {
			return r, ErrUnknownTopic
		}
		r.Topic = topic.Title
		if req.Topic != nil {
			r.Topic = *req.Topic
		}
	case req.Topic != nil:
		r.Topic = *req.Topic
	default:
		return r, ErrTopicRequired
	}
	return r, nil
}

func sameRow(a, b talkRow) bool {
	return equalPtr(a.TopicKey, b.TopicKey) && a.Topic == b.Topic && a.PresenterName == b.PresenterName &&
		equalPtr(a.Notes, b.Notes) && a.StartedAt.Equal(b.StartedAt) && equalTime(a.EndedAt, b.EndedAt)
}

func normaliseTalk(req *TalkRequest, now time.Time) error {
	switch {
	case !textOK(req.Topic, 200):
		return ErrBadTopic
	case !textOK(&req.PresenterName, 200):
		return ErrBadPresenter
	case !textOK(req.Notes, 4000) || (req.Notes != nil && strings.TrimSpace(*req.Notes) == ""):
		return ErrBadNotes
	case req.StartedAt.IsZero():
		return ErrNoStartedAt
	case req.StartedAt.After(now.Add(clockSkew)):
		return ErrStartedInFuture
	case !deviceOK(req.DeviceID):
		return ErrBadDeviceID
	}
	req.StartedAt = micro(req.StartedAt)
	if req.EndedAt != nil {
		end := micro(*req.EndedAt)
		switch {
		case end.After(now.Add(clockSkew)):
			return ErrEndedInFuture
		case end.Before(req.StartedAt):
			return ErrEndsBeforeStart
		case end.After(req.StartedAt.Add(SigningWindow)):
			return ErrEndsTooLate
		}
		req.EndedAt = &end
	}
	return nil
}

// AddPhoto adds a verified photo of the talk's site. Repeating it with the
// same kind answers the talk unchanged. Photos are append-only.
func (s *Service) AddPhoto(ctx context.Context, orgID, userID, role, siteID, talkID, evidenceID string, req PhotoRequest) (*Talk, bool, error) {
	tid, err := uuid.Parse(talkID)
	if err != nil {
		return nil, false, ErrTalkNotFound
	}
	eid, err := uuid.Parse(evidenceID)
	if err != nil {
		return nil, false, ErrBadEvidenceID
	}
	if !slices.Contains(Kinds, req.Kind) {
		return nil, false, ErrBadKind
	}
	var (
		t       *Talk
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
			if err != nil {
				return err
			}
			if _, err := getHeader(ctx, tx, orgID, site.ID, tid.String()); isNotFound(err) {
				return ErrTalkNotFound
			} else if err != nil {
				return err
			}
			kind, err := photoKind(ctx, tx, tid.String(), eid.String())
			switch {
			case err == nil && kind != req.Kind:
				return ErrPhotoKindDiffers
			case err == nil:
			case !isNotFound(err):
				return err
			default:
				status, err := evidenceStatus(ctx, tx, orgID, site.ID, eid.String())
				switch {
				case isNotFound(err):
					return ErrEvidenceNotFound
				case err != nil:
					return err
				case status == evidence.StatusRejected:
					return ErrEvidenceRejected
				case status != evidence.StatusVerified:
					return ErrEvidenceNotVerified
				}
				err = insertPhoto(ctx, tx, orgID, site.ID, tid.String(), eid.String(), req.Kind, userID)
				if isUniqueViolation(err, "toolbox_talk_evidence_pkey") {
					return errRetry
				}
				if err != nil {
					return err
				}
				created = true
			}
			t, err = view(ctx, tx, orgID, site.ID, tid.String())
			return err
		})
	})
	t, err = finish(t, err, "add talk photo")
	return t, created, err
}

// Sign records one signature under an id the device chose; the caller is
// the recorder. Order of checks: site scope, a repeat of the id (200), the
// talk, the signing window, the same person, the paper register.
func (s *Service) Sign(ctx context.Context, orgID, userID, role, siteID, talkID, attendanceID string, req SignRequest) (*Attendance, bool, error) {
	tid, err := uuid.Parse(talkID)
	if err != nil {
		return nil, false, ErrTalkNotFound
	}
	aid, err := uuid.Parse(attendanceID)
	if err != nil {
		return nil, false, ErrBadAttendanceID
	}
	if err := normaliseSign(&req, time.Now()); err != nil {
		return nil, false, err
	}
	var (
		a       *Attendance
		created bool
	)
	err = once(func() error {
		a, created, err = s.signOnce(ctx, orgID, userID, role, siteID, tid.String(), aid.String(), req)
		return err
	})
	if errors.Is(err, errRetry) {
		err = ErrAttendanceIDReused
	}
	if err = mapRule(err); isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("sign: %w", err)
	}
	return a, created, nil
}

func (s *Service) signOnce(ctx context.Context, orgID, userID, role, siteID, talkID, id string, req SignRequest) (a *Attendance, created bool, err error) {
	err = s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		// An identical repeat answers 200 before any other check.
		if existing, err := getAttendance(ctx, tx, orgID, site.ID, id); err == nil {
			if !sameSignature(existing, talkID, userID, req) {
				return ErrAttendanceIDReused
			}
			a = existing
			return nil
		} else if !isNotFound(err) {
			return err
		}
		if found, err := attendanceAnywhere(ctx, tx, orgID, id); err != nil {
			return err
		} else if found {
			return ErrAttendanceIDReused
		}

		talk, err := getHeader(ctx, tx, orgID, site.ID, talkID)
		if isNotFound(err) {
			return ErrTalkNotFound
		}
		if err != nil {
			return err
		}
		if err := CheckSigningWindow(talk.StartedAt, talk.EndedAt, req.ClientSignedAt); err != nil {
			return err
		}
		if dup, err := sameIdentity(ctx, tx, talkID, IdentityKey(req.Name, req.Firm, req.ClockNumber)); err != nil {
			return err
		} else if dup {
			return ErrAlreadySigned
		}
		if req.Method == MethodPaper {
			if ok, err := hasRegisterPhoto(ctx, tx, talkID); err != nil {
				return err
			} else if !ok {
				return ErrNeedsRegisterPhoto
			}
		}
		err = insertAttendance(ctx, tx, orgID, site.ID, talkID, id, userID, req)
		// A concurrent signature committed first. Which unique key reports it
		// depends on index order, so either one retries: then the repeat
		// check (same id) or the same-person check answers.
		if isUniqueViolation(err, "toolbox_attendance_pkey") || isUniqueViolation(err, "toolbox_attendance_once") {
			return errRetry
		}
		if err != nil {
			return err
		}
		created = true
		a, err = getAttendance(ctx, tx, orgID, site.ID, id)
		return err
	})
	return a, created, err
}

// CheckSigningWindow: from the start to the end, or to SigningWindow after the
// start while there's no end, across midnight. The trigger checks the same
// (000023).
func CheckSigningWindow(start time.Time, end *time.Time, at time.Time) error {
	switch {
	case at.Before(start):
		return ErrSignedBeforeStart
	case end != nil && at.After(*end):
		return ErrSignedAfterEnd
	case end == nil && at.After(start.Add(SigningWindow)):
		return ErrSignedTooLate
	}
	return nil
}

func normaliseSign(req *SignRequest, now time.Time) error {
	switch {
	case !textOK(&req.Name, 200):
		return ErrBadName
	case !textOK(req.Firm, 200):
		return ErrBadFirm
	case !textOK(req.ClockNumber, 50):
		return ErrBadClockNumber
	case !slices.Contains(Methods, req.Method):
		return ErrBadMethod
	case req.ClientSignedAt.IsZero():
		return ErrNoSignedAt
	case req.ClientSignedAt.After(now.Add(clockSkew)):
		return ErrSignedInFuture
	case !deviceOK(req.DeviceID):
		return ErrBadDeviceID
	}
	req.ClientSignedAt = micro(req.ClientSignedAt)
	return nil
}

func sameSignature(a *Attendance, talkID, by string, req SignRequest) bool {
	return a.TalkID == talkID && a.RecordedBy == by && a.Name == req.Name && equalPtr(a.Firm, req.Firm) &&
		equalPtr(a.ClockNumber, req.ClockNumber) && a.Method == req.Method && a.ClientSignedAt.Equal(req.ClientSignedAt) &&
		equalPtr(a.DeviceID, req.DeviceID)
}

// Retract withdraws a signature: the person who recorded it, or a lead
// (403 otherwise). Repeating it with the same reason answers 200.
func (s *Service) Retract(ctx context.Context, orgID, userID, role, siteID, talkID, attendanceID string, req RetractRequest) (*Attendance, bool, error) {
	tid, err := uuid.Parse(talkID)
	if err != nil {
		return nil, false, ErrAttendanceNotFound
	}
	aid, err := uuid.Parse(attendanceID)
	if err != nil {
		return nil, false, ErrAttendanceNotFound
	}
	now := time.Now()
	switch {
	case !textOK(&req.Reason, 500):
		return nil, false, ErrBadReason
	case req.ClientRetractedAt.IsZero():
		return nil, false, ErrNoRetractedAt
	case req.ClientRetractedAt.After(now.Add(clockSkew)):
		return nil, false, ErrRetractedInFuture
	}
	req.ClientRetractedAt = micro(req.ClientRetractedAt)
	var (
		a       *Attendance
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
			if err != nil {
				return err
			}
			a, err = getAttendance(ctx, tx, orgID, site.ID, aid.String())
			if isNotFound(err) || (err == nil && a.TalkID != tid.String()) {
				return ErrAttendanceNotFound
			}
			if err != nil {
				return err
			}
			if r := a.Retraction; r != nil {
				if r.Reason != req.Reason {
					return ErrAlreadyRetracted
				}
				return nil
			}
			if a.RecordedBy != userID && !isLead(role) {
				return ErrCantRetract
			}
			err = insertRetraction(ctx, tx, orgID, site.ID, a.ID, userID, req)
			if isUniqueViolation(err, "toolbox_attendance_retractions_pkey") {
				return errRetry
			}
			if err != nil {
				return err
			}
			created = true
			a, err = getAttendance(ctx, tx, orgID, site.ID, a.ID)
			return err
		})
	})
	if err = mapRule(err); isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("retract signature: %w", err)
	}
	return a, created, nil
}

// Get returns a talk with its photos and attendance. Works on archived sites.
func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID, talkID string) (*Talk, error) {
	var t *Talk
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		t, err = view(ctx, tx, orgID, site.ID, talkID)
		if isNotFound(err) {
			return ErrTalkNotFound
		}
		return err
	})
	return finish(t, err, "get talk")
}

// List is a site's talks, newest first, optionally on one local date. The
// query is checked before the site.
func (s *Service) List(ctx context.Context, orgID, userID, role, siteID, date string, limit int) ([]TalkSummary, error) {
	if date != "" {
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return nil, ErrBadDate
		}
	}
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}
	var out []TalkSummary
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = listTalks(ctx, tx, orgID, listFilter{SiteID: site.ID, Date: date, Limit: limit})
		return err
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list talks: %w", err)
	}
	return out, nil
}

func view(ctx context.Context, db DBTX, orgID, siteID, id string) (*Talk, error) {
	h, err := getHeader(ctx, db, orgID, siteID, id)
	if err != nil {
		return nil, err
	}
	t := &Talk{TalkHeader: h.TalkHeader}
	if t.Photos, err = listPhotos(ctx, db, orgID, id); err != nil {
		return nil, err
	}
	if t.Attendance, err = listAttendance(ctx, db, orgID, id); err != nil {
		return nil, err
	}
	return t, nil
}

func finish(t *Talk, err error, prefix string) (*Talk, error) {
	if err = mapRule(err); isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}
	return t, nil
}

// mapRule turns a rule the database enforced into the API's error, for the
// ones a race can reach past the checks above.
func mapRule(err error) error {
	switch {
	case isUniqueViolation(err, "toolbox_attendance_once"):
		return ErrAlreadySigned
	case isRule(err, "people have signed it"):
		return ErrTalkSigned
	case isRule(err, "signed before the talk started"):
		return ErrSignedBeforeStart
	case isRule(err, "signed after the talk ended"):
		return ErrSignedAfterEnd
	case isRule(err, "signed more than 12 hours after the talk started"):
		return ErrSignedTooLate
	case isRule(err, "ended_at is before a signature"):
		return ErrEndsBeforeSigned
	case isRule(err, "paper attendance needs a photo of the signed register"):
		return ErrNeedsRegisterPhoto
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
	ErrTalkNotFound, ErrAttendanceNotFound,
	ErrBadTalkID, ErrTalkIDReused, ErrUnknownTopic, ErrTopicRequired, ErrBadTopic, ErrBadPresenter, ErrBadNotes,
	ErrNoStartedAt, ErrStartedInFuture, ErrEndedInFuture, ErrEndsBeforeStart, ErrEndsTooLate, ErrEndsBeforeSigned, ErrBadDeviceID, ErrTalkSigned, ErrStartedAfterClose,
	ErrBadEvidenceID, ErrBadKind, ErrEvidenceNotFound, ErrEvidenceRejected, ErrEvidenceNotVerified, ErrPhotoKindDiffers,
	ErrBadAttendanceID, ErrAttendanceIDReused, ErrBadName, ErrBadFirm, ErrBadClockNumber, ErrBadMethod, ErrNoSignedAt,
	ErrSignedInFuture, ErrSignedBeforeStart, ErrSignedAfterEnd, ErrSignedTooLate, ErrAlreadySigned, ErrNeedsRegisterPhoto, ErrBadReason,
	ErrNoRetractedAt, ErrRetractedInFuture, ErrCantRetract, ErrAlreadyRetracted, ErrBadDate, ErrBadLimit,
}

func micro(t time.Time) time.Time { return t.Truncate(time.Microsecond).UTC() }

// textOK: absent, or 1 to max characters of valid UTF-8 without NUL, not
// only spaces (the columns count characters and trim spaces).
func textOK(s *string, max int) bool {
	if s == nil {
		return true
	}
	n := utf8.RuneCountInString(*s)
	return utf8.ValidString(*s) && n >= 1 && n <= max && !strings.ContainsRune(*s, 0) && strings.Trim(*s, " ") != ""
}

func deviceOK(s *string) bool { return s == nil || (len(*s) >= 1 && len(*s) <= 200) }

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

// IdentityKey is identity_key (000023) computed in Go: name|firm|clock
// number, each through normalise. A test compares it with the column.
func IdentityKey(name string, firm, clock *string) string {
	opt := func(s *string) string {
		if s == nil {
			return ""
		}
		return normalise(*s)
	}
	return normalise(name) + "|" + opt(firm) + "|" + opt(clock)
}

var spaceRun = regexp.MustCompile(`[\t\n\v\f\r ]+`)

// normalise mirrors toolbox_norm (000023): whitespace runs become one space,
// the ends are trimmed, case is lowered.
func normalise(s string) string {
	return strings.ToLower(strings.Trim(spaceRun.ReplaceAllString(s, " "), " "))
}
