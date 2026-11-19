package workers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/files"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

type Service struct {
	pool  *pgxpool.Pool
	sites *sites.Service
	files *files.Service
	reg   *regimes.Registry
}

func NewService(pool *pgxpool.Pool, sitesSvc *sites.Service, filesSvc *files.Service, reg *regimes.Registry) *Service {
	return &Service{pool: pool, sites: sitesSvc, files: filesSvc, reg: reg}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

var errRetry = errors.New("retry")

func once(fn func() error) error {
	if err := fn(); !errors.Is(err, errRetry) {
		return err
	}
	return fn()
}

// Regimes lists the loaded regimes, for picking the one a record names (D1).
func (s *Service) Regimes() []RegimeSummary {
	out := []RegimeSummary{}
	for _, code := range s.reg.Codes() {
		ds, _ := s.reg.Get(code)
		r := ds.Regime
		out = append(out, RegimeSummary{Code: r.Code, Name: r.Name, Edition: r.Edition, Jurisdiction: r.Jurisdiction})
	}
	return out
}

func (s *Service) Types(code string) (*RegimeTypes, error) {
	ds, ok := s.reg.Get(code)
	if !ok {
		return nil, ErrRegimeNotFound
	}
	return &RegimeTypes{RegimeCode: code, CompetencyTypes: ds.Workforce.CompetencyTypes, AppointmentTypes: ds.Workforce.AppointmentTypes,
		Medical: ds.Workforce.Medical, DocumentTypes: ds.DocumentTypes, Reminders: ds.Reminders}, nil
}

func (s *Service) List(ctx context.Context, orgID, userID string, active *bool, q string, limit int) ([]Worker, error) {
	if limit < 1 || limit > 500 {
		return nil, ErrBadLimit
	}
	var out []Worker
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var err error
		out, err = listWorkers(ctx, tx, orgID, active, q, limit)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	return out, nil
}

// Put creates a worker under the device's id (anyone who signs people in),
// or edits one (owner or SHE officer). An identical repeat answers 200.
func (s *Service) Put(ctx context.Context, orgID, userID, role, workerID string, req WorkerRequest) (*Worker, bool, error) {
	id, err := uuid.Parse(workerID)
	if err != nil {
		return nil, false, ErrBadWorkerID
	}
	if ps := ValidateWorker(req); len(ps) > 0 {
		return nil, false, &Problem{ps[0]}
	}
	var (
		w       *Worker
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			existing, err := GetWorker(ctx, tx, orgID, id.String())
			switch {
			case err == nil:
				active := existing.Active
				if req.Active != nil {
					active = *req.Active
				}
				if sameWorker(existing, req, active) {
					w = existing
					return nil
				}
				if !isLead(role) {
					return ErrCantEdit
				}
				if err := updateWorker(ctx, tx, orgID, existing.ID, userID, req, active); err != nil {
					return clashOr(err)
				}
			case !isNotFound(err):
				return err
			default:
				err = InsertWorker(ctx, tx, orgID, id.String(), userID, req)
				if isUniqueViolation(err, "workers_pkey") {
					return errRetry
				}
				if err != nil {
					return clashOr(err)
				}
				created = true
			}
			w, err = GetWorker(ctx, tx, orgID, id.String())
			return err
		})
	})
	if errors.Is(err, errRetry) {
		err = ErrIDReused
	}
	var c *Clash
	if errors.As(err, &c) {
		lookErr := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			other, err := WorkerByKey(ctx, tx, orgID, IdentityKey(req))
			if err == nil {
				c.WorkerID = other.ID
			}
			return err
		})
		if lookErr != nil {
			return nil, false, fmt.Errorf("put worker: find the clash: %w", lookErr)
		}
	}
	if isSentinel(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("put worker: %w", err)
	}
	return w, created, nil
}

// Clash is the 409 for an identity key already on the register.
type Clash struct{ WorkerID string }

