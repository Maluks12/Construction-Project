// Package appointments is a site's written appointments (CR 2014 reg 8 and
// the others in SOURCES; M11 D21): a register worker appointed to a type
// from the site regime, with the signed letter. Renewed by a new record
// superseding the old, retracted by a lead; nothing is overwritten.
// Migration 000028 enforces the rules; endpoints in docs/api/appointments.md.
package appointments

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/files"
	"construction_db/internal/core/sites"
	"construction_db/internal/core/workers"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/httpapi"
	"construction_db/internal/regimes"
)

type Request struct {
	WorkerID         string    `json:"worker_id"`
	TypeKey          string    `json:"type_key"`
	AppointedOn      string    `json:"appointed_on"`
	ExpiresOn        *string   `json:"expires_on"`
	FileID           string    `json:"file_id"`
	SupersedesID     *string   `json:"supersedes_id"`
	ClientRecordedAt time.Time `json:"client_recorded_at"`
	DeviceID         *string   `json:"device_id"`
}

type RetractRequest struct {
	Reason string `json:"reason"`
}

type Appointment struct {
	workers.RecordHeader
	SiteID      string  `json:"site_id"`
	TypeKey     string  `json:"type_key"`
	WorkerName  string  `json:"worker_name"`
	AppointedOn string  `json:"appointed_on"`
	ExpiresOn   *string `json:"expires_on"`
}

var (
	ErrNotFound         = errors.New("appointment not found")
	ErrBadID            = errors.New("appointment id must be a UUID")
	ErrBadType          = errors.New("type_key is not an appointment type of this site's regime")
	ErrBadDate          = errors.New("appointed_on and expires_on must be YYYY-MM-DD, expiry not before the appointment")
	ErrExpiryNeeded     = errors.New("expires_on is required for this type")
	ErrExpiryNone       = errors.New("this type has no expiry; leave expires_on out")
	ErrBadRecordedAt    = errors.New("client_recorded_at is required, and not in the future")
	ErrBadDeviceID      = errors.New("device_id must be 1-200 bytes")
	ErrBadReason        = errors.New("reason must be 1-500 characters")
	ErrCantWrite        = errors.New("only an owner or SHE officer can record or retract appointments")
	ErrIDReused         = errors.New("appointment id already used with different details")
	ErrSupersedeTarget  = errors.New("supersedes_id must be a live appointment of the same type on this site")
	ErrAlreadyRenewed   = errors.New("that appointment already has a successor")
	ErrAlreadyRetracted = errors.New("appointment already retracted with another reason")
	ErrFileUsed         = errors.New("file already attached to another appointment")
)

// Sentinels are the errors the handlers send; TestAPIDoc checks each is quoted.
var Sentinels = []error{ErrNotFound, ErrBadID, ErrBadType, ErrBadDate, ErrExpiryNeeded, ErrExpiryNone, ErrBadRecordedAt,
	ErrBadDeviceID, ErrBadReason, ErrCantWrite, ErrIDReused, ErrSupersedeTarget, ErrAlreadyRenewed, ErrAlreadyRetracted, ErrFileUsed}

type Service struct {
	deps edition.Deps
}

func NewService(deps edition.Deps) *Service { return &Service{deps: deps} }

func isLead(role string) bool { return role == auth.RoleOwner || role == auth.RoleSHEOfficer }

const selectSQL = `
	SELECT a.id::text, a.worker_id::text, a.regime_code, a.file_id::text, a.supersedes_id::text,
	       (SELECT s.id::text FROM site_appointments s WHERE s.supersedes_id = a.id AND s.retracted_at IS NULL),
	       a.recorded_by::text, a.client_recorded_at, a.recorded_at, a.device_id, a.retracted_at, a.retracted_by::text, a.retract_reason,
	       a.site_id::text, a.type_key, w.name, a.appointed_on::text, a.expires_on::text
	FROM site_appointments a JOIN workers w ON w.id = a.worker_id
	WHERE a.org_id = $1 AND a.site_id = $2`

