package session

import (
	"context"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// Repository joins participants in the live store.
type Repository interface {
	Join(ctx context.Context, in JoinInput) (JoinResult, error)
}

// JoinInput is one participant joining (or rejoining) a quiz.
type JoinInput struct {
	Code          quiz.Code
	ParticipantID quiz.ParticipantID
	DisplayName   string
	TTL           time.Duration
}

// JoinResult is what the snapshot needs (FR-11). Finished means the quiz is over and nothing was joined.
type JoinResult struct {
	Finished         bool
	Room             quiz.RoomRecord
	Score            int
	Rank             int
	ParticipantCount int
	Top              []leaderboard.Entry
	ServerTime       int64
}