func (c *Clash) Error() string { return ErrIdentityTaken.Error() }

func clashOr(err error) error {
	if !isUniqueViolation(err, "workers_identity") {
		return err
	}
	// The statement failed, so the transaction is aborted; a new one finds
	// the worker it clashed with.
	return &Clash{}
}

func sameWorker(w *Worker, r WorkerRequest, active bool) bool {
	return w.Name == r.Name && equalPtr(w.Firm, r.Firm) && equalPtr(w.ClockNumber, r.ClockNumber) && equalPtr(w.Trade, r.Trade) && w.Active == active
}

// MatchOrCreate is the sign-in's on-the-spot path (D11), in the caller's
// transaction: the register worker with req's identity key if there is one
// (matched true), else a new worker under workerID.
func MatchOrCreate(ctx context.Context, tx DBTX, orgID, userID, workerID string, req WorkerRequest) (*Worker, bool, error) {
	if ps := ValidateWorker(req); len(ps) > 0 {
		return nil, false, &Problem{ps[0]}
	}
	// The id is already a worker: the same person again (a retry), or the
	// phone reused the id for someone else (409).
	if existing, err := GetWorker(ctx, tx, orgID, workerID); err == nil {
		if existing.Name != req.Name || !equalPtr(existing.Firm, req.Firm) || !equalPtr(existing.ClockNumber, req.ClockNumber) ||
			(req.Trade != nil && !equalPtr(existing.Trade, req.Trade)) {
			return nil, false, ErrIDReused
		}
		return existing, false, nil
	} else if !isNotFound(err) {
		return nil, false, err
	}
	w, err := WorkerByKey(ctx, tx, orgID, IdentityKey(req))
	if err == nil {
		return w, true, nil
	}
	if !isNotFound(err) {
		return nil, false, err
	}
	if err := InsertWorker(ctx, tx, orgID, workerID, userID, req); err != nil {
		return nil, false, err
	}
	w, err = GetWorker(ctx, tx, orgID, workerID)
	return w, false, err
}

// Get is a worker with every competency and medical, renewals and
// retractions included, each with its state.
func (s *Service) Get(ctx context.Context, orgID, userID, workerID string) (*WorkerDetail, error) {
	var d *WorkerDetail
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		w, err := GetWorker(ctx, tx, orgID, workerID)
		if isNotFound(err) {
			return ErrWorkerNotFound
		}
		if err != nil {
			return err
		}
		d = &WorkerDetail{Worker: *w}
		if d.Competencies, err = listCompetencies(ctx, tx, orgID, `r.worker_id = $2`, w.ID); err != nil {
			return err
		}
		if d.Medicals, err = listMedicals(ctx, tx, orgID, `r.worker_id = $2`, w.ID); err != nil {
			return err
		}
		now := time.Now()
		for i := range d.Competencies {
			s.setState(&d.Competencies[i].RecordHeader, d.Competencies[i].ExpiresOn, now)
		}
		for i := range d.Medicals {
			s.setState(&d.Medicals[i].RecordHeader, &d.Medicals[i].ExpiresOn, now)
		}
		return nil
	})
	if isSentinel(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get worker: %w", err)
	}
	return d, nil
}

func (s *Service) setState(h *RecordHeader, expires *string, now time.Time) {
	h.State = RecordState(s.reg, h, expires, now)
}

// RecordState: retracted, then superseded, then by the expiry date against
// today in the record's regime: expired, expiring within the first reminder
// lead time, or valid.
func RecordState(reg *regimes.Registry, h *RecordHeader, expires *string, now time.Time) string {
	switch {
	case h.Retraction != nil:
		return StateRetracted
	case h.SupersededBy != nil:
		return StateSuperseded
	case expires == nil:
		return StateValid
	}
	ds, ok := reg.Get(h.RegimeCode)
	if !ok {
		return StateValid
	}
	loc, err := time.LoadLocation(ds.Regime.TimeZone)
	if err != nil {
		return StateValid
	}
	exp, err := time.ParseInLocation(time.DateOnly, *expires, loc)
	if err != nil {
		return StateValid
	}
	y, m, d := now.In(loc).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch {
	case exp.Before(today):
		return StateExpired
	case len(ds.Reminders.WorkerExpiryDays) > 0 && !exp.After(today.AddDate(0, 0, ds.Reminders.WorkerExpiryDays[0])):
		return StateExpiring
	}
	return StateValid
}

