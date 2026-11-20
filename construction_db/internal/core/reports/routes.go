package reports

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

// Register mounts the compliance summary: owners and SHE officers on any
// site, supervisors on their own (the service scopes them), others 403.
func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	g := r.Group("/v1", auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor))
	g.GET("/compliance", h.Org)
	g.GET("/sites/:id/compliance", h.Site)
}
