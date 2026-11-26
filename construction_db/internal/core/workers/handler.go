package workers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/files"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/httpapi"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func is(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

func writeError(c *gin.Context, err error) {
	var p *Problem
	var cl *Clash
	var ch *Changed
	switch {
	case errors.As(err, &ch):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "current": ch.Current})
	case errors.As(err, &p):
		c.JSON(http.StatusBadRequest, gin.H{"error": p.FieldProblem.Error, "field": p.Field})
	case errors.As(err, &cl):
		c.JSON(http.StatusConflict, IdentityClash{Error: cl.Error(), WorkerID: cl.WorkerID})
	case is(err, ErrCantEdit, ErrCantRetract, ErrCantLink):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrWorkerNotFound, ErrRecordNotFound, ErrRegimeNotFound, ErrAttendanceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrIDReused, ErrAlreadyRenewed, ErrAlreadyRetracted, ErrAlreadyLinked, ErrNotLinked, ErrFileUsed):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case files.WriteError(c, err):
	case isSentinel(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func status(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func (h *Handler) Regimes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"regimes": h.svc.Regimes()})
}

func (h *Handler) Types(c *gin.Context) {
	t, err := h.svc.Types(c.Param("code"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *Handler) List(c *gin.Context) {
	limit := 200
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			writeError(c, ErrBadLimit)
			return
		}
		limit = n
	}
	var active *bool
	switch c.Query("active") {
	case "":
	case "true", "false":
		v := c.Query("active") == "true"
		active = &v
	default:
		writeError(c, ErrBadActive)
		return
	}
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), active, c.Query("q"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"workers": out})
}

func (h *Handler) Put(c *gin.Context) {
	var req WorkerRequest
	if !bind(c, &req) {
		return
	}
	w, created, err := h.svc.Put(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("workerId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), w)
}

func (h *Handler) Get(c *gin.Context) {
	d, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), c.Param("workerId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) PutCompetency(c *gin.Context) {
	var req CompetencyRequest
	if !bind(c, &req) {
		return
	}
	r, created, err := h.svc.PutCompetency(c.Request.Context(), auth.OrgID(c), auth.UserID(c), c.Param("workerId"), c.Param("recordId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), r)
}

func (h *Handler) PutMedical(c *gin.Context) {
	var req MedicalRequest
	if !bind(c, &req) {
		return
	}
	r, created, err := h.svc.PutMedical(c.Request.Context(), auth.OrgID(c), auth.UserID(c), c.Param("workerId"), c.Param("recordId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), r)
}

func (h *Handler) retract(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req RetractRequest
		if !bind(c, &req) {
			return
		}
		r, created, err := h.svc.Retract(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), kind,
			c.Param("workerId"), c.Param("recordId"), req)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(status(created), r)
	}
}

func (h *Handler) view(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, err := h.svc.ViewFile(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), kind,
			c.Param("workerId"), c.Param("recordId"), evidence.ClientOf(c))
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

func (h *Handler) Link(c *gin.Context) {
	var req LinkRequest
	if !bind(c, &req) {
		return
	}
	l, created, err := h.svc.Link(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("attendanceId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), l)
}

func (h *Handler) Unlink(c *gin.Context) {
	var req UnlinkRequest
	if !bind(c, &req) {
		return
	}
	l, err := h.svc.Unlink(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("attendanceId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, l)
}