// WarningsFor is what a sign-in shows for a worker (D18), in the caller's
// transaction: no current, unexpired medical; an unfit outcome; a current
// competency expired.
func WarningsFor(ctx context.Context, tx DBTX, reg *regimes.Registry, orgID, workerID string, now time.Time) ([]string, error) {
	meds, err := listMedicals(ctx, tx, orgID, `r.worker_id = $2 AND r.retracted_at IS NULL`, workerID)
	if err != nil {
		return nil, err
	}
	comps, err := listCompetencies(ctx, tx, orgID, `r.worker_id = $2 AND r.retracted_at IS NULL`, workerID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	valid, unfit := false, false
	for _, m := range meds {
		st := RecordState(reg, &m.RecordHeader, &m.ExpiresOn, now)
		if st == StateValid || st == StateExpiring {
			valid = true
			unfit = unfit || m.Outcome == OutcomeUnfit
		}
	}
	if !valid {
		out = append(out, WarningNoValidMedical)
	}
	if unfit {
		out = append(out, WarningMedicalUnfit)
	}
	for _, c := range comps {
		if RecordState(reg, &c.RecordHeader, c.ExpiresOn, now) == StateExpired {
			out = append(out, WarningCompetencyExpired)
			break
		}
	}
	return out, nil
}

// PutCompetency records a competency under the device's id. Supervisors
// capture them on site too.
func (s *Service) PutCompetency(ctx context.Context, orgID, userID, workerID, recordID string, req CompetencyRequest) (*Competency, bool, error) {
	rid, err := uuid.Parse(recordID)
	if err != nil {
		return nil, false, ErrBadRecordID
	}
	req.ClientRecordedAt = micro(req.ClientRecordedAt)
	if ps := ValidateCompetency(s.reg, req, time.Now()); len(ps) > 0 {
		return nil, false, &Problem{ps[0]}
	}
	var (
		c       *Competency
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			w, err := s.worker(ctx, tx, orgID, workerID)
			if err != nil {
				return err
			}
			if existing, err := getCompetency(ctx, tx, orgID, rid.String()); err == nil {
				if !sameCompetency(existing, w.ID, userID, req) {
					return ErrIDReused
				}
				c = existing
				return nil
			} else if !isNotFound(err) {
				return err
			}
			if found, err := recordAnywhere(ctx, tx, orgID, rid.String()); err != nil || found {
				return orErr(err, ErrIDReused)
			}
			if err := files.Attachable(ctx, tx, orgID, req.FileID, files.PurposeCompetency, nil); err != nil {
				return err
			}
			if req.SupersedesID != nil {
				prev, err := getCompetency(ctx, tx, orgID, *req.SupersedesID)
				if err := checkTarget(err, prev, w.ID); err != nil {
					return err
				}
				if prev.TypeKey != req.TypeKey || prev.RegimeCode != req.RegimeCode {
					return ErrSupersedeTarget
				}
			}
			err = insertCompetency(ctx, tx, orgID, w.ID, rid.String(), userID, req)
			if err := recordInsertErr(err, "worker_competencies"); err != nil {
				return err
			}
			created = true
			c, err = getCompetency(ctx, tx, orgID, rid.String())
			return err
		})
	})
	if err = finish(err, "put competency"); err != nil {
		return nil, false, err
	}
	s.setState(&c.RecordHeader, c.ExpiresOn, time.Now())
	return c, created, nil
}

