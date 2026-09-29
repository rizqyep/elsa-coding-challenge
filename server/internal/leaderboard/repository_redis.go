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

//go:embed scripts/leaderboard.lua
var leaderboardSrc string

var leaderboardScript = redisx.NewScript("leaderboard", quiz.TopLua+"\n"+leaderboardSrc)

// ErrNoRoom is returned when the room no longer exists.
var ErrNoRoom = errors.New("room not found")

// Scripts lists this module's Lua scripts, for loading at startup.
func Scripts() []*redisx.Script { return []*redisx.Script{leaderboardScript} }

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
