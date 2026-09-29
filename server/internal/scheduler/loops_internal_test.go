package scheduler

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

type fakeTransitions struct {
	mu        sync.Mutex
	due       []quiz.Code
	claimErr  error
	gotNow    int64
	gotLimit  int
	applied   []quiz.Code
	deadlines []bool
}

func (f *fakeTransitions) DueTransitions(_ context.Context, now int64, limit int) ([]quiz.Code, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotNow, f.gotLimit = now, limit
	return f.due, f.claimErr
}

func (f *fakeTransitions) ApplyTransition(ctx context.Context, code quiz.Code) (quiz.TransitionResult, error) {
	_, has := ctx.Deadline()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, code)
	f.deadlines = append(f.deadlines, has)
	if code == "BADBAD" {
		return quiz.TransitionResult{}, errors.New("script failed")
	}
	return quiz.TransitionResult{Outcome: quiz.Applied}, nil
}

type fakeBoards struct {
	mu        sync.Mutex
	dirty     []quiz.Code
	gotN      int
	published []quiz.Code
}

func (f *fakeBoards) PopDirty(_ context.Context, n int) ([]quiz.Code, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotN = n
	return f.dirty, nil
}

func (f *fakeBoards) PublishSnapshot(_ context.Context, code quiz.Code) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, code)
	if code == "GONE22" {
		return 0, leaderboard.ErrNoRoom
	}
	return 1, nil
}

type fakeJobs struct {
	mu            sync.Mutex
	jobs          []history.Job
	gotBatch      int
	gotVisibility time.Duration
	processed     []string
	deadlines     []bool
}

func (f *fakeJobs) Claim(_ context.Context, batch int, visibility time.Duration) ([]history.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotBatch, f.gotVisibility = batch, visibility
	return f.jobs, nil
}

func (f *fakeJobs) Process(ctx context.Context, j history.Job) error {
	_, has := ctx.Deadline()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.processed = append(f.processed, j.ID)
	f.deadlines = append(f.deadlines, has)
	return nil
}

func settings() Settings {
	return Settings{TransitionPoll: 100 * time.Millisecond, LeaderboardTick: 200 * time.Millisecond, FlushPoll: time.Second,
		ClaimBatch: 7, FlushVisibility: 30 * time.Second}
}

func loop(t *testing.T, loops []Loop, name string) Loop {
	t.Helper()
	for _, l := range loops {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no %s loop", name)
	return Loop{}
}

func sorted[T ~string](xs []T) []T { out := slices.Clone(xs); slices.Sort(out); return out }

func TestLoops_Intervals(t *testing.T) {
	ls := Loops(Deps{}, settings())
	want := map[string]time.Duration{"transitions": 100 * time.Millisecond, "leaderboard": 200 * time.Millisecond, "flush": time.Second}
	if len(ls) != 3 {
		t.Fatalf("%d loops, want 3", len(ls))
	}
	for name, iv := range want {
		if got := loop(t, ls, name).Interval; got != iv {
			t.Errorf("%s every %v, want %v", name, got, iv)
		}
	}
}

func TestTransitionLoop_AppliesEveryDueRoomAtRedisTime(t *testing.T) {
	tr := &fakeTransitions{due: []quiz.Code{"AAAAAA", "BADBAD", "CCCCCC"}}
	l := loop(t, Loops(Deps{Transitions: tr, NowMs: func() int64 { return 42_000 }}, settings()), "transitions")
	err := l.Pass(context.Background())
	if err == nil {
		t.Error("a failed transition was not reported")
	}
	if tr.gotNow != 42_000 || tr.gotLimit != 7 {
		t.Errorf("claimed with now=%d limit=%d, want the Redis-aligned 42000 and the batch 7", tr.gotNow, tr.gotLimit)
	}
	if got := sorted(tr.applied); !slices.Equal(got, []quiz.Code{"AAAAAA", "BADBAD", "CCCCCC"}) {
		t.Errorf("applied %v; one failure must not skip the others", got)
	}
	if slices.Contains(tr.deadlines, false) {
		t.Error("a transition ran without a deadline (TRD §9.2)")
	}

	tr.claimErr = errors.New("redis down")
	if err := l.Pass(context.Background()); err == nil {
		t.Error("a failed claim was not reported")
	}
}

func TestLeaderboardLoop_PublishesEveryDirtyRoom(t *testing.T) {
	lb := &fakeBoards{dirty: []quiz.Code{"AAAAAA", "GONE22", "BBBBBB"}}
	l := loop(t, Loops(Deps{Leaderboards: lb}, settings()), "leaderboard")
	if err := l.Pass(context.Background()); err != nil {
		t.Errorf("got %v; a room released since it was marked dirty is not an error", err)
	}
	if lb.gotN != 7 || len(lb.published) != 3 {
		t.Errorf("popped %d, published %v", lb.gotN, lb.published)
	}
}

func TestFlushLoop_ProcessesEveryClaimedJob(t *testing.T) {
	jobs := &fakeJobs{jobs: []history.Job{{ID: "q|AAAAAA|q1"}, {ID: "q|AAAAAA|q2"}, {ID: "final|AAAAAA"}}}
	l := loop(t, Loops(Deps{Jobs: jobs, Processor: jobs}, settings()), "flush")
	if err := l.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if jobs.gotBatch != 7 || jobs.gotVisibility != 30*time.Second {
		t.Errorf("claimed batch %d visibility %v", jobs.gotBatch, jobs.gotVisibility)
	}
	if len(jobs.processed) != 3 || slices.Contains(jobs.deadlines, false) {
		t.Errorf("processed %v, deadlines %v; every job runs once with a deadline", jobs.processed, jobs.deadlines)
	}
}
