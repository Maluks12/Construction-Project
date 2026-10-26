package invites

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	writers := auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer)

	protected := r.Group("/v1/invites")
	protected.Use(auth.RequireAuth(authSvc))

	protected.POST("", writers, h.Invite)
	protected.GET("", writers, h.ListPending)
	protected.DELETE("/:id", writers, h.Revoke)
}
