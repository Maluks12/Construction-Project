package sites

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	protected := r.Group("/v1/sites")
	protected.Use(auth.RequireAuth(authSvc))
	protected.GET("", h.List)
	protected.GET("/:id", h.Get)
}
