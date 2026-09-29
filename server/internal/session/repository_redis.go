package session

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var (
	//go:embed scripts/join.lua
	joinSrc string
	//go:embed scripts/view.lua
	viewSrc string
	//go:embed scripts/standings.lua
	standingsSrc string
	//go:embed scripts/presence.lua
	presenceSrc string

	joinScript      = redisx.NewScript("join", joinSrc)
	viewScript      = redisx.NewScript("view", viewSrc)
	standingsScript = redisx.NewScript("standings", standingsSrc)
	presenceScript  = redisx.NewScript("presence", presenceSrc)
)

// standingsChunk caps the participants per standings call, so one call never blocks Redis for long.
const standingsChunk = 500

// Scripts lists this module's Lua scripts, for loading at startup.
func Scripts() []*redisx.Script {
	return []*redisx.Script{joinScript, viewScript, standingsScript, presenceScript}
}

// RedisRepository implements Repository on Redis.
type RedisRepository struct{ rdb redis.UniversalClient }

// NewRedisRepository returns a repository using rdb.
func NewRedisRepository(rdb redis.UniversalClient) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

// Join adds or restores the participant and returns their snapshot data.
func (r *RedisRepository) Join(ctx context.Context, in JoinInput) (JoinResult, error) {
	c := string(in.Code)
	keys := []string{redisx.RoomKey(c), redisx.RosterKey(c), redisx.LeaderboardKey(c), redisx.OnlineKey(c), redisx.SchedLeaderboardDirty, redisx.RoomChannel(c)}
	reply, err := joinScript.Run(ctx, r.rdb, keys, c, string(in.ParticipantID), in.DisplayName, leaderboard.TopN, int64(in.TTL.Seconds()), in.ConnID).Slice()
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
	v, err := parseView(code, reply[1], reply[4], reply[5], reply[6], reply[7])
	if err != nil {
		return JoinResult{}, fmt.Errorf("join: %w", err)
	}
	return JoinResult{
		Room:             v.Room,
		Score:            int(reply[2].(int64)),
		Rank:             int(reply[3].(int64)) + 1,
		ParticipantCount: v.ParticipantCount,
		Top:              v.Top,
		ServerTime:       v.ServerTime,
	}, nil
}

// View reads a room's shared snapshot part in one round trip, without changing anything.
func (r *RedisRepository) View(ctx context.Context, code quiz.Code) (RoomView, error) {
	c := string(code)
	keys := []string{redisx.RoomKey(c), redisx.LeaderboardKey(c), redisx.RosterKey(c)}
	reply, err := viewScript.Run(ctx, r.rdb, keys, leaderboard.TopN).Slice()
	if err != nil {
		return RoomView{}, fmt.Errorf("view: %w", err)
	}
	if reply[0] == "missing" {
		return RoomView{}, quiz.ErrUnknownQuiz
	}
	if len(reply) != 6 {
		return RoomView{}, fmt.Errorf("view: reply has %d parts, want 6", len(reply))
	}
	v, err := parseView(code, reply[1], reply[2], reply[3], reply[4], reply[5])
	if err != nil {
		return RoomView{}, fmt.Errorf("view: %w", err)
	}
	return v, nil
}

// parseView reads the room hash, participant count, top (id, score)…, their names, and the server time.
func parseView(code quiz.Code, hash, count, top, names, now any) (RoomView, error) {
	room, err := quiz.ParseRoomHash(code, pairs(hash))
	if err != nil {
		return RoomView{}, err
	}
	flat, _ := top.([]any)
	nameList, _ := names.([]any)
	entries := make([]leaderboard.Entry, 0, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		score, err := strconv.ParseFloat(fmt.Sprint(flat[i+1]), 64)
		if err != nil {
			return RoomView{}, fmt.Errorf("score: %w", err)
		}
		name, _ := nameList[i/2].(string)
		entries = append(entries, leaderboard.Entry{ParticipantID: quiz.ParticipantID(fmt.Sprint(flat[i])), DisplayName: name, Score: int(score)})
	}
	leaderboard.Rank(entries)
	n, _ := count.(int64)
	ts, _ := now.(int64)
	return RoomView{Room: room, ParticipantCount: int(n), Top: entries, ServerTime: ts}, nil
}

