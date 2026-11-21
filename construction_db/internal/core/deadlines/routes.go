package deadlines

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

// Register mounts the site deadline endpoints, for every subject (REG4 D7;
// in internal/core/incidents until then). Supervisors are further limited to
// their own sites by the service.
func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	g := r.Group("/v1/sites/:id/deadlines",
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor))
	g.GET("", h.List)
	g.GET("/:deadlineId", h.Get)
	// PUT: the device chooses the satisfaction id.
	g.PUT("/:deadlineId/satisfactions/:satisfactionId", h.Satisfy)
	// POST, but safe to repeat with the same reason (200).
	g.POST("/:deadlineId/satisfactions/:satisfactionId/retract", h.Retract)
}
