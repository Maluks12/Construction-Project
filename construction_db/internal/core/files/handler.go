package files

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
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

// WriteError maps this package's errors; record packages that attach or
// show files use it for those.
func WriteError(c *gin.Context, err error) bool {
	var rej *Rejected
	switch {
	case errors.As(err, &rej):
		c.JSON(http.StatusBadRequest, gin.H{"error": rej.Error(), "rejected_reason": rej.Reason})
	case is(err, ErrCantUpload, ErrCantViewHealth):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case errors.Is(err, ErrFileNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrIDReused, ErrNotUploaded, ErrNotVerified):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case isSentinel(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		return false
	}
	return true
}

func writeError(c *gin.Context, err error) {
	if !WriteError(c, err) {
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func status(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func (h *Handler) Declare(c *gin.Context) {
	var req DeclareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	f, created, err := h.svc.Declare(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("fileId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), f)
}

func (h *Handler) Complete(c *gin.Context) {
	f, created, err := h.svc.Complete(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("fileId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), f)
}

func (h *Handler) Get(c *gin.Context) {
	f, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("fileId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, f)
}
