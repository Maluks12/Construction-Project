package app

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/invites"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/health"
	"construction_db/internal/regimes"
)

// Defaults are the edition and regime given to new organisations and sites.
type Defaults struct {
	Edition string
	Regime  string
}

func NewRouter(ctx context.Context, pool *pgxpool.Pool, jwtSecret string, reg *regimes.Registry, d Defaults) (*gin.Engine, error) {
	if err := reg.CheckDefault(d.Regime, d.Edition); err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}
	if err := regimes.CheckDefaultRow(ctx, pool, d.Regime); err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", health.Handler(pool))

	authSvc := auth.NewService(pool, jwtSecret, d.Edition, d.Regime)
	auth.Register(router, authSvc)

	sitesSvc := sites.NewService(pool, reg, d.Regime)
	sites.Register(router, sitesSvc, authSvc)

	invitesSvc := invites.NewService(pool, authSvc)
	invites.Register(router, invitesSvc, authSvc)

	return router, nil
}
