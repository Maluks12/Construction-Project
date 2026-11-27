package workers

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	h := NewHandler(svc)
	// The register is the organisation's, shared across its sites:
	// supervisors read it and add people on the spot; edits, retractions and
	// links are the leads' (checked by the service).
	people := []gin.HandlerFunc{
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor),
	}

	r.GET("/v1/regimes", append(people, h.Regimes)...)
	r.GET("/v1/regimes/:code/types", append(people, h.Types)...)

	w := r.Group("/v1/workers", people...)
	w.GET("", h.List)
	w.GET("/:workerId", h.Get)
	// PUT: the device chooses every id, so sending a request again is safe.
	w.PUT("/:workerId", h.Put)
	w.PUT("/:workerId/competencies/:recordId", h.PutCompetency)
	w.POST("/:workerId/competencies/:recordId/retract", h.retract(kindCompetency))
	w.POST("/:workerId/competencies/:recordId/view", h.view(kindCompetency))
	w.PUT("/:workerId/medicals/:recordId", h.PutMedical)
	w.POST("/:workerId/medicals/:recordId/retract", h.retract(kindMedical))
	w.POST("/:workerId/medicals/:recordId/view", h.view(kindMedical))

	// ONBOARD D9, D10: the register from a CSV, all or nothing; leads only.
	r.PUT("/v1/worker-imports/:importId", auth.RequireAuth(authSvc), auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer), h.Import)

	att := r.Group("/v1/sites/:id/toolbox-talks/:talkId/attendance/:attendanceId", people...)
	att.PUT("/worker", h.Link)
	att.POST("/worker/unlink", h.Unlink)
}
