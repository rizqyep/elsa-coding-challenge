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
	ConnID        string // published in a kick so other gateways close older connections (FR-12)
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

// RoomView is the part of a snapshot every connection in a room shares.
type RoomView struct {
	Room             quiz.RoomRecord
	ParticipantCount int
	Top              []leaderboard.Entry
	ServerTime       int64
}

// Standings are participants' own parts of a snapshot or rank message.
type Standings struct {
	ParticipantCount int
	ByID             map[quiz.ParticipantID]Standing
}

// Standing is one participant's own part of a snapshot or rank message. Found is false for an unknown participant.
type Standing struct {
	Found  bool
	Score  int
	Rank   int
	Answer *Answer // accepted answer to the requested question, if any
}

// Answer is a stored answer as the participant sees it.
type Answer struct {
	OptionID quiz.OptionID
	Correct  bool
	Points   int
}

// LeaveOutcome is what a leave did to the participant.
type LeaveOutcome string

// Leave outcomes.
const (
	LeftRemoved LeaveOutcome = "removed" // lobby: off the roster and leaderboard
	LeftOffline LeaveOutcome = "offline" // quiz started: score kept, no longer online
	LeftGone    LeaveOutcome = "gone"    // room already released
)
