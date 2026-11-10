package checklists

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	// The roles that do inspections (Rev 4 §2.12). Supervisors are further
	// limited to their own sites by the service.
	protected := r.Group("/v1/sites/:id/checklists")
	protected.Use(auth.RequireAuth(authSvc))
	protected.Use(auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor))

	protected.GET("", h.Catalog)
}
