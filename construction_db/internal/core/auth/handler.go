package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidCredentials),
		errors.Is(err, ErrInvalidRefreshToken),
		errors.Is(err, ErrSessionRevoked):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, ErrEmailTaken):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrOrgRequired), errors.Is(err, ErrNotMember):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		slog.Error("request failed", "path", c.Request.URL.Path, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokens, err := h.svc.Login(c.Request.Context(), req, c.Request.UserAgent())

	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, tokens)
}

func (h *Handler) Signup(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokens, err := h.svc.Signup(c.Request.Context(), req, c.Request.UserAgent())

	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, tokens)
}

func (h *Handler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokens, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)

	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, tokens)
}

func (h *Handler) Logout(c *gin.Context) {
	userID := UserID(c)
	sessionID := SessionID(c)

	if err := h.svc.Logout(c.Request.Context(), userID, sessionID); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) ListSessions(c *gin.Context) {
	userID := UserID(c)
	sessionID := SessionID(c)

	sessions, err := h.svc.ListSessions(c.Request.Context(), userID, sessionID)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

func (h *Handler) RevokeSession(c *gin.Context) {
	id := c.Param("id")

	revoked, err := h.svc.RevokeSession(c.Request.Context(), UserID(c), id)

	if err != nil {
		writeError(c, err)
		return
	}

	if !revoked {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Me(c *gin.Context) {
	resp := MeResponse{
		UserID:    UserID(c),
		OrgID:     OrgID(c),
		SessionID: SessionID(c),
	}

	c.JSON(http.StatusOK, resp)
}
