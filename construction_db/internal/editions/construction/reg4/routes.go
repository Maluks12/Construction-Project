package reg4

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

// Register mounts site work under the edition's group (/v1/construction),
// which is already authenticated. onSite answers 404 for a site under
// another edition's regime. Supervisors are further limited to their own
// sites by the service.
func Register(r *gin.RouterGroup, svc *Service, onSite gin.HandlerFunc) {
	h := handler{svc: svc}
	site := r.Group("/sites/:id", auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor), onSite)
	site.GET("/site-work", h.list)
	site.GET("/site-work-facts", h.facts)
	site.GET("/site-work/:workId", h.get)
	// PUT: the device chooses the id and the version number, so sending a
	// version again is safe.
	site.PUT("/site-work/:workId/versions/:version", h.submit)
}
