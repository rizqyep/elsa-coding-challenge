package realtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// Internal event types on room:{C} (TRD §4.3).
const (
	eventState       = "state"
	eventLeaderboard = "lb"
	eventFinished    = "finished"
	eventKick        = "kick"
)

// roomEvent is an internal event published by a Lua script or a gateway (TRD §4.3).
type roomEvent struct {
	T   string             `json:"t"`
	V   num                `json:"v"`
	S   quiz.Status        `json:"s"`
	I   num                `json:"i"`
	N   num                `json:"n"`
	Q   quiz.QuestionID    `json:"q"`
	O   num                `json:"o"`
	D   num                `json:"d"`
	C   num                `json:"c"`
	X   num                `json:"x"`
	Top topEntries         `json:"top"`
	P   quiz.ParticipantID `json:"p"`
	K   string             `json:"k"`
}

func decodeEvent(b []byte) (roomEvent, error) {
	var ev roomEvent
	if err := json.Unmarshal(b, &ev); err != nil {
		return roomEvent{}, fmt.Errorf("room event: %w", err)
	}
	return ev, nil
}

// num is an integer that cjson may have written in floating-point form.
type num int64

func (n *num) UnmarshalJSON(b []byte) error {
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return err
	}
	*n = num(math.Round(f))
	return nil
}

// topEntries is [[id, name, score], …], ranked on decode; cjson writes an empty list as {}.
type topEntries []leaderboard.Entry

func (t *topEntries) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), []byte("{}")) {
		*t = topEntries{}
		return nil
	}
	var rows [][3]json.RawMessage
	if err := json.Unmarshal(b, &rows); err != nil {
		return err
	}
	out := make(topEntries, len(rows))
	for i, row := range rows {
		var id, name string
		var score num
		if err := json.Unmarshal(row[0], &id); err != nil {
			return err
		}
		_ = json.Unmarshal(row[1], &name) // a missing roster name is written as false
		if err := json.Unmarshal(row[2], &score); err != nil {
			return err
		}
		out[i] = leaderboard.Entry{ParticipantID: quiz.ParticipantID(id), DisplayName: name, Score: int(score)}
	}
	leaderboard.Rank(out)
	*t = out
	return nil
}

func wireEntries(entries []leaderboard.Entry) []protocol.LeaderboardEntry {
	out := make([]protocol.LeaderboardEntry, len(entries))
	for i, e := range entries {
		out[i] = protocol.LeaderboardEntry{Rank: e.Rank, ParticipantID: string(e.ParticipantID), DisplayName: e.DisplayName, Score: e.Score}
	}
	return out
}
