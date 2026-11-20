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
			t.Setenv("S3_BUCKET", "evidence")
			t.Setenv("S3_EXPORT_BUCKET", "exports")
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

// The audit pack builder needs both buckets (M12).
func TestLoadWorkerBuckets(t *testing.T) {
	for _, missing := range []string{"S3_BUCKET", "S3_EXPORT_BUCKET"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("REDIS_URL", "redis://localhost:6379/0")
			t.Setenv("EMAIL_MODE", "log")
			t.Setenv("SMTP_URL", "")
			t.Setenv("S3_BUCKET", "evidence")
			t.Setenv("S3_EXPORT_BUCKET", "exports")
			t.Setenv(missing, "")
			if _, err := LoadWorker(); err == nil || !strings.Contains(err.Error(), missing) {
				t.Errorf("err %v, want it to name %s", err, missing)
			}
		})
	}
}
