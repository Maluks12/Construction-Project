package app

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/invites"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/health"
)

func NewRouter(pool *pgxpool.Pool, jwtSecret string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", health.Handler(pool))

	authSvc := auth.NewService(pool, jwtSecret)
	auth.Register(router, authSvc)

	sitesSvc := sites.NewService(pool)
	sites.Register(router, sitesSvc, authSvc)

	invitesSvc := invites.NewService(pool)
	invites.Register(router, invitesSvc, authSvc)

	return router
}
