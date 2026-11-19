// Package vault mounts the organisation's statutory vault (M11 D19, D20):
// Letter of Good Standing, COIDA, CIDB, tax and the other organisation
// document types of a regime, versioned in internal/core/documents.
package vault

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/documents"
)

func Register(r *gin.Engine, svc *documents.Service, authSvc *auth.Service) {
	g := r.Group("/v1/vault/documents",
		auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer),
		// BILL gates the vault per plan here: one middleware on this group.
	)
	documents.NewHandler(svc, documents.ScopeOrg).Mount(g)
}
