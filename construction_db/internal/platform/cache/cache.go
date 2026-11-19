// Package cache is the worker's Redis: when jobs run, never whether
// anything is sent (Postgres decides that, M11 D24). Losing Redis loses
// timing only.
package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

// Connect opens REDIS_URL (redis://[:password@]host:port/db) and pings it.
func Connect(ctx context.Context, url string) (*Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis url: %w", err)
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb}, nil
}

func (c *Client) Close() error { return c.rdb.Close() }

// Claim takes key for ttl if nobody holds it, and returns the token that
// proves this caller holds it. The worker claims "job:<name>" for the job's
// interval: one instance runs each job per interval, and a crashed run's
// claim simply expires.
func (c *Client) Claim(ctx context.Context, key string, ttl time.Duration) (token string, ok bool, err error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(b)
	ok, err = c.rdb.SetNX(ctx, "siteproof:"+key, token, ttl).Result()
	if err != nil {
		return "", false, fmt.Errorf("claim %s: %w", key, err)
	}
	return token, ok, nil
}

// release deletes key only while it still holds token: a claim that expired
// and was taken by another instance is never deleted from under it.
var release = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)

// Release gives a claim back early (a job that must run again soon).
func (c *Client) Release(ctx context.Context, key, token string) (bool, error) {
	n, err := release.Run(ctx, c.rdb, []string{"siteproof:" + key}, token).Int()
	if err != nil {
		return false, fmt.Errorf("release %s: %w", key, err)
	}
	return n == 1, nil
}

// Heartbeat records that instance is alive, for ttl.
func (c *Client) Heartbeat(ctx context.Context, instance string, ttl time.Duration) error {
	return c.rdb.Set(ctx, "siteproof:worker:"+instance, time.Now().UTC().Format(time.RFC3339), ttl).Err()
}
