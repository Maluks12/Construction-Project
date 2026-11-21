package archtest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// CI run #90: the race step had no -timeout, so Go's default 10 minutes
// ended it with a goroutine dump instead of a slow-test report. Every
// go test in CI names its timeout, and every job has timeout-minutes.
func ciTimeoutViolations(yml string) []string {
	var out []string
	lines := strings.Split(strings.ReplaceAll(yml, "\r\n", "\n"), "\n")
	goTest := regexp.MustCompile(`(^|[\s;|&(])go test\s`)
	inJobs, job, jobHasTimeout := false, "", false
	endJob := func() {
		if job != "" && !jobHasTimeout {
			out = append(out, fmt.Sprintf("job %s has no timeout-minutes", job))
		}
	}
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		indent := len(l) - len(strings.TrimLeft(l, " "))
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case indent == 0:
			endJob()
			inJobs, job = trimmed == "jobs:", ""
		case inJobs && indent == 2 && strings.HasSuffix(trimmed, ":"):
			endJob()
			job, jobHasTimeout = strings.TrimSuffix(trimmed, ":"), false
		case inJobs && indent == 4 && strings.HasPrefix(trimmed, "timeout-minutes:"):
			jobHasTimeout = true
		}
		if goTest.MatchString(l) && !strings.Contains(l, "-timeout ") {
			out = append(out, fmt.Sprintf("ci.yml:%d runs go test without -timeout: %s", i+1, trimmed))
		}
	}
	endJob()
	return out
}

func TestCITimeouts(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate ci_test.go")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "go test") {
		t.Fatal("ci.yml runs no go test; update the guard")
	}
	for _, v := range ciTimeoutViolations(string(src)) {
		t.Error(v)
	}
}

func TestCITimeoutViolations(t *testing.T) {
	ok := "jobs:\n  test:\n    timeout-minutes: 30\n    steps:\n      - run: go test -race -timeout 15m ./...\n      # go test ./... in a comment\n"
	if v := ciTimeoutViolations(ok); len(v) != 0 {
		t.Fatalf("clean workflow flagged: %v", v)
	}
	for name, bad := range map[string]string{
		"no -timeout":         "jobs:\n  test:\n    timeout-minutes: 30\n    steps:\n      - run: go test -race ./...\n",
		"no -timeout in loop": "jobs:\n  test:\n    timeout-minutes: 30\n    steps:\n      - run: |\n          X=1 go test -run A ./internal/app/ | tee x\n",
		"no job timeout":      "jobs:\n  test:\n    steps:\n      - run: go test -timeout 1m ./...\n",
		"second job":          "jobs:\n  test:\n    timeout-minutes: 30\n  docker:\n    runs-on: x\n",
		"step-level only":     "jobs:\n  test:\n    steps:\n      - run: go build\n        timeout-minutes: 5\n",
	} {
		if v := ciTimeoutViolations(bad); len(v) == 0 {
			t.Errorf("%s: not flagged", name)
		}
	}
}
