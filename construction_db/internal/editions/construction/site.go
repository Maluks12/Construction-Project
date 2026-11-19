package construction

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/httpapi"
)

// onSite answers 404 unless the :id site's regime is a construction one:
// mounting under /v1/construction doesn't guarantee it by itself.
func onSite(deps edition.Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ds, err := deps.Sites.Regime(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
		switch {
		case errors.Is(err, sites.ErrSiteNotFound) || (err == nil && ds.Regime.Edition != (Edition{}).Name()):
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "site not found"})
		case err != nil:
			httpapi.WriteUnexpected(c, "request failed", err)
			c.Abort()
		default:
			c.Next()
		}
	}
}
