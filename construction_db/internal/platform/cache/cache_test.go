package cache

import (
	"context"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// Image is pinned like the other test containers.
const image = "redis:7.4-alpine"

func start(t *testing.T) *Client {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcredis.Run(ctx, image)
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })
	url, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// One instance claims a job per interval; a stale holder's release never
// frees a claim someone else holds now.
func TestClaim(t *testing.T) {
	ctx := context.Background()
	c := start(t)

	first, ok, err := c.Claim(ctx, "job:test", time.Hour)
	if err != nil || !ok {
		t.Fatalf("first claim: %v %v", ok, err)
	}
	if _, ok, err := c.Claim(ctx, "job:test", time.Hour); err != nil || ok {
		t.Fatalf("second claim while held: ok %v, err %v", ok, err)
	}
	if freed, err := c.Release(ctx, "job:test", "not-the-token"); err != nil || freed {
		t.Fatalf("release with another token: %v %v", freed, err)
	}
	if freed, err := c.Release(ctx, "job:test", first); err != nil || !freed {
		t.Fatalf("release with the token: %v %v", freed, err)
	}
	if _, ok, _ := c.Claim(ctx, "job:test", 50*time.Millisecond); !ok {
		t.Fatal("claim after release failed")
	}
	time.Sleep(150 * time.Millisecond)
	if _, ok, _ := c.Claim(ctx, "job:test", time.Hour); !ok {
		t.Fatal("an expired claim wasn't freed")
	}
	if err := c.Heartbeat(ctx, "test", time.Minute); err != nil {
		t.Fatal(err)
	}
}
