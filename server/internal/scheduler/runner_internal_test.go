package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

// run starts the runner and returns a stop func that fails the test if Run doesn't return in time.
func run(t *testing.T, r *Runner) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	return func() {
		t.Helper()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Run did not return after stop")
		}
	}
}

// raiseMax sets m to n if n is larger.
func raiseMax(m *atomic.Int64, n int64) {
	for {
		cur := m.Load()
		if n <= cur || m.CompareAndSwap(cur, n) {
			return
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRunner_OverrunningPassesNeverOverlapAndSkipTicks(t *testing.T) {
	var running, maxRunning, passes atomic.Int64
	r := NewRunner(nil, Loop{Name: "slow", Interval: 10 * time.Millisecond, Pass: func(context.Context) error {
		n := running.Add(1)
		raiseMax(&maxRunning, n)
		time.Sleep(35 * time.Millisecond)
		running.Add(-1)
		passes.Add(1)
		return nil
	}})
	stop := run(t, r)
	time.Sleep(250 * time.Millisecond)
	stop()
	if maxRunning.Load() != 1 {
		t.Errorf("%d passes ran at once; a loop runs one pass at a time", maxRunning.Load())
	}
	if n := passes.Load(); n > 9 {
		t.Errorf("%d passes in 250 ms of 35 ms passes; missed ticks must be skipped, not queued", n)
	}
}

func TestRunner_LoopsAreIndependent(t *testing.T) {
	var fast atomic.Int64
	r := NewRunner(nil,
		Loop{Name: "slow", Interval: 10 * time.Millisecond, Pass: func(context.Context) error { time.Sleep(200 * time.Millisecond); return nil }},
		Loop{Name: "fast", Interval: 10 * time.Millisecond, Pass: func(context.Context) error { fast.Add(1); return nil }},
	)
	stop := run(t, r)
	time.Sleep(150 * time.Millisecond)
	stop()
	if n := fast.Load(); n < 5 {
		t.Errorf("fast loop ran %d times beside a slow one; loops must not wait for each other", n)
	}
}

func TestRunner_StopLetsTheInFlightPassFinish(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var cancelled atomic.Bool
	var passes atomic.Int64
	r := NewRunner(nil, Loop{Name: "flush", Interval: 5 * time.Millisecond, Pass: func(ctx context.Context) error {
		if passes.Add(1) == 1 {
			close(started)
			<-release
			cancelled.Store(ctx.Err() != nil)
		}
		return nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
		t.Fatal("Run returned while a pass was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after the pass finished")
	}
	if cancelled.Load() {
		t.Error("the in-flight pass was cancelled; stopping must not cancel work already claimed (TRD §8.4)")
	}
	if n := passes.Load(); n != 1 {
		t.Errorf("%d passes; none may start after stop", n)
	}
}

func TestRunner_HealthFailsWhenALoopStalls(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	block := make(chan struct{})
	var ran atomic.Int64
	r := newRunner(nil, clk.Now,
		Loop{Name: "transitions", Interval: 100 * time.Millisecond, Pass: func(context.Context) error {
			if ran.Add(1) > 1 {
				<-block // the second pass hangs
			}
			return nil
		}},
		Loop{Name: "leaderboard", Interval: 100 * time.Millisecond, Pass: func(context.Context) error {
			return errors.New("redis down") // failing, not stalled
		}},
	)
	stop := run(t, r)
	defer func() { close(block); stop() }()
	waitFor(t, "a first completed pass", func() bool { return ran.Load() >= 2 })
	if err := r.Healthy(); err != nil {
		t.Fatalf("healthy runner reported %v; a failing pass still completes", err)
	}
	clk.Advance(501 * time.Millisecond)
	// The leaderboard loop keeps completing (failed) passes, so it catches up with the jump; transitions can't.
	waitFor(t, "only the stalled loop reported", func() bool {
		err := r.Healthy()
		return err != nil && strings.Contains(err.Error(), "transitions") && !strings.Contains(err.Error(), "leaderboard")
	})
}

func TestForEach_BoundedAndComplete(t *testing.T) {
	items := make([]int, 50)
	for i := range items {
		items[i] = i
	}
	var running, maxRunning atomic.Int64
	var mu sync.Mutex
	seen := map[int]bool{}
	err := forEach(context.Background(), items, 4, func(_ context.Context, i int) error {
		n := running.Add(1)
		raiseMax(&maxRunning, n)
		time.Sleep(2 * time.Millisecond)
		running.Add(-1)
		mu.Lock()
		seen[i] = true
		mu.Unlock()
		if i == 7 {
			return errors.New("item 7 failed")
		}
		return nil
	})
	if m := maxRunning.Load(); m > 4 || m < 2 {
		t.Errorf("%d ran at once, want at most 4 and more than 1", m)
	}
	if len(seen) != 50 {
		t.Errorf("%d of 50 items processed; one failure must not stop the rest", len(seen))
	}
	if err == nil || !strings.Contains(err.Error(), "item 7") {
		t.Errorf("got %v, want item 7's error reported", err)
	}
}
