package app

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/actions"
	"construction_db/internal/core/attendance"
	"construction_db/internal/core/auth"
	"construction_db/internal/core/checklists"
	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/devicesync"
	"construction_db/internal/core/documents"
	"construction_db/internal/core/edition"
	"construction_db/internal/core/evidence"
	"construction_db/internal/core/exports"
	"construction_db/internal/core/files"
	"construction_db/internal/core/incidents"
	"construction_db/internal/core/invites"
	"construction_db/internal/core/members"
	"construction_db/internal/core/notifications"
	"construction_db/internal/core/onboarding"
	"construction_db/internal/core/reports"
	"construction_db/internal/core/sites"
	"construction_db/internal/core/toolbox"
	"construction_db/internal/core/vault"
	"construction_db/internal/core/workers"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/health"
	"construction_db/internal/platform/httpapi"
	"construction_db/internal/platform/storage"
	"construction_db/internal/regimes"
)

// HTTPOptions are the server-level HTTP settings.
type HTTPOptions struct {
	RequestTimeout time.Duration // every request's deadline
	// Proxies whose X-Forwarded-For sets the client IP; nil trusts none.
	TrustedProxies []string
	// Clock is "now" for the services that date things; nil is the real
	// time. Tests set it to run as if at another hour.
	Clock clock.Clock
	// Exports is the bucket audit packs are downloaded from (M12); nil
	// leaves the exports routes out.
	Exports storage.Store
}

