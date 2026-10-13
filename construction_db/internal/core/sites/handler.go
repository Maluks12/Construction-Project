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
