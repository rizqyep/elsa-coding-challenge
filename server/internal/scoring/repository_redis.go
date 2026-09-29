package scoring

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var (
	//go:embed scripts/points.lua
	pointsSrc string
	//go:embed scripts/answer.lua
	answerSrc string

	answerScript = redisx.NewScript("answer", pointsSrc+"\n"+answerSrc)
)

// Scripts lists this module's Lua scripts, for loading at startup.
func Scripts() []*redisx.Script { return []*redisx.Script{answerScript} }

// RedisOptions configures RedisRepository.
type RedisOptions struct {
	OnlineWindow time.Duration // presence TTL for the early-close count
	TTL          time.Duration
	WaitReplicas int // 0 disables WAIT (non-functional §3.2)
	WaitTimeout  time.Duration
}

// RedisRepository implements Repository on Redis.
type RedisRepository struct {
	rdb  redis.UniversalClient
	opts RedisOptions
}

// NewRedisRepository returns a repository using rdb.
func NewRedisRepository(rdb redis.UniversalClient, opts RedisOptions) *RedisRepository {
	return &RedisRepository{rdb: rdb, opts: opts}
}

// RecordAnswer runs the answer script; with WAIT enabled it is pipelined with WAIT on one connection (TRD §4.4).
func (r *RedisRepository) RecordAnswer(ctx context.Context, in AnswerInput) (AnswerResult, error) {
	c, q := string(in.Code), string(in.QuestionID)
	keys := []string{redisx.RoomKey(c), redisx.AnswersKey(c, q), redisx.LeaderboardKey(c), redisx.RosterKey(c),
		redisx.OnlineKey(c), redisx.SchedLeaderboardDirty, redisx.SchedTransitions, redisx.RoomChannel(c)}
	correct := 0
	if in.Correct {
		correct = 1
	}
	args := []any{c, string(in.ParticipantID), q, string(in.OptionID), correct, r.opts.OnlineWindow.Milliseconds(), int64(r.opts.TTL.Seconds())}

	if r.opts.WaitReplicas <= 0 {
		reply, err := answerScript.Run(ctx, r.rdb, keys, args...).Slice()
		if err != nil {
			return AnswerResult{}, fmt.Errorf("answer: %w", err)
		}
		return parseAnswer(reply)
	}
	cmd, wait, err := r.answerWithWait(ctx, keys, args)
	if redisx.IsNoScript(err) { // the script didn't run, so running it again can't double-count
		if err = answerScript.Load(ctx, r.rdb); err == nil {
			cmd, wait, err = r.answerWithWait(ctx, keys, args)
		}
	}
	if err != nil {
		return AnswerResult{}, fmt.Errorf("answer: %w", err)
	}
	reply, err := cmd.Slice()
	if err != nil {
		return AnswerResult{}, fmt.Errorf("answer: %w", err)
	}
	res, err := parseAnswer(reply)
	acked, _ := wait.Int64()
	res.Replicated = acked >= int64(r.opts.WaitReplicas)
	return res, err
}

func (r *RedisRepository) answerWithWait(ctx context.Context, keys []string, args []any) (*redis.Cmd, *redis.Cmd, error) {
	pipe := r.rdb.Pipeline()
	cmd := answerScript.EvalSha(ctx, pipe, keys, args...)
	wait := pipe.Do(ctx, "wait", r.opts.WaitReplicas, r.opts.WaitTimeout.Milliseconds()) // Pipeliner has no Wait method
	_, err := pipe.Exec(ctx)
	if err == nil {
		err = cmd.Err()
	}
	return cmd, wait, err
}

// parseAnswer reads {status, option, correct, points, total, received_at} or {'rejected', reason}.
func parseAnswer(reply []any) (AnswerResult, error) {
	status, _ := reply[0].(string)
	switch status {
	case "rejected":
		switch reply[1] {
		case "question_closed":
			return AnswerResult{}, ErrQuestionClosed
		case "wrong_question":
			return AnswerResult{}, ErrWrongQuestion
		case "not_joined":
			return AnswerResult{}, ErrNotJoined
		}
	case string(Accepted), string(Duplicate):
		if len(reply) == 6 {
			option, ok1 := reply[1].(string)
			correct, ok2 := reply[2].(int64)
			points, ok3 := reply[3].(int64)
			total, ok4 := reply[4].(int64)
			at, ok5 := reply[5].(int64)
			if ok1 && ok2 && ok3 && ok4 && ok5 {
				return AnswerResult{Status: AnswerStatus(status), OptionID: quiz.OptionID(option), Correct: correct == 1,
					Points: int(points), Total: int(total), ReceivedAt: at}, nil
			}
		}
	}
	return AnswerResult{}, fmt.Errorf("%w: %v", ErrUnexpectedReply, reply)
}
