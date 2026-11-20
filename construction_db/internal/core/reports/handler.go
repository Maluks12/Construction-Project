package reports

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/platform/httpapi"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func (h *Handler) Site(c *gin.Context) {
	out, err := h.svc.Site(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) Org(c *gin.Context) {
	out, err := h.svc.Org(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
