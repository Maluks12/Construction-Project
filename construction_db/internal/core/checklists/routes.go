package checklists

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	// The roles that do inspections (Rev 4 §2.12). Supervisors are further
	// limited to their own sites by the service.
	inspectors := []gin.HandlerFunc{
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor),
	}

	catalog := r.Group("/v1/sites/:id/checklists", inspectors...)
	catalog.GET("", h.Catalog)

	inspections := r.Group("/v1/sites/:id/inspections", inspectors...)
	inspections.GET("/:inspectionId", h.Get)
	// PUT: the device chooses the id, so repeating a start is safe.
	inspections.PUT("/:inspectionId", h.Start)
}
