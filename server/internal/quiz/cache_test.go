package quiz_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz/mocks"
)

var pgDown = &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}

// fakeClock advances instantly when slept on, and records the sleeps.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.sleeps = append(c.sleeps, d)
	return ctx.Err()
}

type cacheFixture struct {
	loader *mocks.MockSetLoader
	clock  *fakeClock
	logs   *bytes.Buffer
	cache  *quiz.Cache
}

func newCache(t *testing.T, maxSets int) cacheFixture {
	f := cacheFixture{loader: mocks.NewMockSetLoader(gomock.NewController(t)), clock: &fakeClock{now: time.Unix(0, 0)}, logs: &bytes.Buffer{}}
	f.cache = quiz.NewCache(f.loader, quiz.CacheOptions{
		MaxSets: maxSets,
		Retry: retry.Retrier{
			Policy: retry.Policy{Base: 100 * time.Millisecond, Cap: 5 * time.Second, Budget: 10 * time.Second},
			Clock:  f.clock, Rand: func(n int64) int64 { return n - 1 },
		},
		AttemptTimeout: 2 * time.Second,
		Log:            slog.New(slog.NewTextHandler(f.logs, nil)),
	})
	return f
}

func (f cacheFixture) loads(id string, times int) {
	f.loader.EXPECT().QuestionSet(gomock.Any(), quiz.QuestionSetID(id)).Return(set(id, 3), nil).Times(times)
}

// NFR-9: a burst of joins for an uncached set triggers one PostgreSQL load.
func TestCache_SingleFlight(t *testing.T) {
	f := newCache(t, 10)
	started, release := make(chan struct{}), make(chan struct{})
	var loads atomic.Int32 // counted here, not with Times(1): a gomock failure inside the load goroutine would hang the callers
	f.loader.EXPECT().QuestionSet(gomock.Any(), quiz.QuestionSetID("s")).DoAndReturn(func(context.Context, quiz.QuestionSetID) (quiz.QuestionSet, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		<-release
		return set("s", 3), nil
	}).AnyTimes()

	const n, total = 1000, 1000
	results := make([]*quiz.QuestionSet, n)
	var wg sync.WaitGroup
	var calling atomic.Int32
	acquire := func(i int) {
		defer wg.Done()
		calling.Add(1)
		s, err := f.cache.Acquire(bounded(t), "s")
		if err != nil {
			t.Error(err)
		}
		results[i] = s
	}
	wg.Add(n)
	go acquire(0)
	<-started
	for i := 1; i < n; i++ {
		go acquire(i)
	}
	for calling.Load() < n {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if n := loads.Load(); n != 1 {
		t.Fatalf("%d loads for %d concurrent callers, want 1", n, total)
	}
	for i, s := range results {
		if s == nil || s != results[0] {
			t.Fatalf("caller %d got %p, want the shared %p", i, s, results[0])
		}
	}
	if got := f.cache.Refs("s"); got != n {
		t.Errorf("refs = %d, want %d", got, n)
	}
}

// A caller that gives up must not cancel the load others are waiting on.
func TestCache_CallerCancellationDoesNotCancelTheLoad(t *testing.T) {
	f := newCache(t, 10)
	started, release := make(chan struct{}), make(chan struct{})
	f.loader.EXPECT().QuestionSet(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ quiz.QuestionSetID) (quiz.QuestionSet, error) {
		close(started)
		<-release
		if err := ctx.Err(); err != nil {
			return quiz.QuestionSet{}, err
		}
		return set("s", 3), nil
	}).Times(1)

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := f.cache.Acquire(ctx, "s"); first <- err }()
	<-started
	second := make(chan error, 1)
	go func() { _, err := f.cache.Acquire(bounded(t), "s"); second <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled caller got %v, want context.Canceled", err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Errorf("other caller got %v, want the set", err)
	}
	if f.cache.Refs("s") != 1 {
		t.Errorf("refs = %d, want 1 (the cancelled caller holds none)", f.cache.Refs("s"))
	}
}

// Non-functional §3.1: PostgreSQL hiccups are retried with capped, jittered backoff.
func TestCache_RetriesTransientFailures(t *testing.T) {
	f := newCache(t, 10)
	gomock.InOrder(
		f.loader.EXPECT().QuestionSet(gomock.Any(), gomock.Any()).Return(quiz.QuestionSet{}, pgDown).Times(3),
		f.loader.EXPECT().QuestionSet(gomock.Any(), gomock.Any()).Return(set("s", 3), nil),
	)
	s, err := f.cache.Acquire(bounded(t), "s")
	if err != nil || s.ID != "s" {
		t.Fatalf("got %v, %v", s, err)
	}
	if want := []time.Duration{100*time.Millisecond - 1, 200*time.Millisecond - 1, 400*time.Millisecond - 1}; !equalDurations(f.clock.sleeps, want) {
		t.Errorf("slept %v, want %v", f.clock.sleeps, want)
	}
}

// When the budget runs out the join fails with a retryable error (server_busy / 503), and nothing is cached.
func TestCache_BudgetExhausted(t *testing.T) {
	f := newCache(t, 10)
	var down atomic.Bool
	down.Store(true)
	f.loader.EXPECT().QuestionSet(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, quiz.QuestionSetID) (quiz.QuestionSet, error) {
		if down.Load() {
			return quiz.QuestionSet{}, pgDown
		}
		return set("s", 3), nil
	}).MinTimes(3)
	_, err := f.cache.Acquire(bounded(t), "s")
	if !errors.Is(err, retry.ErrBudgetExhausted) || !retry.Transient(err) {
		t.Fatalf("got %v; want budget exhausted and transient", err)
	}
	var total time.Duration
	for _, d := range f.clock.sleeps {
		total += d
	}
	if total > 10*time.Second {
		t.Errorf("slept %v in total, over the 10s budget", total)
	}
	if _, ok := f.cache.Get("s"); ok || f.cache.Refs("s") != 0 {
		t.Error("a failed load left an entry behind")
	}
	down.Store(false)
	if _, err := f.cache.Acquire(bounded(t), "s"); err != nil {
		t.Errorf("load after recovery: %v", err)
	}
}

