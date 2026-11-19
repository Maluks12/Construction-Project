// Package safetyfile mounts a site's health and safety file (CR 2014 reg
// 7(1)(b); M11 D19, D20): the site regime's document types, versioned in
// core's documents service, with each required type present, missing or
// expired.
package safetyfile

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/documents"
)

// Register mounts the H&S file under the edition's group. Owners and SHE
// officers write (the service checks); supervisors read their own sites.
func Register(r *gin.RouterGroup, svc *documents.Service, onSite gin.HandlerFunc) {
	g := r.Group("/sites/:id/safety-file",
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor),
		onSite)
	documents.NewHandler(svc, documents.ScopeSite).Mount(g)
}
