package sites

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	writers := auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer)

	protected := r.Group("/v1/sites")
	protected.Use(auth.RequireAuth(authSvc))

	protected.GET("", h.List)
	protected.GET("/:id", h.Get)
	protected.POST("", writers, h.Create)
	protected.PATCH("/:id", writers, h.Update)
	protected.DELETE("/:id", writers, h.Archive)

	protected.GET("/:id/members", writers, h.ListMembers)
	protected.PUT("/:id/members/:userId", writers, h.AddMember)
	protected.DELETE("/:id/members/:userId", writers, h.RemoveMember)

	protected.GET("/:id/locations", h.ListLocations)
	protected.POST("/:id/locations", writers, h.CreateLocation)
	protected.PATCH("/:id/locations/:locationId", writers, h.RenameLocation)
	protected.DELETE("/:id/locations/:locationId", writers, h.ArchiveLocation)
}
