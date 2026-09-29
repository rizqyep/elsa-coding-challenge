package redisx

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type fakeTimeSource struct {
	offset time.Duration
	local  *fakeClock
	err    error
}

func (s *fakeTimeSource) Time(context.Context) (time.Time, error) {
	if s.err != nil {
		return time.Time{}, s.err
	}
	return s.local.Now().Add(s.offset), nil
}

func TestClock(t *testing.T) {
	local := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	src := &fakeTimeSource{offset: 5 * time.Second, local: local}
	clk := newClock(src.Time, local.Now)

	if got, want := clk.NowMs(), local.Now().UnixMilli(); got != want {
		t.Errorf("before the first sync: %d, want the local clock %d", got, want)
	}
	if err := clk.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := clk.NowMs(), local.Now().Add(5*time.Second).UnixMilli(); got != want {
		t.Errorf("after sync: %d, want Redis time %d", got, want)
	}
	local.Advance(time.Second)
	if got, want := clk.NowMs(), local.Now().Add(5*time.Second).UnixMilli(); got != want {
		t.Errorf("a second later: %d, want %d; the offset is kept, not the reading", got, want)
	}

	src.err = errors.New("redis down")
	if err := clk.Sync(context.Background()); err == nil {
		t.Error("a failed sync reported no error")
	}
	if got, want := clk.NowMs(), local.Now().Add(5*time.Second).UnixMilli(); got != want {
		t.Errorf("after a failed sync: %d, want the last offset kept (%d)", got, want)
	}
}
