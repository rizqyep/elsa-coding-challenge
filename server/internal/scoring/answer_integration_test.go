//go:build integration

package scoring_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

const code = quiz.Code("K7Q2MX")

// The Lua scoring function must give the same points as Go for every shared vector (TRD §3.5).
func TestPointsLua_SharedVectors(t *testing.T) {
	env.Reset(t)
	src := scoring.PointsLua + "\nreturn points(ARGV[1] == '1', tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4]))"
	for _, c := range loadPointsCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			got, err := env.Redis.Eval(context.Background(), src, nil,
				boolArg(c.Correct), c.OpenedAt, c.Deadline, c.ReceivedAt).Int()
			if err != nil {
				t.Fatal(err)
			}
			if got != c.Points {
				t.Errorf("Lua points = %d, want %d", got, c.Points)
			}
		})
	}
}

// room sets up an open question with the given participants joined (and online).
type room struct {
	repo                        *scoring.RedisRepository
	openedAt, deadline, closeAt int64
}

func openRoom(t *testing.T, participants ...string) room {
	t.Helper()
	env.Reset(t)
	ctx := context.Background()
	all := append(append(quiz.Scripts(), session.Scripts()...), scoring.Scripts()...)
	if err := redisx.LoadScripts(ctx, env.Redis, all...); err != nil {
		t.Fatal(err)
	}
	if _, err := quiz.NewRedisRepository(env.Redis).CreateRoom(ctx, quiz.CreateRoomInput{
		Code: code, QuestionSetID: "demo-quick", HostID: "host_1",
		QuestionIDs: []quiz.QuestionID{"dq-01", "dq-02"}, WindowMs: 15_000, RevealMs: 5_000, LobbyTimeoutMs: 60_000, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	sess := session.NewRedisRepository(env.Redis)
	for _, p := range participants {
		if _, err := sess.Join(ctx, session.JoinInput{Code: code, ParticipantID: quiz.ParticipantID(p), DisplayName: p, TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
	now := env.Redis.Time(ctx).Val().UnixMilli()
	r := room{openedAt: now - 1_000, deadline: now + 14_000, closeAt: now + 14_000}
	env.Redis.HSet(ctx, redisx.RoomKey(string(code)), "status", "question_open", "q_index", 0, "q_id", "dq-01",
		"opened_at", r.openedAt, "deadline", r.deadline, "close_at", r.closeAt, "next_at", r.closeAt, "state_ver", 2)
	r.repo = scoring.NewRedisRepository(env.Redis, scoring.RedisOptions{OnlineWindow: 30 * time.Second, TTL: time.Hour})
	return r
}

func answer(p string, option quiz.OptionID, correct bool) scoring.AnswerInput {
	return scoring.AnswerInput{Code: code, ParticipantID: quiz.ParticipantID(p), QuestionID: "dq-01", OptionID: option, Correct: correct}
}

func TestRecordAnswer_Accepted(t *testing.T) {
	r := openRoom(t, "u_1", "u_2")
	ctx := context.Background()
	res, err := r.repo.RecordAnswer(ctx, answer("u_1", "dq-01-b", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != scoring.Accepted || !res.Correct || res.OptionID != "dq-01-b" {
		t.Errorf("result %+v", res)
	}
	// Points must match the Go formula for the receive time the script used (TRD §3.5).
	if want := scoring.Points(true, r.openedAt, r.deadline, res.ReceivedAt); res.Points != want {
		t.Errorf("points %d, Go Points gives %d for receivedAt %d", res.Points, want, res.ReceivedAt)
	}
	if res.Total != res.Points {
		t.Errorf("total %d, want %d", res.Total, res.Points)
	}
	k := string(code)
	if got := env.Redis.ZScore(ctx, redisx.LeaderboardKey(k), "u_1").Val(); int(got) != res.Points {
		t.Errorf("leaderboard %v, want %d", got, res.Points)
	}
	want := "dq-01-b|1|" + strconv.Itoa(res.Points) + "|" + strconv.FormatInt(res.ReceivedAt, 10)
	if got := env.Redis.HGet(ctx, redisx.AnswersKey(k, "dq-01"), "u_1").Val(); got != want {
		t.Errorf("stored answer %q, want %q", got, want)
	}
	if ttl := env.Redis.TTL(ctx, redisx.AnswersKey(k, "dq-01")).Val(); ttl <= 0 {
		t.Errorf("answers key has no TTL (%v)", ttl)
	}
	if !env.Redis.SIsMember(ctx, redisx.SchedLeaderboardDirty, k).Val() {
		t.Error("leaderboard not marked dirty")
	}
}

func TestRecordAnswer_WrongAnswerScoresZero(t *testing.T) {
	r := openRoom(t, "u_1", "u_2")
	res, err := r.repo.RecordAnswer(context.Background(), answer("u_1", "dq-01-a", false))
	if err != nil || res.Status != scoring.Accepted || res.Correct || res.Points != 0 || res.Total != 0 {
		t.Errorf("result %+v, err %v", res, err)
	}
}

func TestRecordAnswer_Rejections(t *testing.T) {
	cases := []struct {
		name  string
		setup func(ctx context.Context, k string, now int64)
		in    scoring.AnswerInput
		want  error
	}{
		{"quiz still in the lobby", func(ctx context.Context, k string, _ int64) {
			env.Redis.HSet(ctx, redisx.RoomKey(k), "status", "lobby")
		}, answer("u_1", "dq-01-b", true), scoring.ErrQuestionClosed},
		{"question closed (reveal)", func(ctx context.Context, k string, _ int64) {
			env.Redis.HSet(ctx, redisx.RoomKey(k), "status", "question_closed")
		}, answer("u_1", "dq-01-b", true), scoring.ErrQuestionClosed},
		{"answer for another question", nil,
			scoring.AnswerInput{Code: code, ParticipantID: "u_1", QuestionID: "dq-02", OptionID: "dq-02-a", Correct: true}, scoring.ErrWrongQuestion},
		{"received after close_at (late)", func(ctx context.Context, k string, now int64) {
			env.Redis.HSet(ctx, redisx.RoomKey(k), "close_at", now)
		}, answer("u_1", "dq-01-b", true), scoring.ErrQuestionClosed},
		{"not joined", nil, answer("u_stranger", "dq-01-b", true), scoring.ErrNotJoined},
		// Acceptance order (TRD §3.4): the first failing check decides.
		{"wrong question and not joined → wrong question", nil,
			scoring.AnswerInput{Code: code, ParticipantID: "u_stranger", QuestionID: "dq-02", OptionID: "x", Correct: true}, scoring.ErrWrongQuestion},
		{"closed status and wrong question → question closed", func(ctx context.Context, k string, _ int64) {
			env.Redis.HSet(ctx, redisx.RoomKey(k), "status", "question_closed")
		}, scoring.AnswerInput{Code: code, ParticipantID: "u_1", QuestionID: "dq-02", OptionID: "x", Correct: true}, scoring.ErrQuestionClosed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := openRoom(t, "u_1", "u_2")
			ctx := context.Background()
			k := string(code)
			if tc.setup != nil {
				tc.setup(ctx, k, env.Redis.Time(ctx).Val().UnixMilli())
			}
			_, err := r.repo.RecordAnswer(ctx, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if n := env.Redis.HLen(ctx, redisx.AnswersKey(k, string(tc.in.QuestionID))).Val(); n != 0 {
				t.Errorf("a rejected answer was stored (%d entries)", n)
			}
			if s := env.Redis.ZScore(ctx, redisx.LeaderboardKey(k), string(tc.in.ParticipantID)).Val(); s != 0 {
				t.Errorf("a rejected answer changed the score to %v", s)
			}
		})
	}
}

func TestRecordAnswer_DuplicateReturnsOriginal(t *testing.T) {
	r := openRoom(t, "u_1", "u_2")
	ctx := context.Background()
	first, err := r.repo.RecordAnswer(ctx, answer("u_1", "dq-01-b", true))
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.repo.RecordAnswer(ctx, answer("u_1", "dq-01-c", false)) // different option: still the original
	if err != nil {
		t.Fatal(err)
	}
	want := first
	want.Status = scoring.Duplicate
	if again != want {
		t.Errorf("duplicate result %+v, want the original %+v", again, want)
	}
	if got := env.Redis.ZScore(ctx, redisx.LeaderboardKey(string(code)), "u_1").Val(); int(got) != first.Points {
		t.Errorf("score %v after a duplicate, want %d", got, first.Points)
	}
}

// NFR-12: concurrent submissions by one participant score exactly once.
func TestRecordAnswer_ConcurrentSubmitsScoreOnce(t *testing.T) {
	r := openRoom(t, "u_1", "u_2")
	const n = 100
	results := make(chan scoring.AnswerResult, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			res, err := r.repo.RecordAnswer(context.Background(), answer("u_1", "dq-01-b", true))
			results <- res
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	accepted, points := 0, -1
	for res := range results {
		if res.Status == scoring.Accepted {
			accepted++
		}
		if points != -1 && res.Points != points {
			t.Fatalf("results disagree on points: %d vs %d", res.Points, points)
		}
		points = res.Points
	}
	if accepted != 1 {
		t.Errorf("%d accepted, want exactly 1", accepted)
	}
	if got := env.Redis.ZScore(context.Background(), redisx.LeaderboardKey(string(code)), "u_1").Val(); int(got) != points {
		t.Errorf("score %v, want %d (counted once)", got, points)
	}
}

func TestRecordAnswer_EarlyCloseWhenEveryoneOnlineAnswered(t *testing.T) {
	r := openRoom(t, "u_1", "u_2")
	ctx := context.Background()
	k := string(code)
	sub := env.Redis.Subscribe(ctx, redisx.RoomChannel(k))
	t.Cleanup(func() { _ = sub.Close() })
	if _, err := sub.Receive(ctx); err != nil { // subscription confirmation
		t.Fatal(err)
	}

	if _, err := r.repo.RecordAnswer(ctx, answer("u_1", "dq-01-b", true)); err != nil {
		t.Fatal(err)
	}
	if got := roomField(t, "close_at"); got != r.closeAt {
		t.Fatalf("closed early after 1 of 2 answers (close_at %d)", got)
	}
	last, err := r.repo.RecordAnswer(ctx, answer("u_2", "dq-01-a", false))
	if err != nil {
		t.Fatal(err)
	}
	if got := roomField(t, "close_at"); got != last.ReceivedAt {
		t.Errorf("close_at %d, want the last answer's time %d", got, last.ReceivedAt)
	}
	if got := roomField(t, "next_at"); got != last.ReceivedAt {
		t.Errorf("next_at %d, want %d", got, last.ReceivedAt)
	}
	if got := roomField(t, "deadline"); got != r.deadline {
		t.Errorf("early close moved the deadline to %d (want %d unchanged)", got, r.deadline)
	}
	if s := env.Redis.ZScore(ctx, redisx.SchedTransitions, k).Val(); int64(s) != last.ReceivedAt {
		t.Errorf("close not scheduled for now: %v", s)
	}
	msg, err := sub.ReceiveMessage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		T string `json:"t"`
		S string `json:"s"`
		Q string `json:"q"`
		C int64  `json:"c"`
		D int64  `json:"d"`
		X int64  `json:"x"`
	}
	if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
		t.Fatalf("event %q: %v", msg.Payload, err)
	}
	if ev.T != "state" || ev.S != "question_open" || ev.Q != "dq-01" || ev.C != last.ReceivedAt || ev.D != r.deadline || ev.X != last.ReceivedAt {
		t.Errorf("event %+v", ev)
	}
}

func TestRecordAnswer_EarlyCloseIgnoresOfflineParticipants(t *testing.T) {
	r := openRoom(t, "u_1", "u_2", "u_gone")
	ctx := context.Background()
	stale := env.Redis.Time(ctx).Val().UnixMilli() - 60_000
	env.Redis.ZAdd(ctx, redisx.OnlineKey(string(code)), redis.Z{Score: float64(stale), Member: "u_gone"})
	if _, err := r.repo.RecordAnswer(ctx, answer("u_1", "dq-01-b", true)); err != nil {
		t.Fatal(err)
	}
	last, err := r.repo.RecordAnswer(ctx, answer("u_2", "dq-01-b", true))
	if err != nil {
		t.Fatal(err)
	}
	if got := roomField(t, "close_at"); got != last.ReceivedAt {
		t.Errorf("close_at %d: an offline participant held the question open", got)
	}
}

// With a replica, WAIT confirms the answer reached it before the reply (non-functional §3.2).
func TestRecordAnswer_WaitsForReplica(t *testing.T) {
	openRoom(t, "u_1", "u_2")
	replica := env.StartReplica(t)
	repo := scoring.NewRedisRepository(env.Redis, scoring.RedisOptions{
		OnlineWindow: 30 * time.Second, TTL: time.Hour, WaitReplicas: 1, WaitTimeout: time.Second,
	})
	res, err := repo.RecordAnswer(context.Background(), answer("u_1", "dq-01-b", true))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replicated {
		t.Error("expected the answer to be acknowledged by the replica")
	}
	if got := replica.HGet(context.Background(), redisx.AnswersKey(string(code), "dq-01"), "u_1").Val(); got == "" {
		t.Error("replica doesn't have the answer right after the acknowledged reply")
	}
}

func TestRecordAnswer_ReportsUnreplicatedWhenNoReplicaAnswers(t *testing.T) {
	openRoom(t, "u_1", "u_2")
	repo := scoring.NewRedisRepository(env.Redis, scoring.RedisOptions{
		OnlineWindow: 30 * time.Second, TTL: time.Hour, WaitReplicas: 1, WaitTimeout: 50 * time.Millisecond,
	})
	res, err := repo.RecordAnswer(context.Background(), answer("u_1", "dq-01-b", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != scoring.Accepted || res.Replicated {
		t.Errorf("result %+v: want accepted but not replicated (non-functional §3.2)", res)
	}
}

func roomField(t *testing.T, f string) int64 {
	t.Helper()
	v, err := env.Redis.HGet(context.Background(), redisx.RoomKey(string(code)), f).Int64()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
