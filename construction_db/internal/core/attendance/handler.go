package attendance

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/core/workers"
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
	var st *Standing
	var p *workers.Problem
	switch {
	case errors.As(err, &st):
		c.JSON(http.StatusConflict, AlreadySignedIn{Error: st.Error(), SignInID: st.SignInID})
	case errors.As(err, &p):
		c.JSON(http.StatusBadRequest, gin.H{"error": p.FieldProblem.Error, "field": p.Field})
	case errors.Is(err, ErrCantRetract):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrSignInNotFound, workers.ErrWorkerNotFound, ErrNotOnRoster):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrIDReused, workers.ErrIDReused, ErrAlreadySignedOut, ErrAlreadyRetracted, ErrRetracted, ErrNeedsSheet, ErrSiteArchived,
		ErrEvidenceNotVerified, ErrSignOutAfterNext, ErrAlreadyRemoved):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
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

func (h *Handler) SignIn(c *gin.Context) {
	var req SignInRequest
	if !bind(c, &req) {
		return
	}
	s, created, err := h.svc.SignIn(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("signInId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), s)
}

func (h *Handler) SignOut(c *gin.Context) {
	var req SignOutRequest
	if !bind(c, &req) {
		return
	}
	s, created, err := h.svc.SignOut(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("signInId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), s)
}

func (h *Handler) Retract(c *gin.Context) {
	var req RetractRequest
	if !bind(c, &req) {
		return
	}
	s, created, err := h.svc.Retract(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("signInId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), s)
}

func (h *Handler) AddSheet(c *gin.Context) {
	s, created, err := h.svc.AddSheet(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("date"), c.Param("evidenceId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), s)
}

func (h *Handler) Day(c *gin.Context) {
	d, err := h.svc.Day(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Query("date"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) Roster(c *gin.Context) {
	out, err := h.svc.Roster(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Query("all") == "true")
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"roster": out})
}

func (h *Handler) AddToRoster(c *gin.Context) {
	e, created, err := h.svc.AddToRoster(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("workerId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), e)
}

func (h *Handler) RemoveFromRoster(c *gin.Context) {
	var req RemoveRequest
	if !bind(c, &req) {
		return
	}
	e, err := h.svc.RemoveFromRoster(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("workerId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, e)
}