// Standings reads each participant's score, rank, and answer to questionID (if set), and the participant count, in one round trip.
func (r *RedisRepository) Standings(ctx context.Context, code quiz.Code, questionID quiz.QuestionID, ids []quiz.ParticipantID) (Standings, error) {
	out := Standings{ByID: make(map[quiz.ParticipantID]Standing, len(ids))}
	if len(ids) == 0 {
		return out, nil
	}
	res, err := r.standings(ctx, code, questionID, ids)
	if redisx.IsNoScript(err) { // read-only, so running it again is always safe
		if err = standingsScript.Load(ctx, r.rdb); err == nil {
			res, err = r.standings(ctx, code, questionID, ids)
		}
	}
	if err != nil {
		return Standings{}, fmt.Errorf("standings: %w", err)
	}
	out.ParticipantCount = int(res.count.Val())
	var given []any
	if res.answers != nil {
		given = res.answers.Val()
	}
	i := 0
	for _, cmd := range res.chunks {
		vals, err := cmd.Int64Slice()
		if err != nil {
			return Standings{}, fmt.Errorf("standings: %w", err)
		}
		for j := 0; j+1 < len(vals); j, i = j+2, i+1 {
			st := Standing{}
			if vals[j] >= 0 {
				st = Standing{Found: true, Score: int(vals[j]), Rank: int(vals[j+1]) + 1}
			}
			if i < len(given) {
				if raw, ok := given[i].(string); ok {
					if a, ok := parseAnswer(raw); ok {
						st.Answer = &a
					}
				}
			}
			out.ByID[ids[i]] = st
		}
	}
	return out, nil
}

type standingsReply struct {
	chunks  []*redis.Cmd
	answers *redis.SliceCmd
	count   *redis.IntCmd
}

func (r *RedisRepository) standings(ctx context.Context, code quiz.Code, questionID quiz.QuestionID, ids []quiz.ParticipantID) (standingsReply, error) {
	c := string(code)
	pipe := r.rdb.Pipeline()
	var res standingsReply
	for start := 0; start < len(ids); start += standingsChunk {
		part := ids[start:min(start+standingsChunk, len(ids))]
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = string(id)
		}
		res.chunks = append(res.chunks, standingsScript.EvalSha(ctx, pipe, []string{redisx.LeaderboardKey(c)}, args...))
	}
	res.count = pipe.ZCard(ctx, redisx.LeaderboardKey(c))
	if questionID != "" {
		fields := make([]string, len(ids))
		for i, id := range ids {
			fields[i] = string(id)
		}
		res.answers = pipe.HMGet(ctx, redisx.AnswersKey(c, string(questionID)), fields...)
	}
	_, err := pipe.Exec(ctx)
	return res, err
}

// parseAnswer reads a stored answer "option|correct|points|received_ms" (TRD §4.2).
func parseAnswer(raw string) (Answer, bool) {
	parts := strings.Split(raw, "|")
	if len(parts) != 4 {
		return Answer{}, false
	}
	points, err := strconv.Atoi(parts[2])
	if err != nil {
		return Answer{}, false
	}
	return Answer{OptionID: quiz.OptionID(parts[0]), Correct: parts[1] == "1", Points: points}, true
}

func pairs(v any) map[string]string {
	flat, _ := v.([]any)
	m := make(map[string]string, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		m[fmt.Sprint(flat[i])] = fmt.Sprint(flat[i+1])
	}
	return m
}

// RefreshPresence marks participants online at Redis TIME, in chunks so no call blocks Redis for long (TRD §7.7).
func (r *RedisRepository) RefreshPresence(ctx context.Context, code quiz.Code, ids []quiz.ParticipantID) error {
	c := string(code)
	keys := []string{redisx.RoomKey(c), redisx.OnlineKey(c)}
	for start := 0; start < len(ids); start += standingsChunk {
		chunk := ids[start:min(start+standingsChunk, len(ids))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = string(id)
		}
		if err := presenceScript.Run(ctx, r.rdb, keys, args...).Err(); err != nil {
			return fmt.Errorf("presence: %w", err)
		}
	}
	return nil
}
