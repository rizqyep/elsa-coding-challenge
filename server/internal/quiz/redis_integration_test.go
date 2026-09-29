//go:build integration

package quiz_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

func newRepo(t *testing.T) *quiz.RedisRepository {
	t.Helper()
	env.Reset(t)
	if err := redisx.LoadScripts(context.Background(), env.Redis, quiz.Scripts()...); err != nil {
		t.Fatal(err)
	}
	return quiz.NewRedisRepository(env.Redis)
}

func redisNow(t *testing.T) int64 {
	t.Helper()
	ts, err := env.Redis.Time(context.Background()).Result()
	if err != nil {
		t.Fatal(err)
	}
	return ts.UnixMilli()
}

var input = quiz.CreateRoomInput{
	Code: "K7Q2MX", QuestionSetID: "demo-quick", HostID: "host_1",
	QuestionIDs: []quiz.QuestionID{"dq-01", "dq-02", "dq-03"},
	WindowMs:    10_000, RevealMs: 3_000, LobbyTimeoutMs: 1_800_000, TTL: time.Hour,
}

func TestCreateRoom_WritesTheRoom(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	before := redisNow(t)
	if err := repo.CreateRoom(ctx, input); err != nil {
		t.Fatal(err)
	}
	rec, err := repo.Room(ctx, input.Code)
	if err != nil {
		t.Fatal(err)
	}
	if rec.CreatedAt < before || rec.CreatedAt > before+5_000 {
		t.Errorf("created_at %d not close to Redis time %d", rec.CreatedAt, before)
	}
	want := quiz.RoomRecord{
		Code: input.Code, QuestionSetID: input.QuestionSetID, CreatedAt: rec.CreatedAt,
		Room: quiz.Room{
			HostID: "host_1", Status: quiz.StatusLobby, QuestionIndex: -1, QuestionCount: 3,
			WindowMs: 10_000, RevealMs: 3_000,
			NextTransitionAt: rec.CreatedAt + 1_800_000, LobbyExpiresAt: rec.CreatedAt + 1_800_000, StateVersion: 1,
		},
	}
	if rec != want {
		t.Errorf("room =\n  %+v\nwant\n  %+v", rec, want)
	}
	ids, _ := env.Redis.LRange(ctx, redisx.QuestionIDsKey("K7Q2MX"), 0, -1).Result()
	if len(ids) != 3 || ids[0] != "dq-01" || ids[2] != "dq-03" {
		t.Errorf("question ids = %v", ids)
	}
	score, err := env.Redis.ZScore(ctx, redisx.SchedTransitions, "K7Q2MX").Result()
	if err != nil || int64(score) != rec.LobbyExpiresAt {
		t.Errorf("lobby expiry scheduled at %v (%v), want %d", score, err, rec.LobbyExpiresAt)
	}
	for _, k := range []string{redisx.RoomKey("K7Q2MX"), redisx.QuestionIDsKey("K7Q2MX")} {
		if ttl := env.Redis.TTL(ctx, k).Val(); ttl <= 0 || ttl > time.Hour {
			t.Errorf("%s TTL = %v, want within (0, 1h]", k, ttl)
		}
	}
}

func TestCreateRoom_CodeInUse(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	if err := repo.CreateRoom(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateRoom(ctx, input); !errors.Is(err, quiz.ErrCodeInUse) {
		t.Errorf("got %v, want ErrCodeInUse", err)
	}
}

func TestStart_UnknownQuiz(t *testing.T) {
	repo := newRepo(t)
	if err := repo.Start(context.Background(), "ZZZZZZ", "host_1"); !errors.Is(err, quiz.ErrUnknownQuiz) {
		t.Errorf("got %v, want ErrUnknownQuiz", err)
	}
}

// The start script must agree with quiz.Start (task-08) on every case, including check order.
func TestStart_MatchesDomainRules(t *testing.T) {
	cases := []struct {
		name         string
		status       quiz.Status
		requested    bool
		participants int
		caller       quiz.ParticipantID
	}{
		{"host starts a lobby with participants", quiz.StatusLobby, false, 2, "host_1"},
		{"not the host", quiz.StatusLobby, false, 2, "u_1"},
		{"not the host and not in the lobby (host check first)", quiz.StatusQuestionOpen, true, 2, "u_1"},
		{"no participants", quiz.StatusLobby, false, 0, "host_1"},
		{"already running", quiz.StatusQuestionOpen, true, 2, "host_1"},
		{"start requested twice", quiz.StatusLobby, true, 2, "host_1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			ctx := context.Background()
			if err := repo.CreateRoom(ctx, input); err != nil {
				t.Fatal(err)
			}
			key := redisx.RoomKey(string(input.Code))
			env.Redis.HSet(ctx, key, "status", string(tc.status), "start_requested", boolField(tc.requested))
			for i := range tc.participants {
				env.Redis.ZAdd(ctx, redisx.LeaderboardKey(string(input.Code)), redisZ("u_"+strconv.Itoa(i)))
			}
			before, _ := repo.Room(ctx, input.Code)

			now := redisNow(t)
			wantRoom, wantErr := quiz.Start(before.Room, tc.caller, tc.participants, now)
			gotErr := repo.Start(ctx, input.Code, tc.caller)
			if !errors.Is(gotErr, wantErr) {
				t.Fatalf("script error %v, domain error %v", gotErr, wantErr)
			}
			after, _ := repo.Room(ctx, input.Code)
			gotNext := after.NextTransitionAt
			// next_at comes from Redis TIME, so compare it loosely and the rest exactly.
			if d := gotNext - wantRoom.NextTransitionAt; d < 0 || d > 5_000 {
				t.Errorf("next_at %d, domain %d", gotNext, wantRoom.NextTransitionAt)
			}
			after.NextTransitionAt, wantRoom.NextTransitionAt = 0, 0
			if after.Room != wantRoom {
				t.Errorf("room after script =\n  %+v\ndomain\n  %+v", after.Room, wantRoom)
			}
			if wantErr == nil && !before.StartRequested {
				if score := env.Redis.ZScore(ctx, redisx.SchedTransitions, string(input.Code)).Val(); int64(score) != gotNext {
					t.Errorf("transition scheduled at %d, want %d (now)", int64(score), gotNext)
				}
			}
		})
	}
}

func boolField(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func redisZ(member string) redis.Z { return redis.Z{Score: 0, Member: member} }
