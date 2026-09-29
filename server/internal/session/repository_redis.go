package session

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

//go:embed scripts/join.lua
var joinSrc string

var joinScript = redisx.NewScript("join", joinSrc)

// Scripts lists this module's Lua scripts, for loading at startup.
func Scripts() []*redisx.Script { return []*redisx.Script{joinScript} }

// RedisRepository implements Repository on Redis.
type RedisRepository struct{ rdb redis.UniversalClient }

// NewRedisRepository returns a repository using rdb.
func NewRedisRepository(rdb redis.UniversalClient) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

// Join adds or restores the participant and returns their snapshot data.
func (r *RedisRepository) Join(ctx context.Context, in JoinInput) (JoinResult, error) {
	c := string(in.Code)
	keys := []string{redisx.RoomKey(c), redisx.RosterKey(c), redisx.LeaderboardKey(c), redisx.OnlineKey(c), redisx.SchedLeaderboardDirty}
	reply, err := joinScript.Run(ctx, r.rdb, keys, c, string(in.ParticipantID), in.DisplayName, leaderboard.TopN, int64(in.TTL.Seconds())).Slice()
	if err != nil {
		return JoinResult{}, fmt.Errorf("join: %w", err)
	}
	switch reply[0] {
	case "finished":
		return JoinResult{Finished: true}, nil
	case "rejected":
		switch reply[1] {
		case "unknown_quiz":
			return JoinResult{}, quiz.ErrUnknownQuiz
		case "quiz_expired":
			return JoinResult{}, quiz.ErrQuizExpired
		}
	case "ok":
		return parseJoin(in.Code, reply)
	}
	return JoinResult{}, fmt.Errorf("join: unexpected reply %v", reply)
}

// parseJoin reads {'ok', room hash, score, higher, count, top (id, score)…, names, now}.
func parseJoin(code quiz.Code, reply []any) (JoinResult, error) {
	if len(reply) != 8 {
		return JoinResult{}, fmt.Errorf("join: reply has %d parts, want 8", len(reply))
	}
	room, err := quiz.ParseRoomHash(code, pairs(reply[1]))
	if err != nil {
		return JoinResult{}, err
	}
	top, _ := reply[5].([]any)
	names, _ := reply[6].([]any)
	entries := make([]leaderboard.Entry, 0, len(top)/2)
	for i := 0; i+1 < len(top); i += 2 {
		score, err := strconv.ParseFloat(fmt.Sprint(top[i+1]), 64)
		if err != nil {
			return JoinResult{}, fmt.Errorf("join: score: %w", err)
		}
		name, _ := names[i/2].(string)
		entries = append(entries, leaderboard.Entry{ParticipantID: quiz.ParticipantID(fmt.Sprint(top[i])), DisplayName: name, Score: int(score)})
	}
	leaderboard.Rank(entries)
	return JoinResult{
		Room:             room,
		Score:            int(reply[2].(int64)),
		Rank:             int(reply[3].(int64)) + 1,
		ParticipantCount: int(reply[4].(int64)),
		Top:              entries,
		ServerTime:       reply[7].(int64),
	}, nil
}

func pairs(v any) map[string]string {
	flat, _ := v.([]any)
	m := make(map[string]string, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		m[fmt.Sprint(flat[i])] = fmt.Sprint(flat[i+1])
	}
	return m
}
