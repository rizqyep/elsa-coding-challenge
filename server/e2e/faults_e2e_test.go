//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/testkit"
)

// room is the shape every fault scenario runs: 40 players, 3 questions, answers within 3 s of each 8 s window.
var room = testkit.RoomSpec{QuestionSet: "demo-quick", Participants: 40, JoinRamp: time.Second,
	Window: 8 * time.Second, Reveal: 2 * time.Second, AnswerWithin: 3 * time.Second, CorrectRatio: 0.7, NoAnswerRatio: 0.05, Seed: 7}

// faulty prepares a fault test: it needs the chaos profile when toxics are used, and it restores the stack afterwards.
func faulty(t *testing.T, needsToxiproxy bool) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	if needsToxiproxy && !env.ToxiproxyUp(ctx) {
		t.Fatal("Toxiproxy isn't reachable: start the stack with make up PROFILES=chaos")
	}
	t.Cleanup(func() {
		c, done := context.WithTimeout(context.Background(), 2*time.Minute)
		defer done()
		if env.ToxiproxyUp(c) {
			_ = env.ResetFaults(c)
		}
		if err := env.StartAll(c); err != nil {
			t.Errorf("restore: %v", err)
		}
		if err := env.WaitHealthy(c); err != nil {
			t.Errorf("restore: %v", err)
		}
	})
	if err := env.WaitHealthy(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func run(ctx context.Context, t *testing.T, steps ...testkit.Step) *testkit.Result {
	t.Helper()
	res, err := env.RunRoom(ctx, room, steps...)
	if err != nil {
		t.Fatal(err)
	}
	requireClean(t, res)
	return res
}

func after(d time.Duration, f func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
		return f(ctx)
	}
}

// Players on both gateways end with the same, correct totals (FR-22).
func TestE2E_TwoGateways_SameTotals(t *testing.T) {
	ctx := faulty(t, false)
	run(ctx, t, testkit.Step{Name: "both gateways serve players", Question: 1, After: time.Second, Do: func(ctx context.Context) error {
		names, err := env.Containers(ctx, "ws", false)
		if err != nil {
			return err
		}
		for _, n := range names {
			m, err := env.MetricsOf(ctx, n)
			if err != nil {
				return err
			}
			if m["ws_connections"] == 0 {
				t.Errorf("%s holds no connections; the test needs players on every gateway", n)
			}
		}
		return nil
	}})
}

// +100 ms on every Redis call: answers still accepted, nothing scored twice (TRD §10.6).
func TestFault_RedisLatency(t *testing.T) {
	ctx := faulty(t, true)
	run(ctx, t, testkit.Step{Name: "redis +100ms", Do: func(ctx context.Context) error { return env.RedisLatency(ctx, 100) }})
}

// Redis connections torn mid-answer: clients retry with the same ids and nothing is counted twice.
func TestFault_RedisResetMidAnswer(t *testing.T) {
	ctx := faulty(t, true)
	run(ctx, t, testkit.Step{Name: "reset redis connections", Question: 1, After: 500 * time.Millisecond, Do: func(ctx context.Context) error {
		if err := env.RedisResetPeer(ctx); err != nil {
			return err
		}
		return after(300*time.Millisecond, func(ctx context.Context) error { return env.RemoveRedisToxic(ctx, "reset") })(ctx)
	}})
}

// Redis cut off for 5 s mid-quiz: answers get server_busy, and the quiz resumes on its stored deadlines.
func TestFault_RedisDown5s(t *testing.T) {
	ctx := faulty(t, true)
	run(ctx, t, testkit.Step{Name: "redis down 5s", Question: 2, After: 500 * time.Millisecond, Do: func(ctx context.Context) error {
		if err := env.SetRedisEnabled(ctx, false); err != nil {
			return err
		}
		return after(5*time.Second, func(ctx context.Context) error { return env.SetRedisEnabled(ctx, true) })(ctx)
	}})
}

// PostgreSQL paused across a question close: the quiz continues and the answers are saved once it's back.
func TestFault_PostgresDownDuringClose(t *testing.T) {
	ctx := faulty(t, false)
	run(ctx, t, testkit.Step{Name: "postgres paused 10s", Question: 1, After: time.Second, Do: func(ctx context.Context) error {
		if err := env.PausePostgres(ctx); err != nil {
			return err
		}
		return after(10*time.Second, env.UnpausePostgres)(ctx)
	}})
}

// A gateway killed mid-question: its players reconnect through nginx and nothing is lost or counted twice.
func TestFault_GatewayKilledMidQuestion(t *testing.T) {
	ctx := faulty(t, false)
	res := run(ctx, t, testkit.Step{Name: "kill ws-1", Question: 2, After: time.Second,
		Do: func(ctx context.Context) error { return env.Kill(ctx, "quiz-ws-1") }})
	if res.Reconnects == 0 {
		t.Error("no player reconnected; the kill missed every connection")
	}
}

// A worker killed while answers are being saved: the job comes back after its visibility timeout and is saved once.
func TestFault_WorkerKilledDuringFlush(t *testing.T) {
	ctx := faulty(t, false)
	run(ctx, t, testkit.Step{Name: "kill worker-1", Question: 2, After: 50 * time.Millisecond,
		Do: func(ctx context.Context) error { return env.Kill(ctx, "quiz-worker-1") }})
}

// With every worker stopped, a question stays open past its close time, yet the answer script still
// refuses late answers; the quiz resumes when the workers return.
func TestFault_AllWorkersStopped_NoLateAnswers(t *testing.T) {
	ctx := faulty(t, false)
	host, code := quizWith(ctx, t, 5*time.Second)
	keys, err := env.AnswerKeys(ctx, "demo-quick")
	if err != nil {
		t.Fatal(err)
	}
	late, _ := joinAs(ctx, t, participant(ctx, t), code, "Rina")
	joinAs(ctx, t, participant(ctx, t), code, "Budi") // silent, so nothing closes early
	if err := env.Start(ctx, host, code); err != nil {
		t.Fatal(err)
	}
	qm, err := late.Expect(ctx, 0, "question")
	if err != nil {
		t.Fatal(err)
	}
	q := qm.Data["question"].(map[string]any)
	if err := env.StopService(ctx, "worker"); err != nil {
		t.Fatal(err)
	}
	closeAt := time.UnixMilli(int64(q["closeAt"].(float64)))
	time.Sleep(time.Until(closeAt) + 500*time.Millisecond)
	id, err := late.Send("submit_answer", map[string]any{"questionId": q["questionId"], "optionId": keys[q["questionId"].(string)]})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := late.WaitFor(ctx, 0, func(m testkit.Message) bool { return m.ID == id })
	if err != nil {
		t.Fatal(err)
	}
	if reply.Type != "error" || reply.Data["code"] != "question_closed" {
		t.Fatalf("answer 500 ms after closeAt with no worker running: %s %v; want error question_closed", reply.Type, reply.Data)
	}
	if err := env.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := late.Expect(ctx, 0, "quiz_finished"); err != nil {
		t.Fatalf("quiz did not resume after the workers returned: %v", err)
	}
	noViolations(t, late)
}
