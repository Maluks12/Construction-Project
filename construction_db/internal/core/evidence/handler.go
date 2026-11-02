package evidence

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case errors.Is(err, ErrEvidenceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "evidence not found"})
	case errors.Is(err, ErrIDReused), errors.Is(err, ErrRejected):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrBadEvidenceID), errors.Is(err, ErrBadHash), errors.Is(err, ErrLocationPair),
		errors.Is(err, ErrBadDeviceID), errors.Is(err, ErrBadStatus), errors.Is(err, ErrBadLimit):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		slog.Error("request failed", "path", c.Request.URL.Path, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func (h *Handler) Declare(c *gin.Context) {
	var req DeclareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ev, created, err := h.svc.Declare(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("evidenceId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, ev)
}

func (h *Handler) Get(c *gin.Context) {
	ev, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("evidenceId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, ev)
}

func (h *Handler) List(c *gin.Context) {
	limit := 50
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			writeError(c, ErrBadLimit)
			return
		}
		limit = n
	}

	items, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Query("status"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"evidence": items})
}
