package construction

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/edition"
)

type Edition struct{}

var _ edition.Edition = Edition{}

func New() Edition {
	return Edition{}
}

func (Edition) Name() string {
	return "construction"
}

func (Edition) DefaultRegime() string {
	return "za_construction_2014"
}

func (Edition) RegisterRoutes(r *gin.RouterGroup, deps edition.Deps) {}
