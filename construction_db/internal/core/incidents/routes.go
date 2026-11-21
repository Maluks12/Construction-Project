package incidents

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	// The roles that report incidents (Rev 4 §2.12). Supervisors are further
	// limited to their own sites by the service.
	reporters := []gin.HandlerFunc{
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor),
	}

	incidents := r.Group("/v1/sites/:id/incidents", reporters...)
	incidents.GET("", h.List)
	incidents.GET("/:incidentId", h.Get)
	// PUT: the device chooses the incident id and the version number, so
	// sending a version again is safe.
	incidents.PUT("/:incidentId/versions/:version", h.Submit)
	// PUT: the device chooses the statement id.
	incidents.PUT("/:incidentId/witness-statements/:statementId", h.RecordStatement)
	// The deadline endpoints are internal/core/deadlines' (REG4).
}
