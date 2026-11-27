package onboarding

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"construction_db/internal/core/auth"
	"construction_db/internal/platform/httpapi"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Documents(c *gin.Context) {
	dry := c.Query("dry_run")
	if dry != "" && dry != "true" && dry != "false" {
		c.JSON(http.StatusBadRequest, gin.H{"error": ErrBadDryRun.Error()})
		return
	}
	var req DocumentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.svc.SaveDocuments(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c), req, dry == "true")
	switch {
	case errors.Is(err, ErrFilesNotReady):
		c.JSON(http.StatusConflict, res)
	case errors.Is(err, ErrHasProblems):
		c.JSON(http.StatusBadRequest, res)
	case errors.Is(err, ErrBatchSize):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case err != nil:
		httpapi.WriteUnexpected(c, "save documents", err)
	case res.Created > 0 && !res.DryRun:
		c.JSON(http.StatusCreated, res)
	default:
		c.JSON(http.StatusOK, res)
	}
}

func (h *Handler) Progress(c *gin.Context) {
	p, err := h.svc.Progress(c.Request.Context(), auth.OrgID(c), auth.UserID(c), auth.Role(c))
	if err != nil {
		httpapi.WriteUnexpected(c, "onboarding progress", err)
		return
	}
	c.JSON(http.StatusOK, p)
}
