package redisx

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewClient returns a Redis client; per-call deadlines are set by callers (TRD §9.2).
func NewClient(addr, password string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
}

// Ping checks the connection within the given timeout.
func Ping(ctx context.Context, c redis.UniversalClient, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Ping(ctx).Err()
}
