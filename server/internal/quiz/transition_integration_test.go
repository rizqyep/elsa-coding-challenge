//go:build integration

package quiz_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
)

// The Lua state machine must agree with quiz.Next on every shared vector (TRD §3.5).
func TestNextLua_SharedVectors(t *testing.T) {
	env.Reset(t)
	b, err := os.ReadFile("testdata/transition_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Description string           `json:"description"`
		Cases       []transitionCase `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	glue := `
local r = {status = ARGV[1], q_index = tonumber(ARGV[2]), q_count = tonumber(ARGV[3]), window_ms = tonumber(ARGV[4]),
  reveal_ms = tonumber(ARGV[5]), opened_at = tonumber(ARGV[6]), deadline = tonumber(ARGV[7]), close_at = tonumber(ARGV[8]),
  next_at = tonumber(ARGV[9]), start_requested = ARGV[10] == '1', lobby_expires_at = tonumber(ARGV[11]), state_ver = tonumber(ARGV[12])}
local outcome, ev = next_state(r, tonumber(ARGV[13]))
return {outcome, ev, r.status, r.q_index, r.opened_at, r.deadline, r.close_at, r.next_at, r.state_ver}`
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			in := c.Room
			reply, err := env.Redis.Eval(context.Background(), quiz.NextLua+glue, nil,
				string(in.Status), in.QuestionIndex, in.QuestionCount, in.WindowMs, in.RevealMs, in.OpenedAt, in.Deadline,
				in.CloseAt, in.NextTransitionAt, boolField(in.StartRequested), in.LobbyExpiresAt, in.StateVersion, c.Now).Slice()
			if err != nil {
				t.Fatal(err)
			}
			wantEvent := ""
			if c.Expect.Event != nil {
				wantEvent = string(c.Expect.Event.Type)
			}
			w := c.Expect.Room
			want := fmt.Sprint([]any{string(c.Expect.Outcome), wantEvent, string(w.Status), int64(w.QuestionIndex), w.OpenedAt, w.Deadline, w.CloseAt, w.NextTransitionAt, w.StateVersion})
			if got := fmt.Sprint(reply); got != want {
				t.Errorf("Lua got  %s\n         want %s", got, want)
			}
		})
	}
}

// started creates a room with the given participants and a start request, and returns the repository.
func started(t *testing.T, participants ...string) *quiz.RedisRepository {
	t.Helper()
	repo := newRepo(t)
	ctx := context.Background()
	in := input
	in.QuestionIDs = []quiz.QuestionID{"dq-01", "dq-02"}
	if err := repo.CreateRoom(ctx, in); err != nil {
		t.Fatal(err)
	}
	k := string(in.Code)
	for _, p := range participants {
		env.Redis.HSet(ctx, redisx.RosterKey(k), p, "P-"+p)
		env.Redis.ZAdd(ctx, redisx.LeaderboardKey(k), redisZ(p))
	}
	if err := repo.Start(ctx, in.Code, "host_1"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func subscribe(t *testing.T) func() map[string]any {
	t.Helper()
	ctx := context.Background()
	sub := env.Redis.Subscribe(ctx, redisx.RoomChannel(string(input.Code)))
	t.Cleanup(func() { _ = sub.Close() })
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	return func() map[string]any {
		t.Helper()
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		msg, err := sub.ReceiveMessage(cctx)
		if err != nil {
			t.Fatalf("no event: %v", err)
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
			t.Fatalf("event %q: %v", msg.Payload, err)
		}
		return ev
	}
}

func apply(t *testing.T, repo *quiz.RedisRepository) quiz.TransitionResult {
	t.Helper()
	res, err := repo.ApplyTransition(context.Background(), input.Code)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// forceDue makes the pending transition due now, as if its time had passed.
func forceDue(t *testing.T, fields ...any) {
	t.Helper()
	past := env.Redis.Time(context.Background()).Val().UnixMilli() - 1
	args := append([]any{"next_at", past}, fields...)
	env.Redis.HSet(context.Background(), redisx.RoomKey(string(input.Code)), args...)
}

func TestTransition_OpensTheFirstQuestionAndAnnouncesIt(t *testing.T) {
	repo := started(t, "u_1")
	next := subscribe(t)
	res := apply(t, repo)
	if res.Outcome != quiz.Applied || res.Status != quiz.StatusQuestionOpen || res.StateVersion != 2 {
		t.Fatalf("result %+v", res)
	}
	rec, _ := repo.Room(context.Background(), input.Code)
	if rec.QuestionID != "dq-01" || rec.QuestionIndex != 0 || rec.Deadline != rec.OpenedAt+input.WindowMs || rec.CloseAt != rec.Deadline || rec.NextTransitionAt != rec.Deadline {
		t.Errorf("room %+v", rec)
	}
	if s := env.Redis.ZScore(context.Background(), redisx.SchedTransitions, string(input.Code)).Val(); int64(s) != rec.Deadline {
		t.Errorf("next transition scheduled at %v, want the deadline %d", s, rec.Deadline)
	}
	ev := next()
	if ev["t"] != "state" || ev["s"] != "question_open" || ev["q"] != "dq-01" || ev["v"] != 2.0 || ev["c"] != float64(rec.CloseAt) || ev["n"] != 2.0 {
		t.Errorf("event %v", ev)
	}
}

func TestTransition_StaleVersionChangesNothing(t *testing.T) {
	repo := started(t, "u_1")
	ctx := context.Background()
	before, _ := repo.Room(ctx, input.Code)
	res, err := repo.ApplyTransitionAt(ctx, input.Code, before.StateVersion+1)
	if err != nil || res.Outcome != quiz.Stale {
		t.Fatalf("got %+v, %v; want stale", res, err)
	}
	if after, _ := repo.Room(ctx, input.Code); after != before {
		t.Errorf("room changed:\n  %+v\n  %+v", before, after)
	}
}

func TestTransition_NotDueRepairsTheSchedule(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	if err := repo.CreateRoom(ctx, input); err != nil {
		t.Fatal(err)
	}
	env.Redis.ZRem(ctx, redisx.SchedTransitions, string(input.Code))
	if res := apply(t, repo); res.Outcome != quiz.NotDue {
		t.Fatalf("got %+v", res)
	}
	rec, _ := repo.Room(ctx, input.Code)
	if s, err := env.Redis.ZScore(ctx, redisx.SchedTransitions, string(input.Code)).Result(); err != nil || int64(s) != rec.NextTransitionAt {
		t.Errorf("schedule not repaired: %v %v", s, err)
	}
}

func TestTransition_CloseQueuesTheFlushAndMarksTheLeaderboard(t *testing.T) {
	repo := started(t, "u_1")
	ctx := context.Background()
	k := string(input.Code)
	apply(t, repo) // open dq-01
	forceDue(t, "close_at", env.Redis.Time(ctx).Val().UnixMilli()-1)
	env.Redis.SRem(ctx, redisx.SchedLeaderboardDirty, k)
	next := subscribe(t)
	res := apply(t, repo)
	if res.Outcome != quiz.Applied || res.Status != quiz.StatusQuestionClosed {
		t.Fatalf("result %+v", res)
	}
	if _, err := env.Redis.ZScore(ctx, redisx.SchedFlush, "q|"+k+"|dq-01").Result(); err != nil {
		t.Errorf("flush job not queued: %v", err)
	}
	rec, _ := repo.Room(ctx, input.Code)
	if rec.PendingFlush != 1 {
		t.Errorf("pending_flush = %d, want 1", rec.PendingFlush)
	}
	if !env.Redis.SIsMember(ctx, redisx.SchedLeaderboardDirty, k).Val() {
		t.Error("leaderboard not marked dirty at close")
	}
	if ev := next(); ev["s"] != "question_closed" || ev["q"] != "dq-01" {
		t.Errorf("event %v", ev)
	}
}

func TestTransition_NextQuestionComesFromTheList(t *testing.T) {
	repo := started(t, "u_1")
	apply(t, repo)                    // open dq-01
	forceDue(t, "close_at", int64(0)) // close now
	apply(t, repo)                    // closed
	forceDue(t)                       // reveal over
	if res := apply(t, repo); res.Status != quiz.StatusQuestionOpen {
		t.Fatalf("result %+v", res)
	}
	if rec, _ := repo.Room(context.Background(), input.Code); rec.QuestionID != "dq-02" || rec.QuestionIndex != 1 {
		t.Errorf("question %q index %d, want dq-02 / 1", rec.QuestionID, rec.QuestionIndex)
	}
}

func TestTransition_FinishPublishesFinalStandingsAndQueuesResults(t *testing.T) {
	repo := started(t, "u_1", "u_2")
	ctx := context.Background()
	k := string(input.Code)
	env.Redis.ZIncrBy(ctx, redisx.LeaderboardKey(k), 186, "u_2")
	for range 2 { // two questions: open, close, (reveal over → next or finish)
		apply(t, repo)
		forceDue(t, "close_at", int64(0))
		apply(t, repo)
		forceDue(t)
	}
	next := subscribe(t)
	res := apply(t, repo)
	if res.Status != quiz.StatusFinished {
		t.Fatalf("result %+v", res)
	}
	if _, err := env.Redis.ZScore(ctx, redisx.SchedTransitions, k).Result(); err == nil {
		t.Error("a finished quiz is still scheduled")
	}
	if _, err := env.Redis.ZScore(ctx, redisx.SchedFlush, "final|"+k).Result(); err != nil {
		t.Errorf("final-results job not queued: %v", err)
	}
	ev := next()
	top, _ := ev["top"].([]any)
	if ev["t"] != "finished" || ev["n"] != 2.0 || len(top) != 2 {
		t.Fatalf("event %v", ev)
	}
	if first := top[0].([]any); first[0] != "u_2" || first[1] != "P-u_2" || first[2] != 186.0 {
		t.Errorf("top[0] = %v", first)
	}
}

func TestTransition_LobbyExpiry(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	if err := repo.CreateRoom(ctx, input); err != nil {
		t.Fatal(err)
	}
	now := env.Redis.Time(ctx).Val().UnixMilli()
	forceDue(t, "lobby_expires_at", now-1)
	next := subscribe(t)
	if res := apply(t, repo); res.Status != quiz.StatusExpired {
		t.Fatalf("result %+v", res)
	}
	if _, err := env.Redis.ZScore(ctx, redisx.SchedFlush, "final|"+string(input.Code)).Result(); err != nil {
		t.Errorf("final job not queued for an expired quiz: %v", err)
	}
	if ev := next(); ev["t"] != "state" || ev["s"] != "expired" {
		t.Errorf("event %v", ev)
	}
}

func TestTransition_TerminalAndUnknownRoomsLeaveTheSchedule(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	env.Redis.ZAdd(ctx, redisx.SchedTransitions, redisZ("GHOST2"))
	if res, err := repo.ApplyTransition(ctx, "GHOST2"); err != nil || res.Outcome != quiz.Stale {
		t.Errorf("unknown room: %+v %v", res, err)
	}
	if _, err := env.Redis.ZScore(ctx, redisx.SchedTransitions, "GHOST2").Result(); err == nil {
		t.Error("unknown room still scheduled")
	}
}

// NFR-14: several workers applying the same due transition → exactly one applies it.
func TestTransition_CompetingWorkersApplyOnce(t *testing.T) {
	repo := started(t, "u_1")
	ctx := context.Background()
	rec, _ := repo.Room(ctx, input.Code)
	const workers = 20
	outcomes := make(chan quiz.Outcome, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			res, err := repo.ApplyTransitionAt(ctx, input.Code, rec.StateVersion)
			if err != nil {
				t.Error(err)
			}
			outcomes <- res.Outcome
		})
	}
	wg.Wait()
	close(outcomes)
	applied := 0
	for o := range outcomes {
		if o == quiz.Applied {
			applied++
		}
	}
	if applied != 1 {
		t.Errorf("%d workers applied the transition, want exactly 1", applied)
	}
	if after, _ := repo.Room(ctx, input.Code); after.StateVersion != rec.StateVersion+1 {
		t.Errorf("state version %d, want %d", after.StateVersion, rec.StateVersion+1)
	}
}

func TestDueTransitions(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	now := env.Redis.Time(ctx).Val().UnixMilli()
	env.Redis.ZAdd(ctx, redisx.SchedTransitions, redisZScore("AAAAAA", now-10), redisZScore("BBBBBB", now-5), redisZScore("CCCCCC", now+60_000))
	due, err := repo.DueTransitions(ctx, now, 10)
	if err != nil || fmt.Sprint(due) != "[AAAAAA BBBBBB]" {
		t.Errorf("due = %v, %v; want [AAAAAA BBBBBB]", due, err)
	}
}

// Answers racing the close: each is either accepted before the close time or rejected; never
// both, never lost, never stored after the close (TRD §10.5).
func TestTransition_AnswersRacingTheClose(t *testing.T) {
	var participants []string
	for i := range 20 {
		participants = append(participants, fmt.Sprintf("u_%02d", i))
	}
	repo := started(t, participants...)
	ctx := context.Background()
	k := string(input.Code)
	if err := redisx.LoadScripts(ctx, env.Redis, scoring.Scripts()...); err != nil {
		t.Fatal(err)
	}
	apply(t, repo) // open dq-01
	now := env.Redis.Time(ctx).Val().UnixMilli()
	for _, p := range participants { // mark everyone online so early close can't fire before the race
		env.Redis.ZAdd(ctx, redisx.OnlineKey(k), redisZScore(p, now))
	}
	env.Redis.ZAdd(ctx, redisx.OnlineKey(k), redisZScore("u_absent", now)) // keeps "everyone answered" false
	env.Redis.HSet(ctx, redisx.RosterKey(k), "u_absent", "absent")
	closeAt := now + 100
	env.Redis.HSet(ctx, redisx.RoomKey(k), "close_at", closeAt, "next_at", closeAt)

	answers := scoring.NewRedisRepository(env.Redis, scoring.RedisOptions{OnlineWindow: 30 * time.Second, TTL: time.Hour})
	type outcome struct {
		res scoring.AnswerResult
		err error
	}
	results := make(chan outcome, len(participants))
	var wg sync.WaitGroup
	for i, p := range participants {
		wg.Go(func() {
			time.Sleep(time.Duration(i*10) * time.Millisecond) // spread across 0–190 ms, straddling the close at 100 ms
			res, err := answers.RecordAnswer(ctx, scoring.AnswerInput{Code: input.Code, ParticipantID: quiz.ParticipantID(p), QuestionID: "dq-01", OptionID: "dq-01-b", Correct: true})
			results <- outcome{res, err}
		})
	}
	wg.Go(func() { // a worker applying the close as soon as it is due
		for range 1000 {
			if res, err := repo.ApplyTransition(ctx, input.Code); err == nil && res.Status == quiz.StatusQuestionClosed {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Error("the close was never applied")
	})
	wg.Wait()
	close(results)

	finalClose := roomInt(t, "close_at")
	accepted, sum := 0, 0
	for o := range results {
		switch {
		case o.err == nil:
			accepted++
			sum += o.res.Points
			if o.res.ReceivedAt >= finalClose {
				t.Errorf("accepted an answer received at %d, at or after the close %d", o.res.ReceivedAt, finalClose)
			}
		case !errors.Is(o.err, scoring.ErrQuestionClosed):
			t.Errorf("unexpected error %v", o.err)
		}
	}
	if accepted == 0 || accepted == len(participants) {
		t.Errorf("%d of %d accepted: the race didn't straddle the close", accepted, len(participants))
	}
	if n := env.Redis.HLen(ctx, redisx.AnswersKey(k, "dq-01")).Val(); int(n) != accepted {
		t.Errorf("%d answers stored, %d accepted", n, accepted)
	}
	total := 0
	for _, z := range env.Redis.ZRangeWithScores(ctx, redisx.LeaderboardKey(k), 0, -1).Val() {
		total += int(z.Score)
	}
	if total != sum {
		t.Errorf("leaderboard sums to %d, accepted points sum to %d", total, sum)
	}
}

func roomInt(t *testing.T, field string) int64 {
	t.Helper()
	v, err := env.Redis.HGet(context.Background(), redisx.RoomKey(string(input.Code)), field).Int64()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
