package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRejectNUL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var seen string
	r := gin.New()
	r.Use(RejectNUL())
	echo := func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		seen = string(b)
		c.Status(http.StatusNoContent)
	}
	r.POST("/things/:id", echo)
	r.GET("/things/:id", echo)

	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"a value", http.MethodPost, "/things/1", `{"a":"x\u0000y"}`, http.StatusBadRequest},
		{"nested in an array", http.MethodPost, "/things/1", `{"a":{"b":[1,"ok",{"c":"\u0000"}]}}`, http.StatusBadRequest},
		{"an object key", http.MethodPost, "/things/1", `{"a\u0000":1}`, http.StatusBadRequest},
		{"the query", http.MethodGet, "/things/1?q=a%00b", "", http.StatusBadRequest},
		{"a query key", http.MethodGet, "/things/1?a%00=1", "", http.StatusBadRequest},
		{"the path", http.MethodGet, "/things/a%00b", "", http.StatusBadRequest},
		{"an escaped backslash is text", http.MethodPost, "/things/1", `{"a":"x\\u0000y"}`, http.StatusNoContent},
		{"not JSON: the handler decides", http.MethodPost, "/things/1", `{"a":`, http.StatusNoContent},
		{"clean", http.MethodPost, "/things/1", `{"a":"café","n":1.5}`, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen = ""
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.want {
				t.Fatalf("got %d %s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
			if tc.want == http.StatusBadRequest && rec.Body.String() != `{"error":"`+NULText+`"}` {
				t.Errorf("body %s", rec.Body.String())
			}
			if tc.want == http.StatusNoContent && seen != tc.body {
				t.Errorf("the handler read %q, want the body unchanged", seen)
			}
		})
	}
}
