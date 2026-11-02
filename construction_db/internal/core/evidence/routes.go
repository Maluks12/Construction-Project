package evidence

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	// Supervisors are further limited to their own sites by the service.
	protected := r.Group("/v1/sites/:id/evidence")
	protected.Use(auth.RequireAuth(authSvc))
	protected.Use(auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor))

	protected.GET("", h.List)
	protected.GET("/:evidenceId", h.Get)
	protected.PUT("/:evidenceId", h.Declare)
}
