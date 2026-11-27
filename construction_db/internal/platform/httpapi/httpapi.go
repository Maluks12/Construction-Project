package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// RetryAfterSeconds is sent with every 503, so clients back off before retrying.
	RetryAfterSeconds = "2"

	// StatusClientClosedRequest is nginx's 499: the client left before the
	// answer. Nobody receives it; it keeps logs and metrics from counting a 500.
	StatusClientClosedRequest = 499

	// LockNotAvailable is Postgres's SQLSTATE when lock_timeout ends a wait.
	LockNotAvailable = "55P03"

	// DeadlockDetected is Postgres's SQLSTATE when it breaks a deadlock by
	// cancelling one transaction. Every writer takes locks in one order
	// (ONBOARD D3), so it shouldn't happen; if it does, the loser may retry.
	DeadlockDetected = "40P01"
)

// ErrBusy is a "try again" that isn't a timeout: a service that lost a race
// twice returns it, and WriteUnexpected answers it like a deadline, 503 with
// Retry-After (ONBOARD review).
var ErrBusy = errors.New("server busy")

// Deadline gives every request one deadline. Handlers pass c.Request.Context()
// down to the database and S3, so they all stop when it passes.
func Deadline(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// WriteUnexpected answers for an error no handler recognised. Running out of
// time (the deadline passed, including while waiting for a pool connection)
// means busy: 503 with Retry-After, logged as a warning. The client going
// away isn't a server error: 499, logged as info. Anything else is 500.
func WriteUnexpected(c *gin.Context, msg string, err error) {
	// pgxpool returns ctx.Err() and pgconn's timeout error wraps it, so this
	// one check covers both (pgx v5 pgconn/errors.go normalizeTimeoutError).
	// A lock wait past the pool's lock_timeout (database.Bounds), or a
	// deadlock Postgres broke, is the same: busy, retry.
	var pgErr *pgconn.PgError
	if lock := errors.As(err, &pgErr) && (pgErr.Code == LockNotAvailable || pgErr.Code == DeadlockDetected); lock || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBusy) {
		if lock {
			// Where says what it waited on ("while locking tuple (0,1) in relation ...").
			slog.Warn(msg, "path", c.Request.URL.Path, "err", err, "waited_on", pgErr.Where)
		} else {
			slog.Warn(msg, "path", c.Request.URL.Path, "err", err)
		}
		c.Header("Retry-After", RetryAfterSeconds)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "server busy, try again"})
		return
	}
	// Deadline cancels its own context only after the handler returns, so a
	// cancel seen here came from the client disconnecting.
	if errors.Is(err, context.Canceled) {
		slog.Info(msg, "path", c.Request.URL.Path, "err", err)
		c.AbortWithStatus(StatusClientClosedRequest)
		return
	}
	slog.Error(msg, "path", c.Request.URL.Path, "err", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
}
