package files

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	g := r.Group("/v1/files",
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor))
	// PUT: the device chooses the id, so sending it again is safe.
	g.PUT("/:fileId", h.Declare)
	// ONBOARD D11: many declarations at once (bulk documents).
	g.POST("/batch", h.DeclareBatch)
	g.POST("/:fileId/complete", h.Complete)
	g.GET("/:fileId", h.Get)
}
