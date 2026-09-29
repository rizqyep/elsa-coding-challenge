package leaderboard

import (
	"context"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

//go:generate go tool -modfile=../../tools/go.mod mockgen -source=pages.go -destination=mocks/pages.go -package=mocks

// Page is one page of a leaderboard (openapi.yaml LeaderboardPage).
type Page struct {
	Code             quiz.Code
	Status           quiz.Status
	ParticipantCount int
	Offset           int
	Limit            int
	Entries          []Entry
}

// LivePages reads pages of a running quiz's leaderboard; quiz.ErrUnknownQuiz once the room is gone.
type LivePages interface {
	LivePage(ctx context.Context, code quiz.Code, offset, limit int) (Page, error)
}

// FinalPages reads pages of an archived quiz's results; quiz.ErrUnknownQuiz if never created.
type FinalPages interface {
	FinalPage(ctx context.Context, code quiz.Code, offset, limit int) (Page, error)
}
