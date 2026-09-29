//go:build integration

package session_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

// roundTrips counts commands and pipelines sent to Redis.
type roundTrips struct{ n atomic.Int64 }

func (h *roundTrips) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *roundTrips) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error { h.n.Add(1); return next(ctx, cmd) }
}
func (h *roundTrips) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error { h.n.Add(1); return next(ctx, cmds) }
}

func countingRepo(t *testing.T) (*session.RedisRepository, *roundTrips) {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: env.Redis.Options().Addr})
	t.Cleanup(func() { _ = c.Close() })
	h := &roundTrips{}
	c.AddHook(h)
	if err := c.Ping(context.Background()).Err(); err != nil { // open the connection; its handshake isn't counted
		t.Fatal(err)
	}
	h.n.Store(0)
	return session.NewRedisRepository(c), h
}

func TestView_UnknownQuiz(t *testing.T) {
	repo := setup(t)
	if _, err := repo.View(context.Background(), "ZZZZZZ"); !errors.Is(err, quiz.ErrUnknownQuiz) {
		t.Errorf("got %v, want ErrUnknownQuiz", err)
	}
}

func TestView_MatchesTheJoinSnapshotAndChangesNothing(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	join(t, repo, "u_2", "Budi")
	last := join(t, repo, "u_3", "Sari")
	env.Redis.ZIncrBy(ctx, redisx.LeaderboardKey(string(code)), 150, "u_2")
	env.Redis.Del(ctx, redisx.SchedLeaderboardDirty)
	online := env.Redis.ZRangeWithScores(ctx, redisx.OnlineKey(string(code)), 0, -1).Val()

	v, err := repo.View(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if v.Room.Code != code || v.Room.Status != quiz.StatusLobby || v.Room.QuestionSetID != "demo-quick" || v.Room.StateVersion != last.Room.StateVersion {
		t.Errorf("room %+v", v.Room)
	}
	if v.ParticipantCount != 3 || len(v.Top) != 3 || v.Top[0].ParticipantID != "u_2" || v.Top[0].DisplayName != "Budi" ||
		v.Top[0].Score != 150 || v.Top[0].Rank != 1 || v.Top[1].Rank != 2 || v.Top[2].Rank != 2 {
		t.Errorf("leaderboard count=%d top=%+v", v.ParticipantCount, v.Top)
	}
	if v.ServerTime < last.ServerTime {
		t.Errorf("server time %d before the join's %d", v.ServerTime, last.ServerTime)
	}
	if env.Redis.Exists(ctx, redisx.SchedLeaderboardDirty).Val() != 0 {
		t.Error("view marked the leaderboard dirty; it must be read-only")
	}
	if got := env.Redis.ZRangeWithScores(ctx, redisx.OnlineKey(string(code)), 0, -1).Val(); fmt.Sprint(got) != fmt.Sprint(online) {
		t.Errorf("view touched presence: %v → %v", online, got)
	}
}

func TestStandings_ScoresAndSharedRanks(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	lb := redisx.LeaderboardKey(string(code))
	env.Redis.ZAdd(ctx, lb, redis.Z{Score: 300, Member: "u_1"}, redis.Z{Score: 300, Member: "u_2"},
		redis.Z{Score: 100, Member: "u_3"}, redis.Z{Score: 0, Member: "u_4"})

	got, err := repo.Standings(ctx, code, "", []quiz.ParticipantID{"u_1", "u_2", "u_3", "u_4", "u_gone"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[quiz.ParticipantID]session.Standing{
		"u_1":    {Found: true, Score: 300, Rank: 1},
		"u_2":    {Found: true, Score: 300, Rank: 1},
		"u_3":    {Found: true, Score: 100, Rank: 3},
		"u_4":    {Found: true, Score: 0, Rank: 4},
		"u_gone": {},
	}
	if fmt.Sprint(got.ByID) != fmt.Sprint(want) || got.ParticipantCount != 4 {
		t.Errorf("got  %v (count %d)\nwant %v (count 4)", got.ByID, got.ParticipantCount, want)
	}
}

func TestStandings_IncludeOwnAnswerToTheGivenQuestion(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	join(t, repo, "u_2", "Budi")
	env.Redis.HSet(ctx, redisx.AnswersKey(string(code), "dq-01"), "u_1", "dq-01-b|1|186|1790000000000")

	got, err := repo.Standings(ctx, code, "dq-01", []quiz.ParticipantID{"u_1", "u_2"})
	if err != nil {
		t.Fatal(err)
	}
	a := got.ByID["u_1"].Answer
	if a == nil || a.OptionID != "dq-01-b" || !a.Correct || a.Points != 186 {
		t.Errorf("u_1 answer %+v", a)
	}
	if got.ByID["u_2"].Answer != nil {
		t.Errorf("u_2 has not answered, got %+v", got.ByID["u_2"].Answer)
	}
}

func TestStandings_ThousandsOfParticipantsInOneRoundTrip(t *testing.T) {
	setup(t)
	ctx := context.Background()
	const n = 2500
	zs := make([]redis.Z, n)
	ids := make([]quiz.ParticipantID, n)
	for i := range n {
		id := fmt.Sprintf("u_%04d", i)
		ids[i] = quiz.ParticipantID(id)
		zs[i] = redis.Z{Score: float64(i / 2), Member: id} // pairs share a score
	}
	env.Redis.ZAdd(ctx, redisx.LeaderboardKey(string(code)), zs...)

	repo, trips := countingRepo(t)
	got, err := repo.Standings(ctx, code, "dq-01", ids)
	if err != nil {
		t.Fatal(err)
	}
	if trips.n.Load() != 1 {
		t.Errorf("%d round trips for %d participants, want 1", trips.n.Load(), n)
	}
	for i, id := range ids {
		score := i / 2
		want := (n/2-1-score)*2 + 1 // two participants per higher score, plus one
		if s := got.ByID[id]; !s.Found || s.Score != score || s.Rank != want {
			t.Fatalf("%s: got %+v, want score %d rank %d", id, s, score, want)
		}
	}
}

func TestStandings_NoParticipantsNoRedisCall(t *testing.T) {
	setup(t)
	repo, trips := countingRepo(t)
	got, err := repo.Standings(context.Background(), code, "dq-01", nil)
	if err != nil || len(got.ByID) != 0 || trips.n.Load() != 0 {
		t.Errorf("got %v, %v after %d round trips", got, err, trips.n.Load())
	}
}

func TestViewAndStandings_SurviveAnEmptiedScriptCache(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	if err := env.Redis.ScriptFlush(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.View(ctx, code); err != nil {
		t.Errorf("view: %v", err)
	}
	if err := env.Redis.ScriptFlush(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Standings(ctx, code, "dq-01", []quiz.ParticipantID{"u_1"}); err != nil || !got.ByID["u_1"].Found {
		t.Errorf("standings: %v %v", got, err)
	}
}
