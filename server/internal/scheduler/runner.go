package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// stallFactor is how many intervals a loop may go without a completed pass before liveness fails (TRD §8.3).
const stallFactor = 5

// Loop is one claim-and-process pass, run every Interval.
type Loop struct {
	Name     string
	Interval time.Duration
	Pass     func(ctx context.Context) error
}

// Runner runs loops independently and tracks their liveness.
type Runner struct {
	loops []Loop
	last  []atomic.Int64 // unix nanos of each loop's last completed pass
	now   func() time.Time
	log   *slog.Logger
}

// NewRunner returns a runner; log may be nil.
func NewRunner(log *slog.Logger, loops ...Loop) *Runner { return newRunner(log, time.Now, loops...) }

func newRunner(log *slog.Logger, now func() time.Time, loops ...Loop) *Runner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	r := &Runner{loops: loops, last: make([]atomic.Int64, len(loops)), now: now, log: log}
	for i := range r.last {
		r.last[i].Store(now().UnixNano())
	}
	return r
}

// Run blocks until ctx ends and every in-flight pass has finished; stopping never cancels claimed work (TRD §8.4).
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := range r.loops {
		wg.Go(func() { r.runLoop(ctx, i) })
	}
	wg.Wait()
}

// runLoop runs one pass per tick; a ticker drops ticks while a pass overruns, so passes never pile up.
func (r *Runner) runLoop(ctx context.Context, i int) {
	l := r.loops[i]
	t := time.NewTicker(l.Interval)
	defer t.Stop()
	work := context.WithoutCancel(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if ctx.Err() != nil {
			return
		}
		if err := l.Pass(work); err != nil {
			r.log.Warn("loop pass failed", slog.String("loop", l.Name), slog.Any("error", err))
		}
		r.last[i].Store(r.now().UnixNano())
	}
}

// Healthy fails when a loop hasn't completed a pass in stallFactor intervals; a failed pass still counts.
func (r *Runner) Healthy() error {
	now := r.now()
	var errs []error
	for i, l := range r.loops {
		if since := now.Sub(time.Unix(0, r.last[i].Load())); since > stallFactor*l.Interval {
			errs = append(errs, fmt.Errorf("%s loop: no completed pass for %s", l.Name, since.Round(time.Millisecond)))
		}
	}
	return errors.Join(errs...)
}

// forEach runs fn for every item, at most parallel at once, and returns all errors joined.
func forEach[T any](ctx context.Context, items []T, parallel int, fn func(context.Context, T) error) error {
	sem := make(chan struct{}, max(parallel, 1))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for _, it := range items {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			if err := fn(ctx, it); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}