// PutMedical records a medical (D6): outcome and dates, and the certificate.
func (s *Service) PutMedical(ctx context.Context, orgID, userID, workerID, recordID string, req MedicalRequest) (*Medical, bool, error) {
	rid, err := uuid.Parse(recordID)
	if err != nil {
		return nil, false, ErrBadRecordID
	}
	req.ClientRecordedAt = micro(req.ClientRecordedAt)
	if ps := ValidateMedical(s.reg, req, time.Now()); len(ps) > 0 {
		return nil, false, &Problem{ps[0]}
	}
	var (
		m       *Medical
		created bool
	)
	err = once(func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			w, err := s.worker(ctx, tx, orgID, workerID)
			if err != nil {
				return err
			}
			if existing, err := getMedical(ctx, tx, orgID, rid.String()); err == nil {
				if !sameMedical(existing, w.ID, userID, req) {
					return ErrIDReused
				}
				m = existing
				return nil
			} else if !isNotFound(err) {
				return err
			}
			if found, err := recordAnywhere(ctx, tx, orgID, rid.String()); err != nil || found {
				return orErr(err, ErrIDReused)
			}
			if err := files.Attachable(ctx, tx, orgID, req.FileID, files.PurposeMedical, nil); err != nil {
				return err
			}
			if req.SupersedesID != nil {
				prev, err := getMedical(ctx, tx, orgID, *req.SupersedesID)
				if err := checkTarget(err, prev, w.ID); err != nil {
					return err
				}
			}
			err = insertMedical(ctx, tx, orgID, w.ID, rid.String(), userID, req)
			if err := recordInsertErr(err, "worker_medicals"); err != nil {
				return err
			}
			created = true
			m, err = getMedical(ctx, tx, orgID, rid.String())
			return err
		})
	})
	if err = finish(err, "put medical"); err != nil {
		return nil, false, err
	}
	s.setState(&m.RecordHeader, &m.ExpiresOn, time.Now())
	return m, created, nil
}

type headed interface{ head() *RecordHeader }

func (c *Competency) head() *RecordHeader { return &c.RecordHeader }
func (m *Medical) head() *RecordHeader    { return &m.RecordHeader }

// checkTarget: the record renewed is live, the same worker's, not renewed.
func checkTarget[T headed](err error, prev T, workerID string) error {
	if isNotFound(err) {
		return ErrSupersedeTarget
	}
	if err != nil {
		return err
	}
	h := prev.head()
	switch {
	case h.WorkerID != workerID || h.Retraction != nil:
		return ErrSupersedeTarget
	case h.SupersededBy != nil:
		return ErrAlreadyRenewed
	}
	return nil
}

func recordInsertErr(err error, table string) error {
	switch {
	case err == nil:
		return nil
	case isUniqueViolation(err, table+"_pkey"):
		return errRetry
	case isUniqueViolation(err, table+"_one_successor"):
		return ErrAlreadyRenewed
	case isUniqueViolation(err, table+"_file_id_key"):
		return ErrFileUsed
	case isRule(err, "a retracted record can't be renewed"):
		return ErrSupersedeTarget
	}
	return err
}

var ErrFileUsed = errors.New("file already attached to another record")

func sameCompetency(c *Competency, workerID, by string, r CompetencyRequest) bool {
	return c.WorkerID == workerID && c.RecordedBy == by && c.RegimeCode == r.RegimeCode && c.TypeKey == r.TypeKey &&
		equalPtr(c.Number, r.Number) && equalPtr(c.Issuer, r.Issuer) && c.IssuedOn == r.IssuedOn && equalPtr(c.ExpiresOn, r.ExpiresOn) &&
		c.FileID == r.FileID && equalPtr(c.SupersedesID, r.SupersedesID) && c.ClientRecordedAt.Equal(r.ClientRecordedAt) &&
		equalPtr(c.DeviceID, r.DeviceID)
}

