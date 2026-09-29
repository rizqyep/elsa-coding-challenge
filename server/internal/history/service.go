package history

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// Options configures Service.
type Options struct {
	TTL             time.Duration // re-applied to a room's keys before flushing (NFR-18)
	FinalRetryDelay time.Duration // how long a final job waits while flushes are pending
	OnMismatch      func(quiz.Code, []Mismatch)
}

// Service runs persistence jobs (TRD §4.5). Every step is safe to repeat.
type Service struct {
	live  LiveStore
	store Store
	opts  Options
}

// NewService returns a Service.
func NewService(live LiveStore, store Store, opts Options) *Service {
	return &Service{live: live, store: store, opts: opts}
}

// Process runs one claimed job. On error the job stays claimed and becomes due again after its timeout.
func (s *Service) Process(ctx context.Context, job Job) error {
	switch job.Kind {
	case FlushAnswers:
		return s.flush(ctx, job)
	case Finalize:
		return s.finalize(ctx, job)
	}
	return fmt.Errorf("%w: %q", ErrMalformedJob, job.ID)
}

// flush saves a closed question's answers, then releases them from Redis (FR-33, FR-34).
func (s *Service) flush(ctx context.Context, job Job) error {
	if err := s.live.ExtendTTL(ctx, job.Code, job.QuestionID, s.opts.TTL); err != nil {
		return err
	}
	answers, err := s.live.ReadAnswers(ctx, job.Code, job.QuestionID)
	if err != nil {
		return err
	}
	if len(answers) > 0 {
		if err := s.store.SaveAnswers(ctx, job.Code, job.QuestionID, answers); err != nil {
			return fmt.Errorf("save answers: %w", err)
		}
	}
	return s.live.AckFlush(ctx, job)
}

// finalize writes final results once every flush is done, then releases the room (FR-27a, FR-35).
func (s *Service) finalize(ctx context.Context, job Job) error {
	pending, err := s.live.PendingFlushes(ctx, job.Code)
	if err != nil {
		return err
	}
	if pending > 0 {
		return s.live.Defer(ctx, job, s.opts.FinalRetryDelay)
	}
	final, err := s.live.ReadFinal(ctx, job.Code)
	if errors.Is(err, quiz.ErrUnknownQuiz) {
		return s.live.Release(ctx, job)
	}
	if err != nil {
		return err
	}
	mismatches, err := s.store.Finalize(ctx, FinalizeInput{Code: job.Code, Status: final.Status, Results: RankResults(final.Entries)})
	if err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	if len(mismatches) > 0 && s.opts.OnMismatch != nil {
		s.opts.OnMismatch(job.Code, mismatches)
	}
	return s.live.Release(ctx, job)
}
