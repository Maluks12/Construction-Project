package exports

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/platform/httpapi"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func writeError(c *gin.Context, err error) {
	is := func(targets ...error) bool {
		for _, t := range targets {
			if errors.Is(err, t) {
				return true
			}
		}
		return false
	}
	switch {
	case is(ErrSiteNotFound, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(ErrBadID, ErrBadRange, ErrBadRequestedAt, ErrBadDeviceID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case is(ErrIDReused, ErrNotReady, ErrFailed, ErrExpired):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func (h *Handler) Queue(c *gin.Context) {
	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errBody.Error()})
		return
	}
	out, created, err := h.svc.Queue(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("exportId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, out)
}

func (h *Handler) Get(c *gin.Context) {
	out, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("exportId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) List(c *gin.Context) {
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"exports": out})
}

func (h *Handler) Download(c *gin.Context) {
	client := evidence.ClientOf(c)
	out, err := h.svc.Download(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("exportId"), client)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
