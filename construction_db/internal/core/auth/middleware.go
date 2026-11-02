package auth

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/database"
	"construction_db/internal/platform/httpapi"
)

const (
	ctxUserID    = "user_id"
	ctxOrgID     = "org_id"
	ctxSessionID = "session_id"
	ctxRole      = "role"
)

func RequireAuth(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		// The request's own deadline (httpapi.Deadline) bounds this, waiting
		// for a pool connection included.
		ctx := c.Request.Context()

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

		var session *Session
		var m *Membership
		tenant := database.Tenant{OrgID: claims.OrgID, UserID: claims.Subject}
		err = database.WithTenant(ctx, svc.pool, tenant, func(tx pgx.Tx) error {
			var err error
			if session, err = GetSession(ctx, tx, claims.SessionID); err != nil {
				return err
			}
			m, err = GetMembership(ctx, tx, claims.OrgID, claims.Subject)
			return err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if err != nil {
			httpapi.WriteUnexpected(c, "require auth: load session and membership", err)
			return
		}

		if !session.Active(time.Now()) || m.Status != MemberActive {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Set(ctxRole, m.Role)
		c.Set(ctxUserID, claims.Subject)
		c.Set(ctxOrgID, claims.OrgID)
		c.Set(ctxSessionID, claims.SessionID)
		c.Next()
	}
}

func UserID(c *gin.Context) string    { return c.GetString(ctxUserID) }
func OrgID(c *gin.Context) string     { return c.GetString(ctxOrgID) }
func SessionID(c *gin.Context) string { return c.GetString(ctxSessionID) }

func Role(c *gin.Context) string {
	return c.GetString(ctxRole)
}

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if slices.Contains(roles, Role(c)) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	}
}
