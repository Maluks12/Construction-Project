package app

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/invites"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/health"
	"construction_db/internal/platform/httpapi"
	"construction_db/internal/platform/storage"
	"construction_db/internal/regimes"
)

// ed supplies the edition and regime for new organisations and sites, and its own routes.
// store holds evidence objects. requestTimeout is every request's deadline.
func NewRouter(ctx context.Context, pool *pgxpool.Pool, jwtSecret string, reg *regimes.Registry, ed edition.Edition, store storage.Store, requestTimeout time.Duration) (*gin.Engine, error) {
	if err := reg.CheckDefault(ed.DefaultRegime(), ed.Name()); err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}
	if err := regimes.CheckDefaultRow(ctx, pool, ed.DefaultRegime()); err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}

	router := gin.New()
	// Recovery outermost, so a panic is still a 500; then one deadline for
	// everything after it, auth included.
	router.Use(gin.Recovery(), httpapi.Deadline(requestTimeout))

	router.GET("/healthz", health.Handler(pool))

	authSvc := auth.NewService(pool, jwtSecret, ed.Name(), ed.DefaultRegime())
	auth.Register(router, authSvc)

	sitesSvc := sites.NewService(pool, reg, ed.DefaultRegime())
	sites.Register(router, sitesSvc, authSvc)

	invitesSvc := invites.NewService(pool, authSvc)
	invites.Register(router, invitesSvc, authSvc)

	evidenceSvc := evidence.NewService(pool, sitesSvc, store)
	evidence.Register(router, evidenceSvc, authSvc)

	editionRoutes := router.Group("/v1/" + ed.Name())
	editionRoutes.Use(auth.RequireAuth(authSvc))
	ed.RegisterRoutes(editionRoutes, edition.Deps{Pool: pool, Sites: sitesSvc})

	return router, nil
}
