package deadlines

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

func is(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

func writeError(c *gin.Context, err error) {
	var evErr *EvidenceError
	switch {
	case errors.As(err, &evErr):
		status := http.StatusBadRequest
		if errors.Is(err, ErrEvidenceNotVerified) {
			status = http.StatusConflict // still uploading: retry later
		}
		c.JSON(status, gin.H{"error": err.Error(), "evidence_ids": evErr.IDs})
	case errors.Is(err, ErrCantRetract):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrDeadlineNotFound, ErrSatisfactionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrSatisfactionIDReused, ErrAlreadySatisfied, ErrDeadlineWithdrawn, ErrRetractionDiffers):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case isSentinel(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func status(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func (h *Handler) List(c *gin.Context) {
	n := 50
	if s := c.Query("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			writeError(c, ErrBadLimit)
			return
		}
		n = v
	}
	out, err := h.svc.ListDeadlines(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Query("state"), c.Query("subject_type"), n)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deadlines": out})
}

func (h *Handler) Get(c *gin.Context) {
	d, err := h.svc.GetDeadline(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("deadlineId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) Satisfy(c *gin.Context) {
	var req SatisfactionRequest
	if !bind(c, &req) {
		return
	}
	d, created, err := h.svc.Satisfy(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("deadlineId"), c.Param("satisfactionId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), d)
}

func (h *Handler) Retract(c *gin.Context) {
	var req RetractRequest
	if !bind(c, &req) {
		return
	}
	d, created, err := h.svc.Retract(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("deadlineId"), c.Param("satisfactionId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), d)
}
