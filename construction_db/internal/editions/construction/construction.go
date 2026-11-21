package construction

import (
	"github.com/gin-gonic/gin"

	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/edition"
	"construction_db/internal/editions/construction/appointments"
	"construction_db/internal/editions/construction/diary"
	"construction_db/internal/editions/construction/reg4"
	"construction_db/internal/editions/construction/safetyfile"
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
// the site diary, the H&S file and appointments (Rev 4 §5.2).
func (Edition) RegisterRoutes(r *gin.RouterGroup, deps edition.Deps) {
	diarySvc := diary.NewService(deps.Pool, deps.Sites)
	diarySvc.Clock = deps.Clock
	diary.Register(r, diarySvc)
	safetyfile.Register(r, deps.Documents, onSite(deps))
	appointments.Register(r, appointments.NewService(deps), onSite(deps))
	reg4.Register(r, reg4.NewService(deps), onSite(deps)) // site work (REG4)
}

// DeadlineSubjects: site work starts deadlines of its own (REG4 D7).
func (Edition) DeadlineSubjects(edition.Deps) map[deadlines.SubjectKind]deadlines.SubjectHooks {
	return map[deadlines.SubjectKind]deadlines.SubjectHooks{deadlines.SubjectSiteWork: reg4.Hooks{}}
}
