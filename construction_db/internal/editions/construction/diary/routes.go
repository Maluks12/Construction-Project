package diary

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
)

// Register mounts the diary under the edition's group (/v1/construction),
// which is already authenticated.
func Register(r *gin.RouterGroup, svc *Service) {
	h := NewHandler(svc)

	// The roles that write the diary (D5). Supervisors are further limited
	// to their own sites by the service.
	writers := auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor)

	site := r.Group("/sites/:id", writers)
	site.GET("/diary", h.List)
	site.GET("/diary-firms", h.Firms)
	site.GET("/diary/:date", h.Get)
	site.GET("/diary/:date/revisions", h.Revisions)
	// PUT: the device chooses every id, so sending a request again is safe.
	site.PUT("/diary/:date/revisions/:revisionId", h.PutRevision)
	site.PUT("/diary/:date/addenda/:addendumId", h.PutAddendum)
}
