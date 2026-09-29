//go:build integration

package session_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

const code = quiz.Code("K7Q2MX")

// setup resets Redis, loads scripts, and creates a lobby for code.
func setup(t *testing.T) *session.RedisRepository {
	t.Helper()
	env.Reset(t)
	ctx := context.Background()
	scripts := append(quiz.Scripts(), session.Scripts()...)
	if err := redisx.LoadScripts(ctx, env.Redis, scripts...); err != nil {
		t.Fatal(err)
	}
	_, err := quiz.NewRedisRepository(env.Redis).CreateRoom(ctx, quiz.CreateRoomInput{
		Code: code, QuestionSetID: "demo-quick", HostID: "host_1",
		QuestionIDs: []quiz.QuestionID{"dq-01", "dq-02", "dq-03"},
		WindowMs:    10_000, RevealMs: 3_000, LobbyTimeoutMs: 1_800_000, TTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session.NewRedisRepository(env.Redis)
}

func join(t *testing.T, repo *session.RedisRepository, id, name string) session.JoinResult {
	t.Helper()
	res, err := repo.Join(context.Background(), session.JoinInput{Code: code, ParticipantID: quiz.ParticipantID(id), DisplayName: name, TTL: time.Hour})
	if err != nil {
		t.Fatalf("join %s: %v", id, err)
	}
	return res
}

func TestJoin_UnknownQuiz(t *testing.T) {
	repo := setup(t)
	_, err := repo.Join(context.Background(), session.JoinInput{Code: "ZZZZZZ", ParticipantID: "u_1", DisplayName: "Rina", TTL: time.Hour})
	if !errors.Is(err, quiz.ErrUnknownQuiz) {
		t.Errorf("got %v, want ErrUnknownQuiz", err)
	}
}

func TestJoin_ExpiredQuiz(t *testing.T) {
	repo := setup(t)
	env.Redis.HSet(context.Background(), redisx.RoomKey(string(code)), "status", "expired")
	_, err := repo.Join(context.Background(), session.JoinInput{Code: code, ParticipantID: "u_1", DisplayName: "Rina", TTL: time.Hour})
	if !errors.Is(err, quiz.ErrQuizExpired) {
		t.Errorf("got %v, want ErrQuizExpired", err)
	}
}

func TestJoin_FinishedQuizIsReportedNotJoined(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	env.Redis.HSet(ctx, redisx.RoomKey(string(code)), "status", "finished")
	res := join(t, repo, "u_1", "Rina")
	if !res.Finished {
		t.Error("expected Finished")
	}
	if n := env.Redis.HLen(ctx, redisx.RosterKey(string(code))).Val(); n != 0 {
		t.Errorf("joining a finished quiz added %d roster entries", n)
	}
}

func TestJoin_NewParticipant(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	before := env.Redis.Time(ctx).Val().UnixMilli()
	res := join(t, repo, "u_1", "Rina")

	if res.Finished || res.Room.Status != quiz.StatusLobby || res.Room.Code != code {
		t.Errorf("room %+v", res.Room)
	}
	if res.Score != 0 || res.Rank != 1 || res.ParticipantCount != 1 {
		t.Errorf("score=%d rank=%d count=%d, want 0 1 1", res.Score, res.Rank, res.ParticipantCount)
	}
	if want := []leaderboard.Entry{{Rank: 1, ParticipantID: "u_1", DisplayName: "Rina", Score: 0}}; !equalEntries(res.Top, want) {
		t.Errorf("top = %+v, want %+v", res.Top, want)
	}
	if res.ServerTime < before || res.ServerTime > before+5_000 {
		t.Errorf("server time %d not close to %d", res.ServerTime, before)
	}
	k := string(code)
	if name := env.Redis.HGet(ctx, redisx.RosterKey(k), "u_1").Val(); name != "Rina" {
		t.Errorf("roster name %q", name)
	}
	if seen := env.Redis.ZScore(ctx, redisx.OnlineKey(k), "u_1").Val(); int64(seen) != res.ServerTime {
		t.Errorf("online last-seen %v, want %d", seen, res.ServerTime)
	}
	if !env.Redis.SIsMember(ctx, redisx.SchedLeaderboardDirty, k).Val() {
		t.Error("join did not mark the leaderboard dirty (participant count changed)")
	}
	for _, key := range []string{redisx.RosterKey(k), redisx.LeaderboardKey(k), redisx.OnlineKey(k)} {
		if ttl := env.Redis.TTL(ctx, key).Val(); ttl <= 0 || ttl > time.Hour {
			t.Errorf("%s TTL %v", key, ttl)
		}
	}
}

func TestJoin_RejoinKeepsScoreAndUpdatesName(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	env.Redis.ZIncrBy(ctx, redisx.LeaderboardKey(string(code)), 186, "u_1")
	res := join(t, repo, "u_1", "Rina S")
	if res.Score != 186 || res.ParticipantCount != 1 {
		t.Errorf("score=%d count=%d, want 186 and 1", res.Score, res.ParticipantCount)
	}
	if name := env.Redis.HGet(ctx, redisx.RosterKey(string(code)), "u_1").Val(); name != "Rina S" {
		t.Errorf("name = %q, want the new name", name)
	}
}

func TestJoin_SnapshotRanksShareTies(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	for id, name := range map[string]string{"u_a": "Rina", "u_b": "Tomás", "u_c": "Aiko", "u_d": "Budi"} {
		join(t, repo, id, name)
	}
	lb := redisx.LeaderboardKey(string(code))
	env.Redis.ZAdd(ctx, lb, redis.Z{Score: 574, Member: "u_a"}, redis.Z{Score: 551, Member: "u_b"}, redis.Z{Score: 551, Member: "u_c"}, redis.Z{Score: 300, Member: "u_d"})

	res := join(t, repo, "u_e", "Sari")
	if res.Rank != 5 || res.ParticipantCount != 5 {
		t.Errorf("own rank %d count %d, want 5 and 5", res.Rank, res.ParticipantCount)
	}
	var ranks []int
	for _, e := range res.Top {
		ranks = append(ranks, e.Rank)
	}
	if fmt.Sprint(ranks) != "[1 2 2 4 5]" {
		t.Errorf("ranks %v, want [1 2 2 4 5]", ranks)
	}
	if res.Top[0].DisplayName != "Rina" || res.Top[0].Score != 574 {
		t.Errorf("top[0] = %+v", res.Top[0])
	}
}

func TestJoin_TopIsLimitedToTen(t *testing.T) {
	repo := setup(t)
	for i := range 15 {
		join(t, repo, fmt.Sprintf("u_%02d", i), fmt.Sprintf("P%02d", i))
	}
	res := join(t, repo, "u_last", "Last")
	if len(res.Top) != leaderboard.TopN || res.ParticipantCount != 16 {
		t.Errorf("top %d entries, count %d; want %d and 16", len(res.Top), res.ParticipantCount, leaderboard.TopN)
	}
}

// FR-9: many simultaneous joins lose or duplicate nobody.
func TestJoin_ConcurrentJoinsLoseNobody(t *testing.T) {
	repo := setup(t)
	const n, sameTabs = 1000, 20
	var wg sync.WaitGroup
	errs := make(chan error, n+sameTabs) // every goroutine must be able to send without a reader
	for i := range n {
		wg.Go(func() {
			_, err := repo.Join(context.Background(), session.JoinInput{
				Code: code, ParticipantID: quiz.ParticipantID(fmt.Sprintf("u_%04d", i)), DisplayName: "P", TTL: time.Hour,
			})
			errs <- err
		})
	}
	// The same identity joining from 20 tabs at once must still count once.
	for range sameTabs {
		wg.Go(func() {
			_, err := repo.Join(context.Background(), session.JoinInput{Code: code, ParticipantID: "u_same", DisplayName: "Same", TTL: time.Hour})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	k := string(code)
	if r, l := env.Redis.HLen(ctx, redisx.RosterKey(k)).Val(), env.Redis.ZCard(ctx, redisx.LeaderboardKey(k)).Val(); r != n+1 || l != n+1 {
		t.Errorf("roster %d, leaderboard %d; want %d each", r, l, n+1)
	}
}

func equalEntries(a, b []leaderboard.Entry) bool { return fmt.Sprint(a) == fmt.Sprint(b) }
