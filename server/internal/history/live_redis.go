package history

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var (
	//go:embed scripts/claim.lua
	claimSrc string
	//go:embed scripts/flush_ack.lua
	flushAckSrc string
	//go:embed scripts/release.lua
	releaseSrc string

	claimScript    = redisx.NewScript("claim", claimSrc)
	flushAckScript = redisx.NewScript("flush_ack", flushAckSrc)
	releaseScript  = redisx.NewScript("release", releaseSrc)
)

// Scripts lists this module's Lua scripts, for loading at startup.
func Scripts() []*redisx.Script { return []*redisx.Script{claimScript, flushAckScript, releaseScript} }

// RedisLiveStore implements LiveStore on Redis.
type RedisLiveStore struct{ rdb redis.UniversalClient }

// NewRedisLiveStore returns a live store using rdb.
func NewRedisLiveStore(rdb redis.UniversalClient) *RedisLiveStore { return &RedisLiveStore{rdb: rdb} }

// Claim takes up to batch due jobs and hides them for the visibility timeout. Malformed IDs are removed.
func (s *RedisLiveStore) Claim(ctx context.Context, batch int, visibility time.Duration) ([]Job, error) {
	ids, err := claimScript.Run(ctx, s.rdb, []string{redisx.SchedFlush}, batch, visibility.Milliseconds()).StringSlice()
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	jobs := make([]Job, 0, len(ids))
	for _, id := range ids {
		job, err := ParseJob(id)
		if err != nil {
			s.rdb.ZRem(ctx, redisx.SchedFlush, id)
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// ExtendTTL re-applies the TTL to the room's keys (and the question's answers) while a job runs.
func (s *RedisLiveStore) ExtendTTL(ctx context.Context, code quiz.Code, questionID quiz.QuestionID, ttl time.Duration) error {
	c := string(code)
	keys := []string{redisx.RoomKey(c), redisx.QuestionIDsKey(c), redisx.RosterKey(c), redisx.OnlineKey(c), redisx.LeaderboardKey(c)}
	if questionID != "" {
		keys = append(keys, redisx.AnswersKey(c, string(questionID)))
	}
	pipe := s.rdb.Pipeline()
	for _, k := range keys {
		pipe.Expire(ctx, k, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("extend ttl: %w", err)
	}
	return nil
}

// ReadAnswers reads a question's stored answers.
func (s *RedisLiveStore) ReadAnswers(ctx context.Context, code quiz.Code, questionID quiz.QuestionID) ([]StoredAnswer, error) {
	h, err := s.rdb.HGetAll(ctx, redisx.AnswersKey(string(code), string(questionID))).Result()
	if err != nil {
		return nil, fmt.Errorf("read answers: %w", err)
	}
	out := make([]StoredAnswer, 0, len(h))
	for participant, v := range h {
		a, err := ParseStoredAnswer(participant, v)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// AckFlush deletes the flushed answers and completes the job; repeating it is harmless.
func (s *RedisLiveStore) AckFlush(ctx context.Context, job Job) error {
	c := string(job.Code)
	keys := []string{redisx.RoomKey(c), redisx.AnswersKey(c, string(job.QuestionID)), redisx.SchedFlush}
	if err := flushAckScript.Run(ctx, s.rdb, keys, job.ID).Err(); err != nil {
		return fmt.Errorf("flush ack: %w", err)
	}
	return nil
}

// PendingFlushes returns how many answer flushes the room still has pending (0 if the room is gone).
func (s *RedisLiveStore) PendingFlushes(ctx context.Context, code quiz.Code) (int64, error) {
	n, err := s.rdb.HGet(ctx, redisx.RoomKey(string(code)), "pending_flush").Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("pending flushes: %w", err)
	}
	return n, nil
}

// Defer makes the job due again after delay.
func (s *RedisLiveStore) Defer(ctx context.Context, job Job, delay time.Duration) error {
	now, err := s.rdb.Time(ctx).Result()
	if err != nil {
		return fmt.Errorf("defer: %w", err)
	}
	return s.rdb.ZAdd(ctx, redisx.SchedFlush, redis.Z{Score: float64(now.Add(delay).UnixMilli()), Member: job.ID}).Err()
}

// ReadFinal reads the room's status and whole leaderboard with names.
func (s *RedisLiveStore) ReadFinal(ctx context.Context, code quiz.Code) (FinalData, error) {
	c := string(code)
	status, err := s.rdb.HGet(ctx, redisx.RoomKey(c), "status").Result()
	if errors.Is(err, redis.Nil) {
		return FinalData{}, quiz.ErrUnknownQuiz
	}
	if err != nil {
		return FinalData{}, fmt.Errorf("read final: %w", err)
	}
	scores, err := s.rdb.ZRevRangeWithScores(ctx, redisx.LeaderboardKey(c), 0, -1).Result()
	if err != nil {
		return FinalData{}, fmt.Errorf("read final: %w", err)
	}
	names, err := s.rdb.HGetAll(ctx, redisx.RosterKey(c)).Result()
	if err != nil {
		return FinalData{}, fmt.Errorf("read final: %w", err)
	}
	entries := make([]leaderboard.Entry, len(scores))
	for i, z := range scores {
		id := fmt.Sprint(z.Member)
		entries[i] = leaderboard.Entry{ParticipantID: quiz.ParticipantID(id), DisplayName: names[id], Score: int(z.Score)}
	}
	return FinalData{Status: quiz.Status(status), Entries: entries}, nil
}

// Release deletes the room's keys and its remaining schedule entries.
func (s *RedisLiveStore) Release(ctx context.Context, job Job) error {
	c := string(job.Code)
	keys := []string{redisx.RoomKey(c), redisx.QuestionIDsKey(c), redisx.RosterKey(c), redisx.OnlineKey(c),
		redisx.LeaderboardKey(c), redisx.SchedFlush, redisx.SchedTransitions}
	if err := releaseScript.Run(ctx, s.rdb, keys, c, job.ID).Err(); err != nil {
		return fmt.Errorf("release: %w", err)
	}
	return nil
}
