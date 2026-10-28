package edition

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/sites"
)

// Edition is all core and app know about an edition; editions implement it,
// core never imports them (Rev 4 §1.2).
type Edition interface {
	Name() string                                 // organisations.primary_edition, e.g. "construction"
	DefaultRegime() string                        // regulatory_regimes.code for new sites
	RegisterRoutes(r *gin.RouterGroup, deps Deps) // edition-only endpoints; r is authenticated
}

// Deps is what edition handlers may use from core.
type Deps struct {
	Pool  *pgxpool.Pool
	Sites *sites.Service
}
