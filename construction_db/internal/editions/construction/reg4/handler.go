package reg4

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/httpapi"
)

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
		factsErr *FactsError
		nextErr  *NotNextError
	)
	switch {
	case errors.As(err, &factsErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "facts": factsErr.Facts})
	case errors.As(err, &nextErr):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "latest_version": nextErr.LatestVersion})
	case errors.Is(err, ErrWeakeningNeedsLead):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrNotFound, ErrVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrIDReused, ErrVersionDiffers, ErrSiteArchived):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case isSentinel(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func status(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

type handler struct{ svc *Service }

func (h handler) submit(c *gin.Context) {
	var req VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	w, created, err := h.svc.Submit(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c),
		c.Param("id"), c.Param("workId"), c.Param("version"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), w)
}

func (h handler) get(c *gin.Context) {
	w, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Param("workId"), c.Query("version"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, w)
}

func (h handler) list(c *gin.Context) {
	n := 50
	if s := c.Query("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			writeError(c, ErrBadLimit)
			return
		}
		n = v
	}
	out, err := h.svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"), c.Query("status"), n)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"site_work": out})
}

func (h handler) facts(c *gin.Context) {
	out, err := h.svc.Facts(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