// Permanent problems are not retried and not cached.
func TestCache_PermanentFailures(t *testing.T) {
	t.Run("unknown set", func(t *testing.T) {
		f := newCache(t, 10)
		f.loader.EXPECT().QuestionSet(gomock.Any(), gomock.Any()).Return(quiz.QuestionSet{}, quiz.ErrQuestionSetNotFound).Times(1)
		if _, err := f.cache.Acquire(bounded(t), "nope"); !errors.Is(err, quiz.ErrQuestionSetNotFound) || retry.Transient(err) {
			t.Errorf("got %v, want a permanent ErrQuestionSetNotFound", err)
		}
	})
	t.Run("malformed set is refused and logged", func(t *testing.T) {
		f := newCache(t, 10)
		bad := set("bad", 3)
		bad.Questions[1].CorrectOptionID = "elsewhere"
		f.loader.EXPECT().QuestionSet(gomock.Any(), gomock.Any()).Return(bad, nil).Times(1)
		if _, err := f.cache.Acquire(bounded(t), "bad"); !errors.Is(err, quiz.ErrMalformedSet) || retry.Transient(err) {
			t.Errorf("got %v, want a permanent ErrMalformedSet", err)
		}
		if _, ok := f.cache.Get("bad"); ok {
			t.Error("malformed set was cached")
		}
		if !bytes.Contains(f.logs.Bytes(), []byte("level=ERROR")) || !bytes.Contains(f.logs.Bytes(), []byte("bad")) {
			t.Errorf("no error logged naming the set: %q", f.logs.String())
		}
	})
}

// Get is the answer path: it never loads.
func TestCache_GetNeverLoads(t *testing.T) {
	f := newCache(t, 10) // no loader expectations
	if _, ok := f.cache.Get("s"); ok {
		t.Error("Get found an unloaded set")
	}
}

// TRD §7.8: sets used by local rooms stay; unused ones are evicted least-recently-used beyond the cap.
func TestCache_ReferenceCountingAndLRU(t *testing.T) {
	f := newCache(t, 2)
	ctx := bounded(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		f.loads(id, 1)
	}
	must := func(id string) {
		t.Helper()
		if _, err := f.cache.Acquire(ctx, quiz.QuestionSetID(id)); err != nil {
			t.Fatal(err)
		}
	}
	cached := func(want map[string]bool) {
		t.Helper()
		for id, in := range want {
			if _, ok := f.cache.Get(quiz.QuestionSetID(id)); ok != in {
				t.Errorf("%s cached = %v, want %v", id, ok, in)
			}
		}
	}

	must("a") // a: 1 ref
	must("b") // b: 1 ref
	f.cache.Release("b")
	must("c") // over the cap: b is the only unreferenced set
	cached(map[string]bool{"a": true, "b": false, "c": true})

	f.cache.Release("a")
	f.cache.Release("c")
	f.cache.Get("a") // a is now more recently used than c
	must("d")        // evicts c, the least recently used
	cached(map[string]bool{"a": true, "c": false, "d": true})

	must("a")
	must("d")
	f.cache.Release("a")
	f.cache.Release("a")
	f.cache.Release("a") // over-release is ignored
	if f.cache.Refs("a") != 0 {
		t.Errorf("refs(a) = %d after over-release, want 0", f.cache.Refs("a"))
	}
	f.cache.Release("zzz") // unknown: ignored
}

// When every cached set is in use the cap is exceeded rather than evicting a live room's set.
func TestCache_NeverEvictsReferencedSets(t *testing.T) {
	f := newCache(t, 1)
	for _, id := range []string{"a", "b", "c"} {
		f.loads(id, 1)
		if _, err := f.cache.Acquire(bounded(t), quiz.QuestionSetID(id)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []quiz.QuestionSetID{"a", "b", "c"} {
		if _, ok := f.cache.Get(id); !ok {
			t.Errorf("referenced set %s evicted", id)
		}
	}
}

// Under -race: concurrent acquire, get, and release of a small set of IDs.
func TestCache_ConcurrentUse(t *testing.T) {
	f := newCache(t, 2)
	f.loader.EXPECT().QuestionSet(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id quiz.QuestionSetID) (quiz.QuestionSet, error) {
		return set(string(id), 3), nil
	}).AnyTimes()
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := quiz.QuestionSetID([]string{"a", "b", "c", "d"}[i%4])
			for range 200 {
				s, err := f.cache.Acquire(bounded(t), id)
				if err != nil || s.ID != id {
					t.Errorf("acquire %s: %v, %v", id, s, err)
					return
				}
				if got, ok := f.cache.Get(id); !ok || got.ID != id {
					t.Errorf("get %s while referenced: %v", id, ok)
				}
				f.cache.Release(id)
			}
		}()
	}
	wg.Wait()
	for _, id := range []quiz.QuestionSetID{"a", "b", "c", "d"} {
		if r := f.cache.Refs(id); r != 0 {
			t.Errorf("refs(%s) = %d after all releases", id, r)
		}
	}
}

// bounded fails a stuck Acquire quickly instead of hanging the test binary until its timeout.
func bounded(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
