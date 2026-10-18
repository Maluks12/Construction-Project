package sites

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
	case errors.Is(err, ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
	case errors.Is(err, ErrLocationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "location not found"})
	case errors.Is(err, ErrParentNotFound):
		c.JSON(http.StatusBadRequest, gin.H{"error": "parent location not found"})
	case errors.Is(err, ErrBlankName):
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
	case errors.Is(err, ErrLocationNameTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "a location with that name already exists here"})
	case errors.Is(err, ErrLocationHasChildren):
		c.JSON(http.StatusConflict, gin.H{"error": "location has active child locations"})
	default:
		slog.Error("request failed", "path", c.Request.URL.Path, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func (h *Handler) List(c *gin.Context) {
	orgID := auth.OrgID(c)
	userID := auth.UserID(c)
	role := auth.Role(c)

	sites, err := h.svc.List(c.Request.Context(), orgID, userID, role)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sites": sites})
}

func (h *Handler) Get(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")
	userID := auth.UserID(c)
	role := auth.Role(c)

	site, err := h.svc.Get(c.Request.Context(), orgID, userID, role, id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, site)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateSiteRequest
	orgID := auth.OrgID(c)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	site, err := h.svc.Create(c.Request.Context(), orgID, req)

	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, site)
}

func (h *Handler) Update(c *gin.Context) {
	var req UpdateSiteRequest
	orgID := auth.OrgID(c)
	id := c.Param("id")
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	site, err := h.svc.Update(c.Request.Context(), orgID, id, req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, site)
}

func (h *Handler) Archive(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")

	err := h.svc.Archive(c.Request.Context(), orgID, id)

	if err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) ListMembers(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")
	members, err := h.svc.ListMembers(c.Request.Context(), orgID, id)

	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"members": members})
}

func (h *Handler) AddMember(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")
	userID := c.Param("userId")

	err := h.svc.AddMember(c.Request.Context(), orgID, id, userID)

	if err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) RemoveMember(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")
	userID := c.Param("userId")

	err := h.svc.RemoveMember(c.Request.Context(), orgID, id, userID)

	if err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) ListLocations(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")
	userID := auth.UserID(c)
	role := auth.Role(c)

	locations, err := h.svc.ListLocations(c.Request.Context(), orgID, userID, role, id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"locations": locations})

}

func (h *Handler) CreateLocation(c *gin.Context) {
	var req CreateLocationRequest
	orgID := auth.OrgID(c)
	id := c.Param("id")

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	location, err := h.svc.CreateLocation(c.Request.Context(), orgID, id, req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, location)
}

func (h *Handler) RenameLocation(c *gin.Context) {
	var req UpdateLocationRequest
	orgID := auth.OrgID(c)
	id := c.Param("id")
	locationID := c.Param("locationId")

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	location, err := h.svc.RenameLocation(c.Request.Context(), orgID, id, locationID, req)

	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, location)
}

func (h *Handler) ArchiveLocation(c *gin.Context) {
	orgID := auth.OrgID(c)
	id := c.Param("id")
	locationID := c.Param("locationId")

	err := h.svc.ArchiveLocation(c.Request.Context(), orgID, id, locationID)

	if err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
