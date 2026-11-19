package config

import (
	"strings"
	"testing"
)

// The worker never records an email as sent when it wasn't (M11 review):
// without SMTP_URL it refuses to start unless EMAIL_MODE=log says so.
func TestLoadWorkerEmailMode(t *testing.T) {
	tests := []struct {
		name, mode, smtp, wantMode, wantErr string
	}{
		{"smtp by default", "", "smtp://mail:1025?from=a@b.test", EmailSMTP, ""},
		{"no SMTP_URL and no mode", "", "", "", "SMTP_URL is required; set EMAIL_MODE=log"},
		{"smtp without a URL", "smtp", "", "", "SMTP_URL is required"},
		{"log, explicitly", "log", "", EmailLog, ""},
		{"log with a URL too", "log", "smtp://mail:1025?from=a@b.test", "", "EMAIL_MODE=log with SMTP_URL set"},
		{"an unknown mode", "console", "", "", "EMAIL_MODE must be smtp or log"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("REDIS_URL", "redis://localhost:6379/0")
			t.Setenv("SMTP_URL", tc.smtp)
			t.Setenv("EMAIL_MODE", tc.mode)
			w, err := LoadWorker()
			switch {
			case tc.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err %v, want %q", err, tc.wantErr)
				}
			case err != nil:
				t.Errorf("err %v", err)
			case w.EmailMode != tc.wantMode:
				t.Errorf("mode %q, want %q", w.EmailMode, tc.wantMode)
			}
		})
	}
}
