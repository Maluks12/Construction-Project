package onboarding

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	// The first-run set-up is the leads' (D11, D12).
	g := r.Group("/v1/onboarding", auth.RequireAuth(authSvc), auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer))
	g.POST("/documents", h.Documents)
	g.GET("/progress", h.Progress)
}
