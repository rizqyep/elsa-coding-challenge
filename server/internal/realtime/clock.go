package realtime

import (
	"context"
	"sync/atomic"
	"time"
)

// RedisClock is the local clock shifted to Redis TIME, so pong's serverTime matches deadlines (TRD §7.3).
type RedisClock struct {
	source func(ctx context.Context) (time.Time, error)
	now    func() time.Time
	offset atomic.Int64 // nanoseconds
}

// NewRedisClock returns a clock synced from source, typically Redis TIME.
func NewRedisClock(source func(ctx context.Context) (time.Time, error)) *RedisClock {
	return newRedisClock(source, time.Now)
}

func newRedisClock(source func(ctx context.Context) (time.Time, error), now func() time.Time) *RedisClock {
	return &RedisClock{source: source, now: now}
}

// NowMs returns the current Redis-aligned time in epoch milliseconds.
func (c *RedisClock) NowMs() int64 {
	return c.now().Add(time.Duration(c.offset.Load())).UnixMilli()
}

// Sync measures the offset against the reading's midpoint; a failure keeps the last offset.
func (c *RedisClock) Sync(ctx context.Context) error {
	before := c.now()
	remote, err := c.source(ctx)
	if err != nil {
		return err
	}
	after := c.now()
	mid := before.Add(after.Sub(before) / 2)
	c.offset.Store(int64(remote.Sub(mid)))
	return nil
}

// Run syncs every interval until ctx ends.
func (c *RedisClock) Run(ctx context.Context, interval time.Duration, onErr func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.Sync(ctx); err != nil && ctx.Err() == nil {
				onErr(err)
			}
		}
	}
}