func (s *Service) list(ctx context.Context, tx pgx.Tx, orgID, siteID, where string, args ...any) ([]Appointment, error) {
	rows, err := tx.Query(ctx, selectSQL+where+` ORDER BY a.type_key, a.appointed_on, a.recorded_at`, append([]any{orgID, siteID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Appointment{}
	now := s.deps.Clock.Now()
	for rows.Next() {
		var a Appointment
		var retAt *time.Time
		var retBy, reason *string
		if err := rows.Scan(&a.ID, &a.WorkerID, &a.RegimeCode, &a.FileID, &a.SupersedesID, &a.SupersededBy, &a.RecordedBy,
			&a.ClientRecordedAt, &a.RecordedAt, &a.DeviceID, &retAt, &retBy, &reason,
			&a.SiteID, &a.TypeKey, &a.WorkerName, &a.AppointedOn, &a.ExpiresOn); err != nil {
			return nil, err
		}
		if retAt != nil {
			a.Retraction = &workers.Retraction{Reason: *reason, RetractedBy: *retBy, RetractedAt: *retAt}
		}
		a.State = workers.RecordState(s.deps.Regimes, &a.RecordHeader, a.ExpiresOn, now)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) one(ctx context.Context, tx pgx.Tx, orgID, siteID, id string) (*Appointment, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrNotFound
	}
	out, err := s.list(ctx, tx, orgID, siteID, ` AND a.id = $3`, id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.deps.Pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func (s *Service) List(ctx context.Context, orgID, userID, role, siteID string) ([]Appointment, error) {
	var out []Appointment
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.deps.Sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		out, err = s.list(ctx, tx, orgID, site.ID, "")
		return err
	})
	return out, finish(err, "list appointments")
}

// Put records an appointment under the device's id (owner or SHE officer).
// An identical repeat answers 200.
func (s *Service) Put(ctx context.Context, orgID, userID, role, siteID, appointmentID string, req Request) (*Appointment, bool, error) {
	id, err := uuid.Parse(appointmentID)
	if err != nil {
		return nil, false, ErrBadID
	}
	if !isLead(role) {
		return nil, false, ErrCantWrite
	}
	appointed, err := time.Parse(time.DateOnly, req.AppointedOn)
	if err != nil {
		return nil, false, ErrBadDate
	}
	if req.ExpiresOn != nil {
		if exp, err := time.Parse(time.DateOnly, *req.ExpiresOn); err != nil || exp.Before(appointed) {
			return nil, false, ErrBadDate
		}
	}
	if req.ClientRecordedAt.IsZero() || req.ClientRecordedAt.After(s.deps.Clock.Now().Add(5*time.Minute)) {
		return nil, false, ErrBadRecordedAt
	}
	if req.DeviceID != nil && (len(*req.DeviceID) == 0 || len(*req.DeviceID) > 200) {
		return nil, false, ErrBadDeviceID
	}
	req.ClientRecordedAt = req.ClientRecordedAt.Truncate(time.Microsecond).UTC()
	var (
		out     *Appointment
		created bool
	)
	attempt := func() error {
		created = false
		return s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			sr, err := s.deps.Sites.RegimeInTx(ctx, tx, orgID, userID, role, siteID)
			if err != nil {
				return err
			}
			site := sr.Site.ID
			at, ok := sr.Data.AppointmentType(req.TypeKey)
			switch {
			case !ok:
				return ErrBadType
			case at.Expiry == regimes.ExpiryRequired && req.ExpiresOn == nil:
				return ErrExpiryNeeded
			case at.Expiry == regimes.ExpiryNone && req.ExpiresOn != nil:
				return ErrExpiryNone
			}
			if existing, err := s.one(ctx, tx, orgID, site, id.String()); err == nil {
				if !same(existing, userID, req) {
					return ErrIDReused
				}
				out = existing
				return nil
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
			var found bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM site_appointments WHERE org_id = $1 AND id = $2)`, orgID, id.String()).Scan(&found); err != nil {
				return err
			} else if found {
				return ErrIDReused
			}
			if _, err := workers.GetWorker(ctx, tx, orgID, req.WorkerID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) || isCode(err, "22P02") {
					return workers.ErrWorkerNotFound
				}
				return err
			}
			if err := files.Attachable(ctx, tx, orgID, req.FileID, files.PurposeAppointment, &site); err != nil {
				return err
			}
			if req.SupersedesID != nil {
				prev, err := s.one(ctx, tx, orgID, site, *req.SupersedesID)
				switch {
				case errors.Is(err, ErrNotFound):
					return ErrSupersedeTarget
				case err != nil:
					return err
				case prev.TypeKey != req.TypeKey || prev.Retraction != nil:
					return ErrSupersedeTarget
				case prev.SupersededBy != nil:
					return ErrAlreadyRenewed
				}
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO site_appointments (id, org_id, site_id, worker_id, regime_code, type_key, appointed_on, expires_on, file_id,
				                               supersedes_id, recorded_by, client_recorded_at, device_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
				id.String(), orgID, site, req.WorkerID, sr.Data.Regime.Code, req.TypeKey, req.AppointedOn, req.ExpiresOn, req.FileID,
				req.SupersedesID, userID, req.ClientRecordedAt, req.DeviceID)
			switch {
			case isUnique(err, "site_appointments_pkey"):
				return errRetry
			case isUnique(err, "site_appointments_one_successor"):
				return ErrAlreadyRenewed
			case isUnique(err, "site_appointments_file_id_key"):
				return ErrFileUsed
			case err != nil:
				return err
			}
			created = true
			out, err = s.one(ctx, tx, orgID, site, id.String())
			return err
		})
	}
	err = attempt()
	if errors.Is(err, errRetry) {
		if err = attempt(); errors.Is(err, errRetry) {
			err = ErrIDReused
		}
	}
	if err = finish(err, "put appointment"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

var errRetry = errors.New("retry")

func same(a *Appointment, by string, r Request) bool {
	eq := func(x, y *string) bool { return (x == nil && y == nil) || (x != nil && y != nil && *x == *y) }
	return a.WorkerID == r.WorkerID && a.TypeKey == r.TypeKey && a.AppointedOn == r.AppointedOn && eq(a.ExpiresOn, r.ExpiresOn) &&
		a.FileID == r.FileID && eq(a.SupersedesID, r.SupersedesID) && a.RecordedBy == by && a.ClientRecordedAt.Equal(r.ClientRecordedAt) &&
		eq(a.DeviceID, r.DeviceID)
}

// Retract withdraws an appointment recorded in error: an owner or SHE
// officer, with a reason. Repeating it is 200.
func (s *Service) Retract(ctx context.Context, orgID, userID, role, siteID, appointmentID string, req RetractRequest) (*Appointment, bool, error) {
	n := utf8.RuneCountInString(req.Reason)
	if n < 1 || n > 500 || strings.TrimSpace(req.Reason) == "" {
		return nil, false, ErrBadReason
	}
	if !isLead(role) {
		return nil, false, ErrCantWrite
	}
	var (
		out     *Appointment
		created bool
	)
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.deps.Sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		a, err := s.one(ctx, tx, orgID, site.ID, appointmentID)
		if err != nil {
			return err
		}
		if r := a.Retraction; r != nil {
			if r.Reason != req.Reason {
				return ErrAlreadyRetracted
			}
			out = a
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE site_appointments SET retracted_by = $3, retract_reason = $4 WHERE org_id = $1 AND id = $2`,
			orgID, a.ID, userID, req.Reason); err != nil {
			return err
		}
		created = true
		out, err = s.one(ctx, tx, orgID, site.ID, a.ID)
		return err
	})
	if err = finish(err, "retract appointment"); err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// View logs a view of the appointment letter and returns a short-lived URL.
func (s *Service) View(ctx context.Context, orgID, userID, role, siteID, appointmentID string, client evidence.Client) (*evidence.ViewURL, error) {
	var f *files.File
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		site, err := s.deps.Sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		a, err := s.one(ctx, tx, orgID, site.ID, appointmentID)
		if err != nil {
			return err
		}
		f, err = files.LogView(ctx, tx, orgID, userID, role, a.FileID, client)
		return err
	})
	if err = finish(err, "view appointment"); err != nil {
		return nil, err
	}
	return s.deps.Files.Presign(ctx, f)
}

