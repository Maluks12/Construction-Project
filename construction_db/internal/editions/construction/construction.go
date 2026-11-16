package construction

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/edition"
	"construction_db/internal/editions/construction/diary"
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

// RegisterRoutes mounts the edition's endpoints under /v1/construction:
// the site diary (Rev 4 §5.2).
func (Edition) RegisterRoutes(r *gin.RouterGroup, deps edition.Deps) {
	diary.Register(r, diary.NewService(deps.Pool, deps.Sites))
}
