package members

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	// Owners and SHE officers (manage_members); which members each may change
	// is the service's and the database's rule (D1).
	g := r.Group("/v1/members", auth.RequireAuth(authSvc), auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer))
	g.GET("", h.List)
	g.GET("/:userId", h.Get)
	g.PATCH("/:userId", h.Update)
	g.GET("/:userId/history", h.History)
}
