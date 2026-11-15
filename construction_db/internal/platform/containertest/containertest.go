// Package containertest helps test packages start their containers.
package containertest

import (
	"context"
	"fmt"
	"log"
	"time"
)

// RetryWait is how long StartWithRetry waits before its second attempt.
const RetryWait = 2 * time.Second

// StartWithRetry runs start, and if it fails, discards what it returned and
// tries once more after wait. Docker Desktop on Windows sometimes fails a
// container start when several test packages start theirs at once ("rootless
// Docker is not supported on Windows" means it couldn't connect); a second try
// usually works. A real fault fails twice, and both errors are reported.
func StartWithRetry[C any](ctx context.Context, name string, wait time.Duration,
	start func(context.Context) (C, error), discard func(C)) (C, error) {
	c, err := start(ctx)
	if err == nil {
		return c, nil
	}
	discard(c)
	log.Printf("start %s failed, trying once more in %s: %v", name, wait, err)

	select {
	case <-ctx.Done():
		var zero C
		return zero, fmt.Errorf("start %s: %w (gave up waiting to retry: %v)", name, err, ctx.Err())
	case <-time.After(wait):
	}

	c, err2 := start(ctx)
	if err2 != nil {
		discard(c)
		var zero C
		return zero, fmt.Errorf("start %s: failed twice: %w (first: %v)", name, err2, err)
	}
	return c, nil
}
