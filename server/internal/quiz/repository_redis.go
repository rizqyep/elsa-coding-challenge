package quiz

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
)

var (
	//go:embed scripts/create_room.lua
	createRoomSrc string
	//go:embed scripts/start.lua
	startSrc string
	//go:embed scripts/next.lua
	nextSrc string
	//go:embed scripts/top.lua
	topSrc string
	//go:embed scripts/transition.lua
	transitionSrc string

	createRoomScript = redisx.NewScript("create_room", createRoomSrc)
	startScript      = redisx.NewScript("start", startSrc)
	transitionScript = redisx.NewScript("transition", nextSrc+"\n"+topSrc+"\n"+transitionSrc)
)

// TopLua is the Lua top_entries function; other modules prepend it to their scripts.
var TopLua = topSrc

// Scripts lists this module's Lua scripts, for loading at startup.
func Scripts() []*redisx.Script {
	return []*redisx.Script{createRoomScript, startScript, transitionScript}
}

// RedisRepository implements Repository on Redis.
type RedisRepository struct{ rdb redis.UniversalClient }

// NewRedisRepository returns a repository using rdb.
func NewRedisRepository(rdb redis.UniversalClient) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

// CreateRoom creates the room in the lobby, schedules its expiry, and returns its Redis creation time (epoch ms).
func (r *RedisRepository) CreateRoom(ctx context.Context, in CreateRoomInput) (int64, error) {
	if len(in.QuestionIDs) == 0 {
		return 0, errors.New("create room: no questions")
	}
	c := string(in.Code)
	args := []any{c, string(in.QuestionSetID), string(in.HostID), in.WindowMs, in.RevealMs, in.LobbyTimeoutMs, int64(in.TTL.Seconds())}
	for _, id := range in.QuestionIDs {
		args = append(args, string(id))
	}
	reply, err := createRoomScript.Run(ctx, r.rdb, []string{redisx.RoomKey(c), redisx.QuestionIDsKey(c), redisx.SchedTransitions}, args...).Slice()
	if err != nil {
		return 0, fmt.Errorf("create_room: %w", err)
	}
	if err := outcome(reply, map[string]error{"code_in_use": ErrCodeInUse}); err != nil {
		return 0, err
	}
	createdAt, ok := reply[1].(int64)
	if !ok {
		return 0, fmt.Errorf("create_room: unexpected reply %v", reply)
	}
	return createdAt, nil
}

// Start records the host's start request (FR-3).
func (r *RedisRepository) Start(ctx context.Context, code Code, caller ParticipantID) error {
	c := string(code)
	reply, err := startScript.Run(ctx, r.rdb, []string{redisx.RoomKey(c), redisx.LeaderboardKey(c), redisx.SchedTransitions}, c, string(caller)).Slice()
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return outcome(reply, map[string]error{
		"unknown_quiz": ErrUnknownQuiz, "not_host": ErrNotHost, "not_in_lobby": ErrNotInLobby, "no_participants": ErrNoParticipants,
	})
}

// Room reads the room control record.
func (r *RedisRepository) Room(ctx context.Context, code Code) (RoomRecord, error) {
	h, err := r.rdb.HGetAll(ctx, redisx.RoomKey(string(code))).Result()
	if err != nil {
		return RoomRecord{}, fmt.Errorf("read room: %w", err)
	}
	return ParseRoomHash(code, h)
}

// Live reads the room record and participant count in one transaction.
func (r *RedisRepository) Live(ctx context.Context, code Code) (LiveRoom, error) {
	c := string(code)
	var h *redis.MapStringStringCmd
	var n *redis.IntCmd
	if _, err := r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		h = p.HGetAll(ctx, redisx.RoomKey(c))
		n = p.ZCard(ctx, redisx.LeaderboardKey(c))
		return nil
	}); err != nil {
		return LiveRoom{}, fmt.Errorf("read live room: %w", err)
	}
	rec, err := ParseRoomHash(code, h.Val())
	if err != nil {
		return LiveRoom{}, err
	}
	return LiveRoom{RoomRecord: rec, Participants: int(n.Val())}, nil
}

// DueTransitions returns rooms whose next transition is due by now (epoch ms).
func (r *RedisRepository) DueTransitions(ctx context.Context, now int64, limit int) ([]Code, error) {
	ids, err := r.rdb.ZRangeByScore(ctx, redisx.SchedTransitions, &redis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now, 10), Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("due transitions: %w", err)
	}
	codes := make([]Code, len(ids))
	for i, id := range ids {
		codes[i] = Code(id)
	}
	return codes, nil
}

// ApplyTransition applies the room's due transition, if any. The script decides and applies
// atomically, so competing workers apply each transition once (NFR-14, TRD §3.3).
func (r *RedisRepository) ApplyTransition(ctx context.Context, code Code) (TransitionResult, error) {
	c := string(code)
	keys := []string{redisx.RoomKey(c), redisx.QuestionIDsKey(c), redisx.SchedTransitions, redisx.SchedFlush,
		redisx.SchedLeaderboardDirty, redisx.LeaderboardKey(c), redisx.RosterKey(c), redisx.RoomChannel(c)}
	reply, err := transitionScript.Run(ctx, r.rdb, keys, c, TopN).Slice()
	if err != nil {
		return TransitionResult{}, fmt.Errorf("transition: %w", err)
	}
	res := TransitionResult{Outcome: Outcome(fmt.Sprint(reply[0]))}
	if res.Outcome == Applied && len(reply) == 3 {
		res.Status = Status(fmt.Sprint(reply[1]))
		res.StateVersion, _ = reply[2].(int64)
	}
	switch res.Outcome {
	case Applied, NotDue, Terminal, Stale:
		return res, nil
	}
	return TransitionResult{}, fmt.Errorf("transition: unexpected reply %v", reply)
}

// outcome maps a script's {status, reason} reply to an error.
func outcome(reply []any, reasons map[string]error) error {
	status, _ := reply[0].(string)
	if status == "ok" {
		return nil
	}
	if len(reply) > 1 {
		if reason, _ := reply[1].(string); reasons[reason] != nil {
			return reasons[reason]
		}
	}
	return fmt.Errorf("unexpected script reply %v", reply)
}