// ed supplies the edition and regime for new organisations and sites, and its own routes.
// store holds evidence objects.
func NewRouter(ctx context.Context, pool *pgxpool.Pool, jwtSecret string, reg *regimes.Registry, ed edition.Edition, store storage.Store, opts HTTPOptions) (*gin.Engine, error) {
	if err := reg.CheckDefault(ed.DefaultRegime(), ed.Name()); err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}
	if err := regimes.CheckDefaultRow(ctx, pool, ed.DefaultRegime()); err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}

	router := gin.New()
	// Gin trusts every proxy by default, which would let any client choose the
	// IP the evidence access log records. nil trusts none.
	if err := router.SetTrustedProxies(opts.TrustedProxies); err != nil {
		return nil, fmt.Errorf("new router: trusted proxies: %w", err)
	}
	// Recovery outermost, so a panic is still a 500; then one deadline for
	// everything after it, auth included.
	// No NUL reaches a handler (REG4 review R4); before auth, so it holds for every route.
	// A sync sub-request reaches only the route it was checked against (SYNC D6).
	// The push's own body is checked record by record (SYNC review 2).
	router.Use(gin.Recovery(), httpapi.Deadline(opts.RequestTimeout), httpapi.RejectNUL(devicesync.PushRoute), devicesync.GuardSubRequests())

	router.GET("/healthz", health.Handler(pool))
	router.GET("/healthz/jobs", notifications.JobsHealth(pool, opts.Clock))

	authSvc := auth.NewService(pool, jwtSecret, ed.Name(), ed.DefaultRegime())
	auth.Register(router, authSvc)

	sitesSvc := sites.NewService(pool, reg, ed.DefaultRegime())
	sites.Register(router, sitesSvc, authSvc)

	invitesSvc := invites.NewService(pool, authSvc)
	invites.Register(router, invitesSvc, authSvc)
	membersSvc := members.NewService(pool)
	membersSvc.Clock = opts.Clock
	members.Register(router, membersSvc, authSvc)

	evidenceSvc := evidence.NewService(pool, sitesSvc, store)
	evidence.Register(router, evidenceSvc, authSvc)

	filesSvc := files.NewService(pool, sitesSvc, store)
	files.Register(router, filesSvc, authSvc)

	checklistsSvc := checklists.NewService(pool, sitesSvc)
	checklistsSvc.Clock = opts.Clock
	checklists.Register(router, checklistsSvc, authSvc)
	incidentsSvc := incidents.NewService(pool, sitesSvc)
	incidentsSvc.Clock = opts.Clock
	incidents.Register(router, incidentsSvc, authSvc)
	deadlinesSvc := deadlines.NewService(pool, sitesSvc)
	deadlinesSvc.Clock = opts.Clock
	deadlinesSvc.Register(deadlines.SubjectIncident, incidentsSvc.DeadlineHooks())
	actionsSvc := actions.NewService(pool, sitesSvc)
	actionsSvc.Clock = opts.Clock
	actions.Register(router, actionsSvc, authSvc)
	toolboxSvc := toolbox.NewService(pool, sitesSvc)
	toolboxSvc.Clock = opts.Clock
	toolbox.Register(router, toolboxSvc, authSvc)
	workersSvc := workers.NewService(pool, sitesSvc, filesSvc, reg)
	workersSvc.Clock = opts.Clock
	workers.Register(router, workersSvc, authSvc)
	attendanceSvc := attendance.NewService(pool, sitesSvc, reg)
	attendanceSvc.Clock = opts.Clock
	attendance.Register(router, attendanceSvc, authSvc)
	documentsSvc := documents.NewService(pool, sitesSvc, filesSvc, reg)
	documentsSvc.Clock = opts.Clock
	vault.Register(router, documentsSvc, authSvc)

	// What edition code reaches core through, built once for every edition hook.
	deps := edition.Deps{Pool: pool, Sites: sitesSvc, Files: filesSvc, Documents: documentsSvc, Regimes: reg, Clock: opts.Clock}
	var onboard onboarding.Contributor
	if oe, ok := ed.(onboardingEdition); ok {
		onboard = oe.OnboardingContributor(deps)
	}
	onboardingSvc := onboarding.NewService(pool, workersSvc, documentsSvc, reg, onboard)
	onboardingSvc.Clock = opts.Clock
	onboarding.Register(router, onboardingSvc, authSvc)
	notifications.Register(router, notifications.NewService(pool), authSvc)

	if se, ok := ed.(deadlineSubjectEdition); ok {
		for kind, h := range se.DeadlineSubjects(deps) {
			deadlinesSvc.Register(kind, h)
		}
	}
	deadlines.Register(router, deadlinesSvc, authSvc)
	var contrib reports.Contributor
	if re, ok := ed.(reportingEdition); ok {
		contrib = re.ReportsContributor(deps)
	}
	reportsSvc := reports.NewService(pool, sitesSvc, documentsSvc, reg, contrib, ed.DefaultRegime())
	reportsSvc.Clock = opts.Clock
	reports.Register(router, reportsSvc, authSvc)
	if opts.Exports != nil {
		exportsSvc := exports.NewService(pool, sitesSvc, opts.Exports)
		exportsSvc.Clock = opts.Clock
		exports.Register(router, exportsSvc, authSvc)
	}

	editionRoutes := router.Group("/v1/" + ed.Name())
	editionRoutes.Use(auth.RequireAuth(authSvc))
	ed.RegisterRoutes(editionRoutes, deps)

	syncSvc := devicesync.NewService(pool, sitesSvc, reg)
	syncSvc.Clock = opts.Clock
	if se, ok := ed.(syncEdition); ok {
		syncSvc.Add(se.SyncSources())
	}
	devicesync.Register(router, syncSvc, authSvc)
	syncSvc.Router = router

	return router, nil
}

// syncEdition is an edition with records of its own to pull and push (SYNC).
type syncEdition interface {
	SyncSources() devicesync.Sources
}

// reportingEdition is an edition that adds categories to the compliance
// summary (M12); the edition.Edition interface stays as it was.
type reportingEdition interface {
	ReportsContributor(edition.Deps) reports.Contributor
}

// onboardingEdition is an edition with first-run steps of its own (ONBOARD
// D12).
type onboardingEdition interface {
	OnboardingContributor(edition.Deps) onboarding.Contributor
}

// deadlineSubjectEdition is an edition with deadline subjects of its own
// (REG4: site work).
type deadlineSubjectEdition interface {
	DeadlineSubjects(edition.Deps) map[deadlines.SubjectKind]deadlines.SubjectHooks
}
