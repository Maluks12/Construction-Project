package exports

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

// Register mounts the audit pack exports: owners and SHE officers only
// (M12 D9, Rev 4 §2.12); every other role 403.
func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	g := r.Group("/v1/sites/:id/exports", auth.RequireAuth(authSvc), auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer))
	g.GET("", h.List)
	// PUT: the device chooses the id, so sending it again is safe.
	g.PUT("/:exportId", h.Queue)
	g.GET("/:exportId", h.Get)
	// POST: each download is logged, as each view is.
	g.POST("/:exportId/download", h.Download)
}
