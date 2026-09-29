//go:build integration

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics/metricstest"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

const code = quiz.Code("K7Q2MX")

// startWorker runs the real cmd/worker wiring with fast polls until the test ends.
func startWorker(t *testing.T) *httptest.Server {
	t.Helper()
	vars := map[string]string{"POSTGRES_DSN": env.PostgresDSN, "AUTH_SIGNING_KEY": "0123456789abcdef0123456789abcdef",
		"SCHED_TRANSITION_POLL": "20ms", "SCHED_LEADERBOARD_TICK": "50ms", "SCHED_FLUSH_POLL": "50ms"}
	cfg, err := config.LoadWorker(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	w, err := newWorker(context.Background(), cfg, env.Redis, env.Postgres, slog.New(slog.DiscardHandler), prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wg := w.start(ctx)
	srv := httptest.NewServer(w.handler)
	t.Cleanup(func() { srv.Close(); cancel(); wg.Wait() })
	return srv
}

type event struct {
	T string      `json:"t"`
	V float64     `json:"v"`
	S quiz.Status `json:"s"`
	Q string      `json:"q"`
}

// Two workers run a whole quiz with no forced time: questions open, close early once everyone
// answered, reveal, advance, finish, and the results are saved and the room released (FR-2…FR-5, FR-32…FR-35).
func TestWorkers_RunAQuizOnTheirOwn(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	for _, s := range [][]*redisx.Script{session.Scripts(), scoring.Scripts()} {
		if err := redisx.LoadScripts(ctx, env.Redis, s...); err != nil {
			t.Fatal(err)
		}
	}
	a, b := startWorker(t), startWorker(t)

	set, err := quiz.NewPostgresStore(env.Postgres).QuestionSet(ctx, "demo-quick")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]quiz.QuestionID, len(set.Questions))
	for i, q := range set.Questions {
		ids[i] = q.ID
	}
	if _, err := env.Postgres.Exec(ctx, `INSERT INTO quizzes (code, question_set_id, host_id, status, window_ms, reveal_ms)
		VALUES ($1, 'demo-quick', 'host_1', 'lobby', 5000, 300)`, string(code)); err != nil {
		t.Fatal(err)
	}
	qr := quiz.NewRedisRepository(env.Redis)
	if _, err := qr.CreateRoom(ctx, quiz.CreateRoomInput{Code: code, QuestionSetID: "demo-quick", HostID: "host_1",
		QuestionIDs: ids, WindowMs: 5_000, RevealMs: 300, LobbyTimeoutMs: 1_800_000, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewRedisRepository(env.Redis)
	for _, id := range []quiz.ParticipantID{"u_1", "u_2"} {
		if _, err := sessions.Join(ctx, session.JoinInput{Code: code, ParticipantID: id, DisplayName: string(id), TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
	sub := env.Redis.Subscribe(ctx, redisx.RoomChannel(string(code)))
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}

	answers := scoring.NewRedisRepository(env.Redis, scoring.RedisOptions{OnlineWindow: 30 * time.Second, TTL: time.Hour})
	started := time.Now()
	if err := qr.Start(ctx, code, "host_1"); err != nil {
		t.Fatal(err)
	}

	versions := map[float64]int{}
	var states []quiz.Status
	lbEvents, total := 0, 0
	timeout := time.After(15 * time.Second)
	for done := false; !done; {
		select {
		case msg := <-sub.Channel():
			var ev event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				t.Fatal(err)
			}
			switch ev.T {
			case "lb":
				lbEvents++
			case "state", "finished":
				versions[ev.V]++
				if ev.T == "finished" {
					states = append(states, quiz.StatusFinished)
					done = true
					break
				}
				if ev.S == quiz.StatusQuestionOpen && (len(states) == 0 || states[len(states)-1] != quiz.StatusQuestionOpen) {
					q, _ := set.Question(quiz.QuestionID(ev.Q))
					for id, opt := range map[quiz.ParticipantID]quiz.OptionID{"u_1": q.CorrectOptionID, "u_2": wrongOption(q)} {
						res, err := answers.RecordAnswer(ctx, scoring.AnswerInput{Code: code, ParticipantID: id, QuestionID: q.ID, OptionID: opt, Correct: q.IsCorrect(opt)})
						if err != nil {
							t.Fatalf("answer %s: %v", id, err)
						}
						if id == "u_1" {
							total += res.Points
						}
					}
				}
				if len(states) == 0 || states[len(states)-1] != ev.S {
					states = append(states, ev.S)
				}
			}
		case <-timeout:
			t.Fatalf("quiz did not finish on its own; states so far %v", states)
		}
	}

	want := []quiz.Status{quiz.StatusQuestionOpen, quiz.StatusQuestionClosed, quiz.StatusQuestionOpen, quiz.StatusQuestionClosed,
		quiz.StatusQuestionOpen, quiz.StatusQuestionClosed, quiz.StatusFinished}
	if len(states) != len(want) {
		t.Fatalf("states %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states %v, want %v", states, want)
		}
	}
	for v, n := range versions {
		if n != 1 {
			t.Errorf("state version %v published %d times with two workers; each transition happens once (NFR-14)", v, n)
		}
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("took %v for 3 questions with 5 s windows; early close should end each as soon as both answered (FR-5)", elapsed)
	}
	if lbEvents == 0 {
		t.Error("no leaderboard update published after scores changed")
	}

	deadline := time.Now().Add(10 * time.Second)
	for env.Redis.Exists(ctx, redisx.RoomKey(string(code))).Val() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("room not released after the quiz finished")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var status string
	var saved, top int
	var leader string
	if err := env.Postgres.QueryRow(ctx, `SELECT status FROM quizzes WHERE code = $1`, string(code)).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := env.Postgres.QueryRow(ctx, `SELECT count(*) FROM answers WHERE quiz_code = $1`, string(code)).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if err := env.Postgres.QueryRow(ctx, `SELECT participant_id, total_score FROM quiz_results WHERE quiz_code = $1 AND rank = 1`,
		string(code)).Scan(&leader, &top); err != nil {
		t.Fatal(err)
	}
	if status != "finished" || saved != 6 || leader != "u_1" || top != total {
		t.Errorf("archived: status %s, %d answers, leader %s with %d; want finished, 6, u_1 with %d", status, saved, leader, top, total)
	}

	m := metricstest.Scrape(t, a.URL, b.URL)
	for to, want := range map[string]float64{"question_open": 3, "question_closed": 3, "finished": 1} {
		if got := m[`transitions_total{to="`+to+`"}`]; got != want {
			t.Errorf("transitions_total{to=%q} = %v across both workers, want %v", to, got, want)
		}
	}
	if got, ok := m["score_reconciliation_mismatches_total"]; !ok || got != 0 {
		t.Errorf("score_reconciliation_mismatches_total = %v (exported %v), want 0 (FR-35)", got, ok)
	}
	metricstest.AssertBoundedLabels(t, m)

	for _, srv := range []*httptest.Server{a, b} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/healthz", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("healthz %d after a busy run", resp.StatusCode)
		}
	}
}

func wrongOption(q quiz.Question) quiz.OptionID {
	for _, o := range q.Options {
		if o.ID != q.CorrectOptionID {
			return o.ID
		}
	}
	return ""
}
