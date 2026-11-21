package incidents

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
	var (
		evErr    *EvidenceError
		typesErr *TypesError
		nextErr  *NotNextError
	)
	switch {
	// These carry data the app acts on, next to the usual error text.
	case errors.As(err, &evErr):
		status := http.StatusBadRequest
		if errors.Is(err, ErrEvidenceNotVerified) {
			status = http.StatusConflict // still uploading: retry later
		}
		c.JSON(status, gin.H{"error": err.Error(), "evidence_ids": evErr.IDs})
	case errors.As(err, &typesErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "taxonomy_codes": typesErr.Codes})
	case errors.As(err, &nextErr):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "latest_version": nextErr.LatestVersion})

	case errors.Is(err, ErrWeakeningNeedsLead):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrIncidentNotFound, ErrVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrSiteArchived, ErrIDReused, ErrVersionDiffers, ErrNotNextVersion, ErrStatementIDReused, ErrStatementCorrected):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case isSentinel(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

// bind decodes a JSON body; a malformed one is 400 with the decoder's text, as in checklists.
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

func limit(c *gin.Context) (int, bool) {
	s := c.Query("limit")
	if s == "" {
		return 50, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		writeError(c, ErrBadLimit)
		return 0, false
	}
	return n, true
}

func (h *Handler) Submit(c *gin.Context) {
	var req SubmitVersionRequest
	if !bind(c, &req) {
		return
	}
	inc, created, err := h.svc.Submit(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("incidentId"), c.Param("version"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), inc)
}

func (h *Handler) Get(c *gin.Context) {
	inc, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("incidentId"), c.Query("version"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, inc)
}

func (h *Handler) List(c *gin.Context) {
	n, ok := limit(c)
	if !ok {
		return
	}
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Query("type"), c.Query("reportable"), c.Query("open_deadlines"), n)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"incidents": out})
}

func (h *Handler) RecordStatement(c *gin.Context) {
	var req StatementRequest
	if !bind(c, &req) {
		return
	}
	st, created, err := h.svc.RecordStatement(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("incidentId"), c.Param("statementId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), st)
}
