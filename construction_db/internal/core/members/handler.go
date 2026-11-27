package members

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/platform/httpapi"
)

type Handler struct{ svc *Service }

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
	switch {
	case is(err, ErrMemberNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrSelf, ErrOwnerOnly):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case is(err, ErrLastOwner, ErrTransition, ErrInviteRole):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case is(err, ErrNothingToChange, ErrBadReason, ErrBadRole, ErrBadStatus, ErrBadStatusFilter,
		ErrSiteIDsRequired, ErrSiteIDsUnexpected, ErrBadSiteIDs):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func (h *Handler) List(c *gin.Context) {
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), c.Query("status"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": out})
}

func (h *Handler) Get(c *gin.Context) {
	out, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), c.Param("userId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) Update(c *gin.Context) {
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := h.svc.Update(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("userId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) History(c *gin.Context) {
	out, err := h.svc.History(c.Request.Context(), auth.OrgID(c), c.Param("userId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