func sameMedical(m *Medical, workerID, by string, r MedicalRequest) bool {
	return m.WorkerID == workerID && m.RecordedBy == by && m.RegimeCode == r.RegimeCode && m.Outcome == r.Outcome &&
		m.ExaminedOn == r.ExaminedOn && m.ExpiresOn == r.ExpiresOn && equalPtr(m.PracticeNumber, r.PracticeNumber) &&
		m.FileID == r.FileID && equalPtr(m.SupersedesID, r.SupersedesID) && m.ClientRecordedAt.Equal(r.ClientRecordedAt) &&
		equalPtr(m.DeviceID, r.DeviceID)
}

func (s *Service) worker(ctx context.Context, tx pgx.Tx, orgID, workerID string) (*Worker, error) {
	w, err := GetWorker(ctx, tx, orgID, workerID)
	if isNotFound(err) {
		return nil, ErrWorkerNotFound
	}
	return w, err
}

// Retract withdraws a competency or medical entered in error: an owner or
// SHE officer, with a reason. Repeating it with the same reason is 200.
func (s *Service) Retract(ctx context.Context, orgID, userID, role, kind, workerID, recordID string, req RetractRequest) (any, bool, error) {
	if !textOK(&req.Reason, 500) {
		return nil, false, ErrBadReason
	}
	if !isLead(role) {
		return nil, false, ErrCantRetract
	}
	table := tableCompetencies
	if kind == kindMedical {
		table = tableMedicals
	}
	var (
		out     any
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		w, err := s.worker(ctx, tx, orgID, workerID)
		if err != nil {
			return err
		}
		read := func() (*RecordHeader, error) {
			if kind == kindMedical {
				m, err := getMedical(ctx, tx, orgID, recordID)
				if err == nil {
					s.setState(&m.RecordHeader, &m.ExpiresOn, time.Now())
					out = m
					return &m.RecordHeader, nil
				}
				return nil, err
			}
			c, err := getCompetency(ctx, tx, orgID, recordID)
			if err == nil {
				s.setState(&c.RecordHeader, c.ExpiresOn, time.Now())
				out = c
				return &c.RecordHeader, nil
			}
			return nil, err
		}
		h, err := read()
		if isNotFound(err) || (err == nil && h.WorkerID != w.ID) {
			return ErrRecordNotFound
		}
		if err != nil {
			return err
		}
		if r := h.Retraction; r != nil {
			if r.Reason != req.Reason {
				return ErrAlreadyRetracted
			}
			return nil
		}
		if err := retractRecord(ctx, tx, table, orgID, h.ID, userID, req.Reason); err != nil {
			return err
		}
		created = true
		_, err = read()
		return err
	})
	if err = finish(err, "retract record"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

const (
	kindCompetency = "competency"
	kindMedical    = "medical"
)

// ViewFile logs a view of a record's certificate and returns a short-lived
// URL. A medical certificate opens only for an owner or SHE officer
// (files.LogView enforces it).
func (s *Service) ViewFile(ctx context.Context, orgID, userID, role, kind, workerID, recordID string, client evidence.Client) (*evidence.ViewURL, error) {
	var f *files.File
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var h *RecordHeader
		if kind == kindMedical {
			m, err := getMedical(ctx, tx, orgID, recordID)
			if err != nil {
				return orNotFound(err)
			}
			h = &m.RecordHeader
		} else {
			c, err := getCompetency(ctx, tx, orgID, recordID)
			if err != nil {
				return orNotFound(err)
			}
			h = &c.RecordHeader
		}
		if h.WorkerID != workerID {
			return ErrRecordNotFound
		}
		var err error
		f, err = files.LogView(ctx, tx, orgID, userID, role, h.FileID, client)
		return err
	})
	if err = finish(err, "view record file"); err != nil {
		return nil, err
	}
	return s.files.Presign(ctx, f)
}

