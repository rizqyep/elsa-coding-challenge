package scoring

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Service submits answers (FR-16 to FR-22).
type Service struct {
	repo    Repository
	timeout time.Duration
}

// NewService returns a Service; timeout bounds each call (TRD §9.2).
func NewService(repo Repository, timeout time.Duration) *Service {
	return &Service{repo: repo, timeout: timeout}
}

// SubmitAnswer records an answer once. It never retries (TRD §9.3); infrastructure failures
// become ErrServerBusy so the client resends with the same request ID.
func (s *Service) SubmitAnswer(ctx context.Context, in AnswerInput) (AnswerResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	res, err := s.repo.RecordAnswer(ctx, in)
	switch {
	case err == nil:
		return res, nil
	case errors.Is(err, ErrQuestionClosed), errors.Is(err, ErrWrongQuestion), errors.Is(err, ErrNotJoined):
		return AnswerResult{}, err
	case errors.Is(err, ErrUnexpectedReply):
		return AnswerResult{}, fmt.Errorf("%w: %w", ErrInternal, err)
	default:
		return AnswerResult{}, fmt.Errorf("%w: %w", ErrServerBusy, err)
	}
}
