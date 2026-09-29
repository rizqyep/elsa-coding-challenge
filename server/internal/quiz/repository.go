package quiz

import (
	"context"
	"time"
)

// Repository stores rooms in the live store.
type Repository interface {
	CreateRoom(ctx context.Context, in CreateRoomInput) error
	Start(ctx context.Context, code Code, caller ParticipantID) error
	Room(ctx context.Context, code Code) (RoomRecord, error)
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
