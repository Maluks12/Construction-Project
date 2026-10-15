package sites

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
	case errors.Is(err, ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	default:
		slog.Error("request failed", "path", c.Request.URL.Path, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func (h *Handler) List(c *gin.Context) {
	orgID := auth.OrgID(c)

	sites, err := h.svc.List(c.Request.Context(), orgID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sites": sites})
}

func (h *Handler) Get(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")

	site, err := h.svc.Get(c.Request.Context(), orgID, id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, site)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateSiteRequest
	orgID := auth.OrgID(c)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	site, err := h.svc.Create(c.Request.Context(), orgID, req)

	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, site)
}

func (h *Handler) Update(c *gin.Context) {
	var req UpdateSiteRequest
	orgID := auth.OrgID(c)
	id := c.Param("id")
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	site, err := h.svc.Update(c.Request.Context(), orgID, id, req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, site)
}

func (h *Handler) Archive(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")

	err := h.svc.Archive(c.Request.Context(), orgID, id)

	if err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