func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func isCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

func isSentinel(err error) bool {
	return files.IsSentinel(err) || errors.Is(err, workers.ErrWorkerNotFound) || errors.Is(err, sites.ErrSiteNotFound) ||
		slices.ContainsFunc(Sentinels, func(s error) bool { return errors.Is(err, s) })
}

func finish(err error, prefix string) error {
	if err == nil || isSentinel(err) {
		return err
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// Register mounts the endpoints under the edition's group, behind onSite
// (the site's regime is a construction one).
func Register(r *gin.RouterGroup, svc *Service, onSite gin.HandlerFunc) {
	g := r.Group("/sites/:id/appointments",
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor), onSite)
	g.GET("", func(c *gin.Context) {
		out, err := svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"appointments": out})
	})
	// PUT: the device chooses the id, so sending it again is safe.
	g.PUT("/:appointmentId", func(c *gin.Context) {
		var req Request
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		a, created, err := svc.Put(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("appointmentId"), req)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(statusOf(created), a)
	})
	g.POST("/:appointmentId/retract", func(c *gin.Context) {
		var req RetractRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		a, created, err := svc.Retract(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("appointmentId"), req)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(statusOf(created), a)
	})
	g.POST("/:appointmentId/view", func(c *gin.Context) {
		u, err := svc.View(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("appointmentId"),
			evidence.ClientOf(c))
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, u)
	})
}

func statusOf(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrCantWrite):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case errors.Is(err, ErrNotFound), errors.Is(err, workers.ErrWorkerNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrIDReused), errors.Is(err, ErrAlreadyRenewed), errors.Is(err, ErrAlreadyRetracted), errors.Is(err, ErrFileUsed):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case files.WriteError(c, err):
	case isSentinel(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}
