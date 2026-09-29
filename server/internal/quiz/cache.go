package quiz

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
)

//go:generate go tool -modfile=../../tools/go.mod mockgen -source=cache.go -destination=mocks/cache.go -package=mocks

// SetLoader reads a whole question set, or ErrQuestionSetNotFound.
type SetLoader interface {
	QuestionSet(ctx context.Context, id QuestionSetID) (QuestionSet, error)
}

// CacheOptions configure a Cache (TRD §2.3, §7.8).
type CacheOptions struct {
	MaxSets        int
	Retry          retry.Retrier // Retryable is set to retry.Transient; a zero Clock means real time
	AttemptTimeout time.Duration
	Log            *slog.Logger
}

// Cache holds question sets with their answer keys, so answers are checked without external reads (NFR-9).
// Sets are immutable once cached and shared by every room using them.
type Cache struct {
	loader  SetLoader
	opts    CacheOptions
	retrier retry.Retrier
	flight  singleflight.Group
	tick    atomic.Uint64

	mu      sync.RWMutex
	entries map[QuestionSetID]*cacheEntry
}

type cacheEntry struct {
	set  *QuestionSet
	refs int           // local rooms using the set; guarded by Cache.mu
	used atomic.Uint64 // last use, for LRU eviction
}

// NewCache returns an empty cache over loader.
func NewCache(loader SetLoader, opts CacheOptions) *Cache {
	r := opts.Retry
	if r.Clock == nil {
		r = retry.New(r.Policy, nil)
	}
	r.Retryable = retry.Transient
	return &Cache{loader: loader, opts: opts, retrier: r, entries: map[QuestionSetID]*cacheEntry{}}
}

// Acquire returns the set, loading it once however many callers ask at the same time, and holds a
// reference until Release. A caller that gives up doesn't cancel the load for the others.
func (c *Cache) Acquire(ctx context.Context, id QuestionSetID) (*QuestionSet, error) {
	if s, ok := c.cached(id); ok {
		return c.hold(id, s), nil
	}
	ch := c.flight.DoChan(string(id), func() (any, error) { return c.load(context.WithoutCancel(ctx), id) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		return c.hold(id, r.Val.(*QuestionSet)), nil
	}
}

// Get returns a cached set without loading it; the answer path uses it.
func (c *Cache) Get(id QuestionSetID) (*QuestionSet, bool) { return c.cached(id) }

// Release drops one reference; unreferenced sets become evictable.
func (c *Cache) Release(id QuestionSetID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[id]; ok && e.refs > 0 {
		e.refs--
	}
	c.evictLocked()
}

func (c *Cache) cached(id QuestionSetID) (*QuestionSet, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[id]
	if !ok {
		return nil, false
	}
	e.used.Store(c.tick.Add(1))
	return e.set, true
}

func (c *Cache) hold(id QuestionSetID, s *QuestionSet) *QuestionSet {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok { // evicted between load and hold
		e = &cacheEntry{set: s}
		c.entries[id] = e
	}
	e.refs++
	e.used.Store(c.tick.Add(1))
	c.evictLocked()
	return e.set
}

// load runs once per set at a time, inside the single flight, and caches before returning so a
// caller arriving just after the flight finds the set instead of starting a second load.
func (c *Cache) load(ctx context.Context, id QuestionSetID) (any, error) {
	var s QuestionSet
	err := c.retrier.Do(ctx, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, c.opts.AttemptTimeout)
		defer cancel()
		var err error
		s, err = c.loader.QuestionSet(ctx, id)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("load question set %s: %w", id, err)
	}
	if err := ValidateSet(s); err != nil {
		c.opts.Log.ErrorContext(ctx, "refusing malformed question set", "question_set_id", string(id), "error", err)
		return nil, err
	}
	p := &s
	c.mu.Lock()
	if _, ok := c.entries[id]; !ok {
		e := &cacheEntry{set: p}
		e.used.Store(c.tick.Add(1))
		c.entries[id] = e
	}
	c.mu.Unlock()
	return p, nil
}

// evictLocked removes least-recently-used unreferenced sets while over the cap. Sets in use are
// never evicted, so the cap can be exceeded while every cached set is live.
func (c *Cache) evictLocked() {
	for len(c.entries) > c.opts.MaxSets {
		var victim QuestionSetID
		var oldest uint64
		found := false
		for id, e := range c.entries {
			if u := e.used.Load(); e.refs == 0 && (!found || u < oldest) {
				victim, oldest, found = id, u, true
			}
		}
		if !found {
			return
		}
		delete(c.entries, victim)
	}
}
