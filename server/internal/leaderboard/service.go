package leaderboard

import (
	"context"
	"errors"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// Service reads leaderboard pages from wherever the quiz currently lives.
type Service struct {
	live  LivePages
	final FinalPages
}

// NewService returns a service over the live and archived leaderboards.
func NewService(live LivePages, final FinalPages) *Service { return &Service{live: live, final: final} }

// Page reads live standings while the room exists, final results once it's released (FR-27, FR-13).
// The final job commits results before releasing the room, so this order never misses them.
func (s *Service) Page(ctx context.Context, code quiz.Code, offset, limit int) (Page, error) {
	p, err := s.live.LivePage(ctx, code, offset, limit)
	if !errors.Is(err, quiz.ErrUnknownQuiz) {
		return p, err
	}
	return s.final.FinalPage(ctx, code, offset, limit)
}