func orNotFound(err error) error {
	if isNotFound(err) {
		return ErrRecordNotFound
	}
	return err
}

// Link links a toolbox signature to a worker by hand (D7): an owner or SHE
// officer, on a site they can see.
func (s *Service) Link(ctx context.Context, orgID, userID, role, siteID, attendanceID string, req LinkRequest) (*Link, bool, error) {
	if !isLead(role) {
		return nil, false, ErrCantLink
	}
	var (
		l       *Link
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if as, err := attendanceSite(ctx, tx, orgID, attendanceID); isNotFound(err) || (err == nil && as != site.ID) {
			return ErrAttendanceNotFound
		} else if err != nil {
			return err
		}
		if _, err := s.worker(ctx, tx, orgID, req.WorkerID); err != nil {
			return err
		}
		if cur, err := liveLink(ctx, tx, orgID, attendanceID); err == nil {
			if cur.WorkerID == req.WorkerID {
				l = cur
				return nil
			}
			return ErrAlreadyLinked
		} else if !isNotFound(err) {
			return err
		}
		id, err := insertLink(ctx, tx, orgID, site.ID, attendanceID, req.WorkerID, "manual", userID)
		if isUniqueViolation(err, "toolbox_attendance_workers_one_live") {
			return ErrAlreadyLinked
		}
		if err != nil {
			return err
		}
		created = true
		l, err = getLink(ctx, tx, orgID, id)
		return err
	})
	if err = finish(err, "link signature"); err != nil {
		return nil, false, err
	}
	return l, created, nil
}

// Unlink ends a signature's live link: "different person" (D7).
func (s *Service) Unlink(ctx context.Context, orgID, userID, role, siteID, attendanceID string, req UnlinkRequest) (*Link, error) {
	if !textOK(&req.Reason, 500) {
		return nil, ErrBadReason
	}
	if !isLead(role) {
		return nil, ErrCantLink
	}
	var l *Link
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		if as, err := attendanceSite(ctx, tx, orgID, attendanceID); isNotFound(err) || (err == nil && as != site.ID) {
			return ErrAttendanceNotFound
		} else if err != nil {
			return err
		}
		cur, err := liveLink(ctx, tx, orgID, attendanceID)
		if isNotFound(err) {
			return ErrNotLinked
		}
		if err != nil {
			return err
		}
		if err := unlink(ctx, tx, orgID, cur.ID, userID, req.Reason); err != nil {
			return err
		}
		l, err = getLink(ctx, tx, orgID, cur.ID)
		return err
	})
	if err = finish(err, "unlink signature"); err != nil {
		return nil, err
	}
	return l, nil
}

func finish(err error, prefix string) error {
	if err == nil || isSentinel(err) {
		return err
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func orErr(err, otherwise error) error {
	if err != nil {
		return err
	}
	return otherwise
}

func isSentinel(err error) bool {
	var p *Problem
	var c *Clash
	if errors.As(err, &p) || errors.As(err, &c) || files.IsSentinel(err) {
		return true
	}
	return slices.ContainsFunc(Sentinels, func(s error) bool { return errors.Is(err, s) })
}

// Sentinels are the errors the handlers send; TestAPIDoc checks each is quoted.
var Sentinels = []error{
	sites.ErrSiteNotFound,
	ErrWorkerNotFound, ErrRecordNotFound, ErrRegimeNotFound, ErrAttendanceNotFound, ErrBadWorkerID, ErrBadRecordID, ErrBadActive,
	ErrBadLimit, ErrBadReason, ErrCantEdit, ErrCantRetract, ErrCantLink, ErrIdentityTaken, ErrIDReused, ErrSupersedeTarget,
	ErrAlreadyRenewed, ErrAlreadyRetracted, ErrAlreadyLinked, ErrNotLinked, ErrFileUsed,
}

func micro(t time.Time) time.Time { return t.Truncate(time.Microsecond).UTC() }

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
