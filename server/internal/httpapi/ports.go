package httpapi

import (
	"context"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

//go:generate go tool -modfile=../../tools/go.mod mockgen -source=ports.go -destination=mocks/ports.go -package=mocks

// Quizzes is what the handlers need from the quiz module.
type Quizzes interface {
	QuestionSets(ctx context.Context) ([]quiz.QuestionSetSummary, error)
	Create(ctx context.Context, in quiz.CreateInput) (quiz.Summary, error)
	Start(ctx context.Context, code quiz.Code, caller quiz.ParticipantID) error
	Get(ctx context.Context, code quiz.Code) (quiz.Summary, error)
}

// Leaderboards is what the handlers need from the leaderboard module.
type Leaderboards interface {
	Page(ctx context.Context, code quiz.Code, offset, limit int) (leaderboard.Page, error)
}
