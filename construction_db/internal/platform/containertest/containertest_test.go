package containertest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fake fails the first n calls to start, returning a half-made "container"
// each time, and records what was discarded.
type fake struct {
	failures, calls int
	discarded       []string
}

func (f *fake) start(context.Context) (string, error) {
	f.calls++
	if f.calls <= f.failures {
		return "partial", errors.New("docker unreachable")
	}
	return "ready", nil
}

func (f *fake) discard(c string) { f.discarded = append(f.discarded, c) }

func TestStartWithRetry(t *testing.T) {
	ctx := context.Background()

	t.Run("works first time: one start, nothing discarded", func(t *testing.T) {
		f := &fake{}
		c, err := StartWithRetry(ctx, "db", 0, f.start, f.discard)
		if err != nil || c != "ready" || f.calls != 1 || len(f.discarded) != 0 {
			t.Errorf("got %q, %v; calls %d, discarded %v", c, err, f.calls, f.discarded)
		}
	})

	t.Run("fails once: the half-made one is discarded and the retry wins", func(t *testing.T) {
		f := &fake{failures: 1}
		c, err := StartWithRetry(ctx, "db", 0, f.start, f.discard)
		if err != nil || c != "ready" || f.calls != 2 || len(f.discarded) != 1 {
			t.Errorf("got %q, %v; calls %d, discarded %v", c, err, f.calls, f.discarded)
		}
	})

	t.Run("fails twice: both errors reported, both discarded, no third try", func(t *testing.T) {
		f := &fake{failures: 5}
		_, err := StartWithRetry(ctx, "db", 0, f.start, f.discard)
		if err == nil || !strings.Contains(err.Error(), "failed twice") || f.calls != 2 || len(f.discarded) != 2 {
			t.Errorf("err %v; calls %d, discarded %v", err, f.calls, f.discarded)
		}
	})

	t.Run("a cancelled context stops the wait", func(t *testing.T) {
		f := &fake{failures: 1}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		began := time.Now()
		_, err := StartWithRetry(cancelled, "db", time.Hour, f.start, f.discard)
		if err == nil || !strings.Contains(err.Error(), "gave up waiting to retry") || f.calls != 1 || time.Since(began) > time.Second {
			t.Errorf("err %v after %s; calls %d", err, time.Since(began), f.calls)
		}
	})
}
