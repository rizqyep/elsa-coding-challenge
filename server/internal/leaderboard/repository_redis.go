package leaderboard

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var (
	//go:embed scripts/leaderboard.lua
	leaderboardSrc string
	//go:embed scripts/page.lua
	pageSrc string

	leaderboardScript = redisx.NewScript("leaderboard", quiz.TopLua+"\n"+leaderboardSrc)
	pageScript        = redisx.NewScript("page", pageSrc)
)

// ErrNoRoom is returned when the room no longer exists.
var ErrNoRoom = errors.New("room not found")

// Scripts lists this module's Lua scripts, for loading at startup.
func Scripts() []*redisx.Script { return []*redisx.Script{leaderboardScript, pageScript} }

// RedisRepository publishes leaderboard snapshots from Redis.
type RedisRepository struct{ rdb redis.UniversalClient }

// NewRedisRepository returns a repository using rdb.
func NewRedisRepository(rdb redis.UniversalClient) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

// PopDirty takes up to n rooms whose leaderboard changed; each room goes to one caller only.
func (r *RedisRepository) PopDirty(ctx context.Context, n int) ([]quiz.Code, error) {
	ids, err := r.rdb.SPopN(ctx, redisx.SchedLeaderboardDirty, int64(n)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pop dirty: %w", err)
	}
	codes := make([]quiz.Code, len(ids))
	for i, id := range ids {
		codes[i] = quiz.Code(id)
	}
	return codes, nil
}

// PublishSnapshot publishes the room's top entries with a new version and returns that version.
func (r *RedisRepository) PublishSnapshot(ctx context.Context, code quiz.Code) (int64, error) {
	c := string(code)
	keys := []string{redisx.RoomKey(c), redisx.LeaderboardKey(c), redisx.RosterKey(c), redisx.RoomChannel(c)}
	reply, err := leaderboardScript.Run(ctx, r.rdb, keys, c, TopN).Slice()
	if err != nil {
		return 0, fmt.Errorf("leaderboard: %w", err)
	}
	switch reply[0] {
	case "stale":
		return 0, ErrNoRoom
	case "ok":
		if v, ok := reply[1].(int64); ok {
			return v, nil
		}
	}
	return 0, fmt.Errorf("leaderboard: unexpected reply %v", reply)
}

// LivePage reads one page of the live leaderboard in a single script, so it is consistent.
func (r *RedisRepository) LivePage(ctx context.Context, code quiz.Code, offset, limit int) (Page, error) {
	c := string(code)
	reply, err := pageScript.Run(ctx, r.rdb, []string{redisx.RoomKey(c), redisx.LeaderboardKey(c), redisx.RosterKey(c)}, offset, limit).Slice()
	if err != nil {
		return Page{}, fmt.Errorf("page: %w", err)
	}
	if reply[0] == "rejected" {
		return Page{}, quiz.ErrUnknownQuiz
	}
	status, _ := reply[1].(string)
	count, _ := reply[2].(int64)
	rows, _ := reply[3].([]any)
	p := Page{Code: code, Status: quiz.Status(status), ParticipantCount: int(count), Offset: offset, Limit: limit, Entries: make([]Entry, len(rows))}
	for i, row := range rows {
		f, ok := row.([]any)
		if !ok || len(f) != 4 {
			return Page{}, fmt.Errorf("page: unexpected entry %v", row)
		}
		id, _ := f[0].(string)
		name, _ := f[1].(string)
		score, _ := f[2].(int64)
		rank, _ := f[3].(int64)
		p.Entries[i] = Entry{Rank: int(rank), ParticipantID: quiz.ParticipantID(id), DisplayName: name, Score: int(score)}
	}
	return p, nil
}
