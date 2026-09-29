package quiz

import (
	"context"
	"time"
)

//go:generate go tool -modfile=../../tools/go.mod mockgen -source=repository.go -destination=mocks/repository.go -package=mocks

// Repository stores rooms in the live store.
type Repository interface {
	CreateRoom(ctx context.Context, in CreateRoomInput) (int64, error)
	Start(ctx context.Context, code Code, caller ParticipantID) error
	Room(ctx context.Context, code Code) (RoomRecord, error)
	Live(ctx context.Context, code Code) (LiveRoom, error)
}

// LiveRoom is a room with its participant count, read together.
type LiveRoom struct {
	RoomRecord
	Participants int
}

// CreateRoomInput describes a new room in the lobby.
type CreateRoomInput struct {
	Code           Code
	QuestionSetID  QuestionSetID
	HostID         ParticipantID
	QuestionIDs    []QuestionID
	WindowMs       int64
	RevealMs       int64
	LobbyTimeoutMs int64
	TTL            time.Duration
}
