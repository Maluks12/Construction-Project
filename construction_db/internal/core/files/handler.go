package files

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/httpapi"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func is(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// WriteError maps this package's errors; record packages that attach or
// show files use it for those.
func WriteError(c *gin.Context, err error) bool {
	var rej *Rejected
	if errors.As(err, &rej) {
		c.JSON(http.StatusBadRequest, gin.H{"error": rej.Error(), "rejected_reason": rej.Reason})
		return true
	}
	st, ok := errorStatus(err)
	if ok {
		c.JSON(st, gin.H{"error": err.Error()})
	}
	return ok
}

// errorStatus is the status this package answers err with; false for an
// error that isn't the caller's (a 5xx).
func errorStatus(err error) (int, bool) {
	switch {
	case is(err, ErrCantUpload, ErrCantViewHealth):
		return http.StatusForbidden, true
	case is(err, sites.ErrSiteNotFound, ErrFileNotFound):
		return http.StatusNotFound, true
	case is(err, ErrIDReused, ErrNotUploaded, ErrNotVerified):
		return http.StatusConflict, true
	case isSentinel(err):
		return http.StatusBadRequest, true
	}
	return 0, false
}

// DeclareBatch is POST /v1/files/batch (ONBOARD D11): many declarations in
// one request, each answered as its own PUT would be. Each is its own
// transaction, and repeating one is safe, so the batch isn't all or
// nothing: send it again after a failure.
func (h *Handler) DeclareBatch(c *gin.Context) {
	var req BatchDeclareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if n := len(req.Files); n < 1 || n > MaxBatchFiles {
		c.JSON(http.StatusBadRequest, gin.H{"error": ErrBatchSize.Error()})
		return
	}
	out := BatchDeclareResponse{Results: make([]BatchDeclareResult, 0, len(req.Files))}
	for _, it := range req.Files {
		f, created, err := h.svc.Declare(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), it.FileID, it.DeclareRequest)
		if err != nil {
			st, ok := errorStatus(err)
			if !ok {
				httpapi.WriteUnexpected(c, "declare files", err)
				return
			}
			msg := err.Error()
			out.Results = append(out.Results, BatchDeclareResult{FileID: it.FileID, Status: st, Error: &msg})
			continue
		}
		out.Results = append(out.Results, BatchDeclareResult{FileID: it.FileID, Status: status(created), File: f})
	}
	c.JSON(http.StatusOK, out)
}

func writeError(c *gin.Context, err error) {
	if !WriteError(c, err) {
		httpapi.WriteUnexpected(c, "request failed", err)
	}
}

func status(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func (h *Handler) Declare(c *gin.Context) {
	var req DeclareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	f, created, err := h.svc.Declare(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("fileId"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), f)
}

func (h *Handler) Complete(c *gin.Context) {
	f, created, err := h.svc.Complete(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("fileId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status(created), f)
}

func (h *Handler) Get(c *gin.Context) {
	f, err := h.svc.Get(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), c.Param("fileId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, f)
}
