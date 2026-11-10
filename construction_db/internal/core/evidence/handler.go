package evidence

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/httpapi"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// clientOf is where the request came from, for the access log. ClientIP
// believes X-Forwarded-For only from the router's trusted proxies.
func clientOf(c *gin.Context) Client {
	return Client{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}
}

func writeError(c *gin.Context, err error) {
	var rej *RejectedError
	switch {
	case errors.As(err, &rej):
		// The code lets the app act on the reason without parsing the text.
		c.JSON(http.StatusBadRequest, gin.H{"error": rej.Error(), "rejected_reason": rej.Reason})
	case errors.Is(err, ErrNotUploaded):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
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
		httpapi.WriteUnexpected(c, "request failed", err)
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

func (h *Handler) Complete(c *gin.Context) {
	ev, created, err := h.svc.Complete(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("evidenceId"))
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

func (h *Handler) VerifyChain(c *gin.Context) {
	v, err := h.svc.VerifyChain(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) Get(c *gin.Context) {
	ev, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("evidenceId"), clientOf(c))
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
		c.Param("id"), c.Query("status"), limit, clientOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"evidence": items})
}
