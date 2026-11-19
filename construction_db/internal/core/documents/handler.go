package documents

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/files"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/httpapi"
)

// Handler serves one register: the vault (ScopeOrg) or a site's H&S file
// (ScopeSite, the site from the :id parameter).
type Handler struct {
	svc   *Service
	scope string
}

func NewHandler(svc *Service, scope string) *Handler { return &Handler{svc: svc, scope: scope} }

func (h *Handler) where(c *gin.Context) Where { return Where{Scope: h.scope, SiteID: c.Param("id")} }

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
	case is(err, ErrCantWrite, ErrCantRetract):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case is(err, ErrDocumentNotFound, ErrVersionNotFound, ErrRegimeNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case is(err, ErrIDReused, ErrVersionTaken, ErrNotNextVersion, ErrFileUsed, ErrRetracted, ErrAlreadyRetracted, ErrNotCurrent, ErrOnlyVersion):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case files.WriteError(c, err):
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

func (h *Handler) Register(c *gin.Context) {
	r, err := h.svc.Register(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), h.where(c), c.Query("regime_code"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, r)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, created, err := h.svc.Create(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), h.where(c), c.Param("documentId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), d)
}

func (h *Handler) AddVersion(c *gin.Context) {
	var req VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, created, err := h.svc.AddVersion(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), h.where(c),
		c.Param("documentId"), c.Param("version"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), d)
}

func (h *Handler) Get(c *gin.Context) {
	d, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), h.where(c), c.Param("documentId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) View(c *gin.Context) {
	u, err := h.svc.View(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), h.where(c), c.Param("documentId"), c.Param("version"),
		evidence.Client{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

// Mount puts the register's routes on g.
func (h *Handler) Mount(g gin.IRoutes) {
	g.GET("", h.Register)
	g.GET("/:documentId", h.Get)
	// PUT: the device chooses the document id and the version number, so
	// sending a request again is safe.
	g.PUT("/:documentId", h.Create)
	g.PUT("/:documentId/versions/:version", h.AddVersion)
	g.POST("/:documentId/versions/:version/view", h.View)
	// POST, but safe to repeat with the same reason (200).
	g.POST("/:documentId/retract", h.Retract)
	g.POST("/:documentId/versions/:version/retract", h.RetractVersion)
}

func (h *Handler) Retract(c *gin.Context) {
	var req RetractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, created, err := h.svc.Retract(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), h.where(c), c.Param("documentId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), d)
}

func (h *Handler) RetractVersion(c *gin.Context) {
	var req RetractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, created, err := h.svc.RetractVersion(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), h.where(c),
		c.Param("documentId"), c.Param("version"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), d)
}
