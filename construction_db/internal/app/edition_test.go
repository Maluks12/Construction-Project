package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/sites"
)

type fakeEdition struct {
	name, regime string
	routes       func(*gin.RouterGroup, edition.Deps)
}

func (e fakeEdition) Name() string          { return e.name }
func (e fakeEdition) DefaultRegime() string { return e.regime }
func (e fakeEdition) RegisterRoutes(r *gin.RouterGroup, deps edition.Deps) {
	if e.routes != nil {
		e.routes(r, deps)
	}
}

// The fake handler follows the CLAUDE.md rule: resolve the site through
// deps.Sites.Regime and 404 unless its regime belongs to this edition.
func TestEditionRoutes(t *testing.T) {
	ed := fakeEdition{
		name:   "construction",
		regime: "za_construction_2014",
		routes: func(r *gin.RouterGroup, deps edition.Deps) {
			r.GET("/sites/:id/regime", func(c *gin.Context) {
				ds, err := deps.Sites.Regime(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("id"))
				if errors.Is(err, sites.ErrSiteNotFound) || (err == nil && ds.Regime.Edition != "construction") {
					c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
					return
				}
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"regime": ds.Regime.Code})
			})
		},
	}
	router, err := NewRouter(context.Background(), testPool, testSecret, testRegistry, ed, testStore)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	token := signup(t, router, "Edition Co", "owner@edition.test")
	otherToken := signup(t, router, "Other Edition Co", "owner@other-edition.test")
	site := listSites(t, router, token)[0].ID
	otherSite := listSites(t, router, otherToken)[0].ID

	tests := map[string]struct {
		token, site string
		want        int
	}{
		"no token":         {"", site, http.StatusUnauthorized},
		"own site":         {token, site, http.StatusOK},
		"other org's site": {token, otherSite, http.StatusNotFound},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rec := get(t, router, "/v1/construction/sites/"+tc.site+"/regime", tc.token)
			if rec.Code != tc.want {
				t.Errorf("got %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
