package attendance

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	// Supervisors only on their own sites (the service checks the site).
	g := r.Group("/v1/sites/:id",
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor))

	g.GET("/sign-ins", h.Day)
	// PUT: the device chooses the id, so sending it again is safe.
	g.PUT("/sign-ins/:signInId", h.SignIn)
	g.POST("/sign-ins/:signInId/sign-out", h.SignOut)
	g.POST("/sign-ins/:signInId/retract", h.Retract)
	g.PUT("/sign-in-sheets/:date/:evidenceId", h.AddSheet)
	g.GET("/roster", h.Roster)
	g.PUT("/roster/:workerId", h.AddToRoster)
	g.POST("/roster/:workerId/remove", h.RemoveFromRoster)
}
