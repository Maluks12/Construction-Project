package invites

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrBlankName),
		errors.Is(err, ErrContactRequired),
		errors.Is(err, ErrInvalidInvite):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrWrongPassword):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, ErrCannotInviteOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, ErrAlreadyMember),
		errors.Is(err, ErrContactConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInviteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		slog.Error("request failed", "path", c.Request.URL.Path, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func (h *Handler) Invite(c *gin.Context) {
	var req InviteRequest
	orgID := auth.OrgID(c)
	userID := auth.UserID(c)
	role := auth.Role(c)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.svc.Invite(c.Request.Context(), orgID, userID, role, req)

	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *Handler) ListPending(c *gin.Context) {
	orgID := auth.OrgID(c)
	invites, err := h.svc.ListPending(c.Request.Context(), orgID)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"invites": invites})
}

func (h *Handler) Revoke(c *gin.Context) {
	id := c.Param("id")
	orgID := auth.OrgID(c)

	err := h.svc.Revoke(c.Request.Context(), orgID, id)

	if err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) Preview(c *gin.Context) {
	var req PreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	preview, err := h.svc.Preview(c.Request.Context(), req.Token)

	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, preview)
}

func (h *Handler) Accept(c *gin.Context) {
	var req AcceptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokens, err := h.svc.Accept(c.Request.Context(), req, c.Request.UserAgent())

	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, tokens)
}
