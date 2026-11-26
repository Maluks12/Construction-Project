package devicesync

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/httpapi"
)

// CorePushRoutes are the core endpoints a batch may carry: the captures a
// phone makes offline. Everything else stays one request at a time
// (TestPushRoutes lists each with its reason).
func CorePushRoutes() []string {
	return []string{
		"PUT /v1/sites/:id/evidence/:evidenceId",
		"PUT /v1/files/:fileId",
		"PUT /v1/sites/:id/inspections/:inspectionId",
		"PUT /v1/sites/:id/inspections/:inspectionId/items/:itemKey",
		"POST /v1/sites/:id/inspections/:inspectionId/complete",
		"POST /v1/sites/:id/inspections/:inspectionId/abandon",
		"PUT /v1/sites/:id/incidents/:incidentId/versions/:version",
		"PUT /v1/sites/:id/incidents/:incidentId/witness-statements/:statementId",
		"PUT /v1/sites/:id/deadlines/:deadlineId/satisfactions/:satisfactionId",
		"POST /v1/sites/:id/deadlines/:deadlineId/satisfactions/:satisfactionId/retract",
		"PUT /v1/sites/:id/actions/:actionId",
		"PUT /v1/sites/:id/actions/:actionId/events/:eventId",
		"PUT /v1/sites/:id/actions/:actionId/closeouts/:closeoutId",
		"PUT /v1/sites/:id/waivers/:waiverId",
		"POST /v1/sites/:id/waivers/:waiverId/retract",
		"PUT /v1/sites/:id/toolbox-talks/:talkId",
		"PUT /v1/sites/:id/toolbox-talks/:talkId/photos/:evidenceId",
		"PUT /v1/sites/:id/toolbox-talks/:talkId/attendance/:attendanceId",
		"POST /v1/sites/:id/toolbox-talks/:talkId/attendance/:attendanceId/retract",
		"PUT /v1/sites/:id/sign-ins/:signInId",
		"POST /v1/sites/:id/sign-ins/:signInId/sign-out",
		"POST /v1/sites/:id/sign-ins/:signInId/retract",
		"PUT /v1/sites/:id/sign-in-sheets/:date/:evidenceId",
		"PUT /v1/sites/:id/roster/:workerId",
		"POST /v1/sites/:id/roster/:workerId/remove",
		"PUT /v1/workers/:workerId",
		"PUT /v1/workers/:workerId/competencies/:recordId",
		"POST /v1/workers/:workerId/competencies/:recordId/retract",
		"PUT /v1/workers/:workerId/medicals/:recordId",
		"POST /v1/workers/:workerId/medicals/:recordId/retract",
		"PUT /v1/vault/documents/:documentId",
		"PUT /v1/vault/documents/:documentId/versions/:version",
		"POST /v1/vault/documents/:documentId/retract",
		"POST /v1/vault/documents/:documentId/versions/:version/retract",
		"POST /v1/notifications/:notificationId/read",
	}
}

// CoreReferenceParts are the regime's data a phone needs to capture
// offline, each its own GET.
func CoreReferenceParts() []RefPart {
	return []RefPart{
		{"checklists", "/v1/sites/:id/checklists", func(site, _ string) string { return "/v1/sites/" + site + "/checklists" }},
		{"toolbox_topics", "/v1/sites/:id/toolbox-topics", func(site, _ string) string { return "/v1/sites/" + site + "/toolbox-topics" }},
		{"document_types", "/v1/regimes/:code/types", func(_, code string) string { return "/v1/regimes/" + code + "/types" }},
	}
}

// PushRoute is the batch's pattern: httpapi.RejectNUL leaves its body to the
// records' own sub-requests.
const PushRoute = "/v1/sync/push"

func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	g := r.Group("/v1/sync", auth.RequireAuth(authSvc),
		auth.RequireRole(auth.RoleOwner, auth.RoleSHEOfficer, auth.RoleSupervisor))
	g.GET("", svc.handlePullOrg)
	g.GET("/sites/:id", svc.handlePullSite)
	g.GET("/sites/:id/reference", svc.handleReference)
	g.POST("/push", svc.handlePush)
}

func caller(c *gin.Context) Caller {
	return Caller{OrgID: auth.OrgID(c), UserID: auth.UserID(c), Role: auth.Role(c), Request: c.Request}
}

func pullLimit(c *gin.Context) (int, error) {
	s := c.Query("limit")
	if s == "" {
		return DefaultPull, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > MaxPullItems {
		return 0, ErrBadLimit
	}
	return n, nil
}

func (s *Service) handlePullOrg(c *gin.Context) {
	limit, err := pullLimit(c)
	if err != nil {
		writeError(c, err)
		return
	}
	out, err := s.PullOrg(c.Request.Context(), caller(c), c.Query("cursor"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (s *Service) handlePullSite(c *gin.Context) {
	limit, err := pullLimit(c)
	if err != nil {
		writeError(c, err)
		return
	}
	out, err := s.PullSite(c.Request.Context(), caller(c), c.Param("id"), c.Query("cursor"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (s *Service) handleReference(c *gin.Context) {
	out, err := s.Reference(c.Request.Context(), caller(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (s *Service) handlePush(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, MaxPushBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(c, &BatchError{Err: ErrBatchTooLarge, Index: -1})
			return
		}
		httpapi.WriteUnexpected(c, "push failed", err)
		return
	}
	var req PushRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(c, &BatchError{Err: ErrBadBatch, Index: -1})
		return
	}
	out, err := s.Push(c.Request.Context(), caller(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func writeError(c *gin.Context, err error) {
	var be *BatchError
	var up *UpstreamError
	switch {
	case errors.As(err, &be):
		p := BatchProblem{Error: be.Err.Error()}
		if be.Index >= 0 {
			i := be.Index
			p.Index = &i
		}
		c.JSON(http.StatusBadRequest, p)
	case errors.As(err, &up):
		if up.RetryAfter != nil {
			c.Header("Retry-After", strconv.Itoa(*up.RetryAfter))
		}
		c.Data(up.Status, "application/json; charset=utf-8", up.Body)
	case errors.Is(err, ErrBadCursor), errors.Is(err, ErrBadLimit):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, sites.ErrSiteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		httpapi.WriteUnexpected(c, "sync failed", err)
	}
}
