package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const (
	ctxUserID    = "user_id"
	ctxOrgID     = "org_id"
	ctxSessionID = "session_id"
)

func RequireAuth(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			return
		}

		token, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		claims, err := ParseAccessToken(token, svc.secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		session, err := GetSession(ctx, svc.pool, claims.SessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if err != nil {
			slog.Error("require auth: get session", "err", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		if !session.Active(time.Now()) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Set(ctxUserID, claims.Subject)
		c.Set(ctxOrgID, claims.OrgID)
		c.Set(ctxSessionID, claims.SessionID)
		c.Next()
	}
}

func UserID(c *gin.Context) string    { return c.GetString(ctxUserID) }
func OrgID(c *gin.Context) string     { return c.GetString(ctxOrgID) }
func SessionID(c *gin.Context) string { return c.GetString(ctxSessionID) }
