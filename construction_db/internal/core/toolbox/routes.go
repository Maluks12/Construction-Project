package toolbox

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

	r.GET("/v1/sites/:id/toolbox-topics", append(safety, h.Topics)...)

	talks := r.Group("/v1/sites/:id/toolbox-talks", safety...)
	talks.GET("", h.List)
	talks.GET("/:talkId", h.Get)
	// PUT: the device chooses every id, so sending a request again is safe.
	talks.PUT("/:talkId", h.PutTalk)
	talks.PUT("/:talkId/photos/:evidenceId", h.AddPhoto)
	talks.PUT("/:talkId/attendance/:attendanceId", h.Sign)
	// POST, but safe to repeat with the same reason (200).
	talks.POST("/:talkId/attendance/:attendanceId/retract", h.Retract)
}
