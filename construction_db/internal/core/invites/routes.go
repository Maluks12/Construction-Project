package invites

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	writers := auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer)

	public := r.Group("/v1/invites")
	public.POST("/preview", h.Preview)
	public.POST("/accept", h.Accept)

	protected := r.Group("/v1/invites")
	protected.Use(auth.RequireAuth(authSvc))
	protected.POST("", writers, h.Invite)
	protected.GET("", writers, h.ListPending)
	protected.DELETE("/:id", writers, h.Revoke)
}
