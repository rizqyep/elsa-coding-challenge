package redisx

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Clock is the local clock shifted to Redis TIME, so services agree with the time scripts stamp (TRD §7.3).
type Clock struct {
	source func(ctx context.Context) (time.Time, error)
	now    func() time.Time
	offset atomic.Int64 // nanoseconds
}

// NewClock returns a clock synced from source, typically Redis TIME.
func NewClock(source func(ctx context.Context) (time.Time, error)) *Clock {
	return newClock(source, time.Now)
}

// ClockFor returns a clock synced from rdb's TIME.
func ClockFor(rdb redis.UniversalClient) *Clock {
	return NewClock(func(ctx context.Context) (time.Time, error) { return rdb.Time(ctx).Result() })
}

func newClock(source func(ctx context.Context) (time.Time, error), now func() time.Time) *Clock {
	return &Clock{source: source, now: now}
}

// NowMs returns the current Redis-aligned time in epoch milliseconds.
func (c *Clock) NowMs() int64 {
	return c.now().Add(time.Duration(c.offset.Load())).UnixMilli()
}

// Sync measures the offset against the reading's midpoint; a failure keeps the last offset.
func (c *Clock) Sync(ctx context.Context) error {
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
func (c *Clock) Run(ctx context.Context, interval time.Duration, onErr func(error)) {
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
