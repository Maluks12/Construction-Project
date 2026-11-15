package checklists

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

func writeError(c *gin.Context, err error) {
	var (
		evErr    *EvidenceError
		staleErr *StaleAnswerError
		incErr   *IncompleteError
	)
	switch {
	// These carry data the app acts on, next to the usual error text.
	case errors.As(err, &evErr):
		status := http.StatusBadRequest
		if errors.Is(err, ErrEvidenceNotVerified) {
			status = http.StatusConflict // still uploading: retry later
		}
		c.JSON(status, gin.H{"error": err.Error(), "evidence_ids": evErr.IDs})
	case errors.As(err, &staleErr):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "stored": staleErr.Stored})
	case errors.As(err, &incErr):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "item_keys": incErr.ItemKeys})
	case errors.Is(err, ErrNotInspector), errors.Is(err, ErrCantAbandon), errors.Is(err, ErrAbandonNeedsLead):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInspectionCompleted), errors.Is(err, ErrInspectionAbandoned):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrItemNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrSeverityRequired), errors.Is(err, ErrSeverityNotAllowed), errors.Is(err, ErrBadSeverity),
		errors.Is(err, ErrCommentTooLong), errors.Is(err, ErrBadEvidenceIDs), errors.Is(err, ErrRepeatedEvidence),
		errors.Is(err, ErrTooManyPhotos), errors.Is(err, ErrBadReason), errors.Is(err, ErrAnswerInFuture):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case errors.Is(err, ErrInspectionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInspectionIDReused), errors.Is(err, ErrSiteArchived):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrBadInspectionID), errors.Is(err, ErrBadDeviceID), errors.Is(err, ErrBadCorrectsID),
		errors.Is(err, ErrUnknownTemplateVersion), errors.Is(err, ErrTemplateMismatch),
		errors.Is(err, ErrBadStatus), errors.Is(err, ErrBadLimit), errors.Is(err, ErrBadCoverage),
		errors.Is(err, ErrAlreadyCorrected), errors.Is(err, ErrCorrectionChecklist):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func (h *Handler) Catalog(c *gin.Context) {
	cat, err := h.svc.Catalog(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, cat)
}

func (h *Handler) Start(c *gin.Context) {
	var req StartInspectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	in, created, err := h.svc.Start(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("inspectionId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, in)
}

func (h *Handler) Get(c *gin.Context) {
	in, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("inspectionId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, in)
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
	c.JSON(http.StatusOK, gin.H{"inspections": items})
}

func (h *Handler) Answer(c *gin.Context) {
	var req AnswerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.svc.Answer(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("inspectionId"), c.Param("itemKey"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) Complete(c *gin.Context) {
	var req CompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in, created, err := h.svc.Complete(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("inspectionId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, in)
}

func (h *Handler) Findings(c *gin.Context) {
	limit := 50
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			writeError(c, ErrBadLimit)
			return
		}
		limit = n
	}
	out, err := h.svc.Findings(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Query("severity"), c.Query("coverage"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"findings": out})
}

func (h *Handler) Abandon(c *gin.Context) {
	var req AbandonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, ErrBadReason)
		return
	}
	in, created, err := h.svc.Abandon(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("inspectionId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, in)
}
