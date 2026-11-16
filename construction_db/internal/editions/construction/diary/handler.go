package diary

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
		evErr  *EvidenceError
		secErr *SectionError
	)
	switch {
	// These carry data the app acts on, next to the usual error text.
	case errors.As(err, &evErr):
		status := http.StatusBadRequest
		if errors.Is(err, ErrEvidenceNotVerified) {
			status = http.StatusConflict // still uploading: retry later
		}
		c.JSON(status, gin.H{"error": err.Error(), "evidence_ids": evErr.IDs})
	case errors.As(err, &secErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "section": secErr.Section, "row": secErr.Row, "field": secErr.Field})

	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case errors.Is(err, ErrDiaryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	// The request is fine; the stored state refuses it.
	case is(err, ErrRevisionIDReused, ErrDayAfterArchive, ErrDiaryClosed, ErrPastGrace, ErrAddendumIDReused, ErrDiaryOpen):
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

func (h *Handler) PutRevision(c *gin.Context) {
	var req RevisionRequest
	if !bind(c, &req) {
		return
	}
	d, created, err := h.svc.PutRevision(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("date"), c.Param("revisionId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), d)
}

func (h *Handler) PutAddendum(c *gin.Context) {
	var req AddendumRequest
	if !bind(c, &req) {
		return
	}
	a, created, err := h.svc.PutAddendum(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("date"), c.Param("addendumId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), a)
}

func (h *Handler) Get(c *gin.Context) {
	d, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("date"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) Revisions(c *gin.Context) {
	out, err := h.svc.Revisions(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("date"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revisions": out})
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
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Query("from"), c.Query("to"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"diaries": out})
}

func (h *Handler) Firms(c *gin.Context) {
	out, err := h.svc.Firms(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"firms": out})
}
