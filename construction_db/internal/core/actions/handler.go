package actions

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
		evErr     *EvidenceError
		latestErr *NotLatestError
	)
	switch {
	// These carry data the app acts on, next to the usual error text.
	case errors.As(err, &evErr):
		status := http.StatusBadRequest
		if errors.Is(err, ErrEvidenceNotVerified) {
			status = http.StatusConflict // still uploading: retry later
		}
		c.JSON(status, gin.H{"error": err.Error(), "evidence_ids": evErr.IDs})
	case errors.As(err, &latestErr):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "latest_version": latestErr.LatestVersion})

	case is(err, ErrLaterNeedsLead, ErrCantReassign, ErrCantMoveEarlier, ErrNeedsLead, ErrCantCloseOut, ErrCantWaive):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrActionNotFound, ErrWaiverNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrSiteArchived, ErrActionIDReused, ErrFindingSuperseded, ErrNotLatestVersion, ErrEventIDReused,
		ErrActionClosed, ErrActionCancelled, ErrNotAwaiting, ErrNotPending, ErrNotClosed, ErrCloseoutIDReused,
		ErrAlreadyAwaiting, ErrWaiverIDReused, ErrAlreadyWaived, ErrWaiverRetractionDiffers):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case isSentinel(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

// bind decodes a JSON body; a malformed one is 400 with the decoder's text,
// as elsewhere.
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

func (h *Handler) Raise(c *gin.Context) {
	var req RaiseRequest
	if !bind(c, &req) {
		return
	}
	a, created, err := h.svc.Raise(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("actionId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), a)
}

func (h *Handler) Get(c *gin.Context) {
	a, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("actionId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, a)
}

func (h *Handler) List(c *gin.Context) {
	h.list(c, c.Param("id"))
}

// ListAll is the register across every site the caller can open.
func (h *Handler) ListAll(c *gin.Context) {
	h.list(c, "")
}

func (h *Handler) list(c *gin.Context, siteID string) {
	n, ok := limit(c)
	if !ok {
		return
	}
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), siteID,
		c.Query("state"), c.Query("owner"), c.Query("source"), n)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"actions": out})
}

func (h *Handler) Event(c *gin.Context) {
	var req EventRequest
	if !bind(c, &req) {
		return
	}
	a, created, err := h.svc.Event(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("actionId"), c.Param("eventId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), a)
}

func (h *Handler) CloseOut(c *gin.Context) {
	var req CloseoutRequest
	if !bind(c, &req) {
		return
	}
	a, created, err := h.svc.CloseOut(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("actionId"), c.Param("closeoutId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), a)
}

func (h *Handler) Waive(c *gin.Context) {
	var req WaiverRequest
	if !bind(c, &req) {
		return
	}
	w, created, err := h.svc.Waive(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("waiverId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), w)
}

func (h *Handler) RetractWaiver(c *gin.Context) {
	var req RetractWaiverRequest
	if !bind(c, &req) {
		return
	}
	w, created, err := h.svc.RetractWaiver(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("waiverId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), w)
}
