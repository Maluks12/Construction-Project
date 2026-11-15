package actions

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)

	// The roles that hold safety records (Rev 4 §2.12). Supervisors are
	// further limited to their own sites by the service.
	safety := []gin.HandlerFunc{
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor),
	}

	actions := r.Group("/v1/sites/:id/actions", safety...)
	actions.GET("", h.List)
	actions.GET("/:actionId", h.Get)
	// PUT: the device chooses every id, so sending a request again is safe.
	actions.PUT("/:actionId", h.Raise)
	actions.PUT("/:actionId/events/:eventId", h.Event)
	actions.PUT("/:actionId/closeouts/:closeoutId", h.CloseOut)

	// The register across every site the caller can open.
	r.GET("/v1/actions", append(safety, h.ListAll)...)

	waivers := r.Group("/v1/sites/:id/waivers", safety...)
	// PUT: the device chooses the waiver id.
	waivers.PUT("/:waiverId", h.Waive)
	// POST, but safe to repeat with the same reason (200).
	waivers.POST("/:waiverId/retract", h.RetractWaiver)
}
