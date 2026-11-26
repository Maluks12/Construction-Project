package notifications

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/auth"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/httpapi"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

const inboxSQL = `
	SELECT id::text, kind, subject_type, subject_id::text, threshold, site_id::text, title, due_on::text, due_at, created_at, read_at,
	       CASE WHEN notification_subject_current(subject_type, subject_id, threshold) THEN 'current' ELSE 'superseded' END
	FROM notifications`

func scan(row pgx.Row) (*Notification, error) {
	var n Notification
	err := row.Scan(&n.ID, &n.Kind, &n.SubjectType, &n.SubjectID, &n.Threshold, &n.SiteID, &n.Title, &n.DueOn, &n.DueAt, &n.CreatedAt,
		&n.ReadAt, &n.SubjectState)
	return &n, err
}

// List is the caller's own notifications, newest first.
func (s *Service) List(ctx context.Context, orgID, userID string, unreadOnly bool, limit int) ([]Notification, error) {
	if limit < 1 || limit > 200 {
		return nil, ErrBadLimit
	}
	out := []Notification{}
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, inboxSQL+` WHERE org_id = $1 AND user_id = $2 AND (NOT $3 OR read_at IS NULL)
			ORDER BY created_at DESC, id LIMIT $4`, orgID, userID, unreadOnly, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			n, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, *n)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	return out, nil
}

// Read marks one of the caller's notifications read. Reading it again
// answers it unchanged.
func (s *Service) Read(ctx context.Context, orgID, userID, id string) (*Notification, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrNotFound
	}
	var n *Notification
	err := database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
		var err error
		n, err = scan(tx.QueryRow(ctx, inboxSQL+` WHERE org_id = $1 AND user_id = $2 AND id = $3`, orgID, userID, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil || n.ReadAt != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE id = $1 AND read_at IS NULL`, id); err != nil {
			return err
		}
		n, err = scan(tx.QueryRow(ctx, inboxSQL+` WHERE id = $1`, id))
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("read notification: %w", err)
	}
	return n, nil
}

// Register mounts the inbox: every role reads its own.
func Register(r *gin.Engine, svc *Service, authSvc *auth.Service) {
	g := r.Group("/v1/notifications", auth.RequireAuth(authSvc))
	g.GET("", func(c *gin.Context) {
		limit := 50
		if s := c.Query("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": ErrBadLimit.Error()})
				return
			}
			limit = n
		}
		unread := false
		switch c.Query("unread") {
		case "", "false":
		case "true":
			unread = true
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": ErrBadUnread.Error()})
			return
		}
		out, err := svc.List(c.Request.Context(), auth.OrgID(c), auth.UserID(c), unread, limit)
		switch {
		case errors.Is(err, ErrBadLimit):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case err != nil:
			httpapi.WriteUnexpected(c, "request failed", err)
		default:
			c.JSON(http.StatusOK, gin.H{"notifications": out})
		}
	})
	g.POST("/:notificationId/read", func(c *gin.Context) {
		n, err := svc.Read(c.Request.Context(), auth.OrgID(c), auth.UserID(c), c.Param("notificationId"))
		switch {
		case errors.Is(err, ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case err != nil:
			httpapi.WriteUnexpected(c, "request failed", err)
		default:
			c.JSON(http.StatusOK, n)
		}
	})
}

// JobsHealth is /healthz/jobs: 503 while any worker job has gone twice its
// interval without completing (Rev 4 §2.3: alert on non-completion), so an
// uptime monitor pages; 200 otherwise.
func JobsHealth(pool *pgxpool.Pool, clk clock.Clock) gin.HandlerFunc {
	return func(c *gin.Context) {
		stale, err := Stale(c.Request.Context(), pool, clk.Now())
		if err != nil {
			httpapi.WriteUnexpected(c, "request failed", err)
			return
		}
		// A pull never passes a write still open anywhere on the server
		// (SYNC D1), so one left open holds every phone's pull back.
		var oldest float64
		if err := pool.QueryRow(c.Request.Context(), `SELECT sync_oldest_write_age()`).Scan(&oldest); err != nil {
			httpapi.WriteUnexpected(c, "request failed", err)
			return
		}
		if oldest > PullLagLimit.Seconds() {
			stale = append(stale, StaleSyncPull)
		}
		if len(stale) > 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "stale", "stale": stale, "oldest_write_seconds": oldest})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "stale": []string{}, "oldest_write_seconds": oldest})
	}
}

// StaleSyncPull is /healthz/jobs' entry when a write transaction has been open
// longer than PullLagLimit: phones' pulls are held back behind it.
const StaleSyncPull = "sync_pull"

// PullLagLimit: no API transaction lasts beyond the request deadline (10 s)
// and the idle limit (60 s), and the worker's writes are short; a write open
// longer than this is someone's session or a migration. Tests shorten it.
var PullLagLimit = 5 * time.Minute
