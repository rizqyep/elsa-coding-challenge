package quiz

import (
	"errors"
	"fmt"
	"strconv"
)

// Errors from the room store.
var (
	ErrCodeInUse   = errors.New("quiz code already in use")
	ErrUnknownQuiz = errors.New("unknown quiz")
	ErrQuizExpired = errors.New("quiz expired")
)

// RoomRecord is the full room control record stored in Redis (TRD §4.2).
type RoomRecord struct {
	Room
	Code               Code
	QuestionSetID      QuestionSetID
	QuestionID         QuestionID // current question; empty in the lobby
	LeaderboardVersion int64
	PendingFlush       int64
	CreatedAt          int64
}

// ParseRoomHash converts the room hash (HGETALL) into a record.
func ParseRoomHash(code Code, h map[string]string) (RoomRecord, error) {
	if len(h) == 0 {
		return RoomRecord{}, ErrUnknownQuiz
	}
	p := hashParser{h: h}
	r := RoomRecord{
		Code:               code,
		QuestionSetID:      QuestionSetID(p.str("set_id")),
		QuestionID:         QuestionID(p.str("q_id")),
		LeaderboardVersion: p.int("lb_ver"),
		PendingFlush:       p.int("pending_flush"),
		CreatedAt:          p.int("created_at"),
		Room: Room{
			HostID:           ParticipantID(p.str("host_id")),
			Status:           Status(p.str("status")),
			QuestionIndex:    int(p.int("q_index")),
			QuestionCount:    int(p.int("q_count")),
			WindowMs:         p.int("window_ms"),
			RevealMs:         p.int("reveal_ms"),
			OpenedAt:         p.int("opened_at"),
			Deadline:         p.int("deadline"),
			CloseAt:          p.int("close_at"),
			NextTransitionAt: p.int("next_at"),
			StartRequested:   p.str("start_requested") == "1",
			LobbyExpiresAt:   p.int("lobby_expires_at"),
			StateVersion:     p.int("state_ver"),
		},
	}
	return r, p.err
}

type hashParser struct {
	h   map[string]string
	err error
}

func (p *hashParser) str(k string) string {
	v, ok := p.h[k]
	if !ok && p.err == nil {
		p.err = fmt.Errorf("room hash: missing field %q", k)
	}
	return v
}

func (p *hashParser) int(k string) int64 {
	s := p.str(k)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil && p.err == nil {
		p.err = fmt.Errorf("room hash: field %q: %w", k, err)
	}
	return n
}
