package toolbox

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
	switch {
	case errors.Is(err, ErrAlreadySigned):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "hint": AlreadySignedHint})
	case errors.Is(err, ErrCantRetract):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrTalkNotFound, ErrAttendanceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	// The request is fine; the stored state refuses it (or isn't ready yet:
	// a photo still uploading, a register photo not added).
	case is(err, ErrTalkIDReused, ErrTalkSigned, ErrStartedAfterClose, ErrEvidenceNotVerified, ErrPhotoKindDiffers,
		ErrAttendanceIDReused, ErrNeedsRegisterPhoto, ErrAlreadyRetracted, ErrEndsBeforeSigned):
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

func (h *Handler) Topics(c *gin.Context) {
	topics, err := h.svc.Topics(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"topics": topics})
}

func (h *Handler) PutTalk(c *gin.Context) {
	var req TalkRequest
	if !bind(c, &req) {
		return
	}
	t, created, err := h.svc.PutTalk(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("talkId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), t)
}

func (h *Handler) Get(c *gin.Context) {
	t, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("talkId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
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
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Query("date"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"talks": out})
}

func (h *Handler) AddPhoto(c *gin.Context) {
	var req PhotoRequest
	if !bind(c, &req) {
		return
	}
	t, created, err := h.svc.AddPhoto(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("talkId"), c.Param("evidenceId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), t)
}

func (h *Handler) Sign(c *gin.Context) {
	var req SignRequest
	if !bind(c, &req) {
		return
	}
	a, created, err := h.svc.Sign(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("talkId"), c.Param("attendanceId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), a)
}

func (h *Handler) Retract(c *gin.Context) {
	var req RetractRequest
	if !bind(c, &req) {
		return
	}
	a, created, err := h.svc.Retract(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("talkId"), c.Param("attendanceId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), a)
}
