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
}
