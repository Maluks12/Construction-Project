package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Deadline(300 * time.Millisecond))

	var left time.Duration
	var ok bool
	r.GET("/", func(c *gin.Context) {
		var deadline time.Time
		deadline, ok = c.Request.Context().Deadline()
		left = time.Until(deadline)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !ok || left <= 0 || left > 300*time.Millisecond {
		t.Errorf("handler's deadline: set %v, %s away; want set, at most 300ms away", ok, left)
	}
}

// captureLogs sends the default logger to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &logs
}

func TestWriteUnexpected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logs := captureLogs(t)

	tests := []struct {
		name       string
		err        error
		status     int
		body       string
		retryAfter string
		level      string
	}{
		{"internal error", errors.New("boom"), 500, `{"error":"internal error"}`, "", "ERROR"},
		{"deadline passed", fmt.Errorf("with tenant: %w", context.DeadlineExceeded), 503, `{"error":"server busy, try again"}`, "2", "WARN"},
		{"client went away", fmt.Errorf("with tenant: %w", context.Canceled), StatusClientClosedRequest, "", "", "INFO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

			WriteUnexpected(c, "request failed", tt.err)

			if rec.Code != tt.status || rec.Body.String() != tt.body || rec.Header().Get("Retry-After") != tt.retryAfter {
				t.Errorf("got %d %s Retry-After %q; want %d %s Retry-After %q",
					rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"), tt.status, tt.body, tt.retryAfter)
			}
			if !c.IsAborted() {
				t.Error("want the chain aborted, so middleware stops too")
			}
			var entry struct{ Level, Msg string }
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil || entry.Level != tt.level || entry.Msg != "request failed" {
				t.Errorf("log %q (err %v), want one %s entry", logs.String(), err, tt.level)
			}
		})
	}
}

// The client disconnects while the handler is working, as a phone losing
// signal would. That's logged as info, never as a server error.
func TestClientDisconnectMidHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logs := captureLogs(t)

	r := gin.New()
	r.Use(Deadline(time.Minute))
	clientCtx, disconnect := context.WithCancel(context.Background())
	r.GET("/", func(c *gin.Context) {
		disconnect()
		<-c.Request.Context().Done() // the query the handler was waiting on is cut off
		WriteUnexpected(c, "request failed", fmt.Errorf("list sites: %w", c.Request.Context().Err()))
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(clientCtx))

	if rec.Code != StatusClientClosedRequest {
		t.Errorf("status %d, want %d", rec.Code, StatusClientClosedRequest)
	}
	var entry struct{ Level string }
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil || entry.Level != "INFO" {
		t.Errorf("log %q (err %v), want one INFO entry", logs.String(), err)
	}
}
