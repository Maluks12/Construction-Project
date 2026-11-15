package checklists

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	// The roles that do inspections (Rev 4 §2.12). Supervisors are further
	// limited to their own sites by the service.
	inspectors := []gin.HandlerFunc{
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor),
	}

	catalog := r.Group("/v1/sites/:id/checklists", inspectors...)
	catalog.GET("", h.Catalog)

	inspections := r.Group("/v1/sites/:id/inspections", inspectors...)
	inspections.GET("", h.List)
	inspections.GET("/:inspectionId", h.Get)
	// PUT: the device chooses the id, so repeating a start is safe.
	inspections.PUT("/:inspectionId", h.Start)
	// PUT: the whole answer, so sending it again is safe.
	inspections.PUT("/:inspectionId/items/:itemKey", h.Answer)
	// POST, but safe to repeat: completing a completed inspection is 200.
	inspections.POST("/:inspectionId/complete", h.Complete)
	// POST, but safe to repeat: abandoning an abandoned inspection is 200.
	inspections.POST("/:inspectionId/abandon", h.Abandon)

	findings := r.Group("/v1/sites/:id/findings", inspectors...)
	findings.GET("", h.Findings)
}
