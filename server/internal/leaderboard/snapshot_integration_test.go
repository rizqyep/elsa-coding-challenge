//go:build integration

package leaderboard_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

const code = quiz.Code("K7Q2MX")

func setup(t *testing.T, scores map[string]int) *leaderboard.RedisRepository {
	t.Helper()
	env.Reset(t)
	ctx := context.Background()
	all := append(quiz.Scripts(), leaderboard.Scripts()...)
	if err := redisx.LoadScripts(ctx, env.Redis, all...); err != nil {
		t.Fatal(err)
	}
	if err := quiz.NewRedisRepository(env.Redis).CreateRoom(ctx, quiz.CreateRoomInput{
		Code: code, QuestionSetID: "demo-quick", HostID: "host_1", QuestionIDs: []quiz.QuestionID{"dq-01"},
		WindowMs: 10_000, RevealMs: 3_000, LobbyTimeoutMs: 60_000, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	k := string(code)
	for id, s := range scores {
		env.Redis.HSet(ctx, redisx.RosterKey(k), id, "P-"+id)
		env.Redis.ZAdd(ctx, redisx.LeaderboardKey(k), redis.Z{Score: float64(s), Member: id})
	}
	return leaderboard.NewRedisRepository(env.Redis)
}

type lbEvent struct {
	T   string  `json:"t"`
	V   int64   `json:"v"`
	N   int     `json:"n"`
	Top [][]any `json:"top"`
}

func publishAndReceive(t *testing.T, repo *leaderboard.RedisRepository) (int64, lbEvent) {
	t.Helper()
	ctx := context.Background()
	sub := env.Redis.Subscribe(ctx, redisx.RoomChannel(string(code)))
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	ver, err := repo.PublishSnapshot(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	msg, err := sub.ReceiveMessage(cctx)
	if err != nil {
		t.Fatal(err)
	}
	var ev lbEvent
	if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
		t.Fatalf("%q: %v", msg.Payload, err)
	}
	return ver, ev
}

func TestPublishSnapshot_TopWithNamesAndVersion(t *testing.T) {
	repo := setup(t, map[string]int{"u_a": 574, "u_b": 551, "u_c": 300})
	ver, ev := publishAndReceive(t, repo)
	if ver != 1 || ev.T != "lb" || ev.V != 1 || ev.N != 3 || len(ev.Top) != 3 {
		t.Fatalf("version %d, event %+v", ver, ev)
	}
	if fmt.Sprint(ev.Top[0]) != "[u_a P-u_a 574]" || fmt.Sprint(ev.Top[2]) != "[u_c P-u_c 300]" {
		t.Errorf("top %v", ev.Top)
	}
	if ver2, _ := publishAndReceive(t, repo); ver2 != 2 {
		t.Errorf("second snapshot version %d, want 2 (monotonic)", ver2)
	}
}

func TestPublishSnapshot_LimitedToTopTen(t *testing.T) {
	scores := map[string]int{}
	for i := range 15 {
		scores[fmt.Sprintf("u_%02d", i)] = i * 10
	}
	repo := setup(t, scores)
	_, ev := publishAndReceive(t, repo)
	if len(ev.Top) != leaderboard.TopN || ev.N != 15 {
		t.Errorf("top %d, count %d; want %d and 15", len(ev.Top), ev.N, leaderboard.TopN)
	}
}

func TestPublishSnapshot_UnknownQuizPublishesNothing(t *testing.T) {
	repo := setup(t, nil)
	if _, err := repo.PublishSnapshot(context.Background(), "ZZZZZZ"); !errors.Is(err, leaderboard.ErrNoRoom) {
		t.Errorf("got %v, want ErrNoRoom", err)
	}
}

// Each dirty room is popped by exactly one worker per tick (TRD §4.4 leaderboard).
func TestPopDirty_EachRoomOnce(t *testing.T) {
	repo := setup(t, nil)
	ctx := context.Background()
	var want []string
	for i := range 200 {
		c := fmt.Sprintf("R%05d", i)
		want = append(want, c)
		env.Redis.SAdd(ctx, redisx.SchedLeaderboardDirty, c)
	}
	var mu sync.Mutex
	var got []string
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for {
				codes, err := repo.PopDirty(ctx, 17)
				if err != nil {
					t.Error(err)
					return
				}
				if len(codes) == 0 {
					return
				}
				mu.Lock()
				for _, c := range codes {
					got = append(got, string(c))
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	sort.Strings(got)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("popped %d codes (with duplicates or gaps), want each of %d exactly once", len(got), len(want))
	}
}
