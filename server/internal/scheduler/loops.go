package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// Parallelism per loop and per-item timeouts (TRD §8.1, §9.2).
const (
	transitionParallel  = 16
	leaderboardParallel = 16
	flushParallel       = 4
	scriptTimeout       = 500 * time.Millisecond
	jobTimeout          = 15 * time.Second // finalise transaction; below the claim's visibility timeout
)

// Transitions claims and applies due quiz transitions (quiz.RedisRepository).
type Transitions interface {
	DueTransitions(ctx context.Context, now int64, limit int) ([]quiz.Code, error)
	ApplyTransition(ctx context.Context, code quiz.Code) (quiz.TransitionResult, error)
}

// Leaderboards pops rooms with changed scores and publishes their snapshots (leaderboard.RedisRepository).
type Leaderboards interface {
	PopDirty(ctx context.Context, n int) ([]quiz.Code, error)
	PublishSnapshot(ctx context.Context, code quiz.Code) (int64, error)
}

// Jobs claims persistence jobs (history.RedisLiveStore).
type Jobs interface {
	Claim(ctx context.Context, batch int, visibility time.Duration) ([]history.Job, error)
}

// Processor runs one persistence job (history.Service).
type Processor interface {
	Process(ctx context.Context, job history.Job) error
}

// Deps are what the loops work on.
type Deps struct {
	Transitions  Transitions
	Leaderboards Leaderboards
	Jobs         Jobs
	Processor    Processor
	NowMs        func() int64 // Redis-aligned, so claims agree with the scripts' due checks
}

// Settings come from the worker's configuration (TRD §2.4).
type Settings struct {
	TransitionPoll  time.Duration
	LeaderboardTick time.Duration
	FlushPoll       time.Duration
	ClaimBatch      int
	FlushVisibility time.Duration
}

// Loops returns the worker's three loops (TRD §8.1). Extra workers only compete for the same work:
// the transition script's due check, SPOP, and the claim's visibility timeout keep it from being duplicated.
func Loops(d Deps, s Settings) []Loop {
	return []Loop{
		{Name: "transitions", Interval: s.TransitionPoll, Pass: func(ctx context.Context) error {
			codes, err := d.Transitions.DueTransitions(ctx, d.NowMs(), s.ClaimBatch)
			if err != nil {
				return err
			}
			return forEach(ctx, codes, transitionParallel, func(ctx context.Context, code quiz.Code) error {
				ctx, cancel := context.WithTimeout(ctx, scriptTimeout)
				defer cancel()
				if _, err := d.Transitions.ApplyTransition(ctx, code); err != nil {
					return fmt.Errorf("transition %s: %w", code, err)
				}
				return nil
			})
		}},
		{Name: "leaderboard", Interval: s.LeaderboardTick, Pass: func(ctx context.Context) error {
			codes, err := d.Leaderboards.PopDirty(ctx, s.ClaimBatch)
			if err != nil {
				return err
			}
			return forEach(ctx, codes, leaderboardParallel, func(ctx context.Context, code quiz.Code) error {
				ctx, cancel := context.WithTimeout(ctx, scriptTimeout)
				defer cancel()
				if _, err := d.Leaderboards.PublishSnapshot(ctx, code); err != nil && !errors.Is(err, leaderboard.ErrNoRoom) {
					return fmt.Errorf("leaderboard %s: %w", code, err)
				}
				return nil
			})
		}},
		{Name: "flush", Interval: s.FlushPoll, Pass: func(ctx context.Context) error {
			jobs, err := d.Jobs.Claim(ctx, s.ClaimBatch, s.FlushVisibility)
			if err != nil {
				return err
			}
			return forEach(ctx, jobs, flushParallel, func(ctx context.Context, j history.Job) error {
				ctx, cancel := context.WithTimeout(ctx, jobTimeout)
				defer cancel()
				if err := d.Processor.Process(ctx, j); err != nil {
					return fmt.Errorf("job %s: %w", j.ID, err)
				}
				return nil
			})
		}},
	}
}
