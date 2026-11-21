package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every variable the API or the worker won't start without is in
// .env.example (REG4 review: S3_EXPORT_BUCKET was missing from a .env).
func TestEnvExampleHasEveryRequiredVariable(t *testing.T) {
	example, err := os.ReadFile("../../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"DATABASE_URL", "JWT_SECRET", "S3_BUCKET", "S3_EXPORT_BUCKET", "EVIDENCE_LOCK_MODE",
		"REDIS_URL", "SMTP_URL", "EMAIL_MODE"} {
		t.Setenv(k, "")
	}
	listed := regexp.MustCompile(`missing required environment variable: (.+)`)
	required := map[string]bool{"SMTP_URL": true} // the worker's, unless EMAIL_MODE=log
	_, apiErr := Load()
	_, workerErr := LoadWorker()
	for _, err := range []error{apiErr, workerErr} {
		m := listed.FindStringSubmatch(err.Error())
		if m == nil {
			t.Fatalf("with nothing set: %v; want the missing variables listed", err)
		}
		for _, name := range strings.Split(m[1], ", ") {
			required[name] = true
		}
	}
	for name := range required {
		if !regexp.MustCompile(`(?m)^#?\s*` + name + `=`).Match(example) {
			t.Errorf(".env.example has no %s=, which is required", name)
		}
	}
	if len(required) < 7 {
		t.Errorf("only %d required variables found: %v", len(required), required)
	}
}
