//go:build integration

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics/metricstest"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

const (
	code       = quiz.Code("K7Q2MX")
	signingKey = "0123456789abcdef0123456789abcdef"
)

var tokens = &auth.Tokens{Key: []byte(signingKey), TTL: time.Hour}

type server struct {
	url  string
	http string // base URL for /metrics, /healthz, /readyz
	reg  *prometheus.Registry
}

// startGateway runs the real cmd/ws wiring, with its background loops, until the test ends.
func startGateway(t *testing.T) server {
	t.Helper()
	vars := map[string]string{"POSTGRES_DSN": env.PostgresDSN, "AUTH_SIGNING_KEY": signingKey,
		"PRESENCE_REFRESH": "100ms", "PRESENCE_TTL": "1s"}
	cfg, err := config.LoadGateway(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	gw, err := newGateway(context.Background(), cfg, env.Redis, env.Postgres, slog.New(slog.DiscardHandler), reg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wg := gw.start(ctx)
	srv := httptest.NewServer(gw.handler)
	t.Cleanup(func() { srv.Close(); cancel(); wg.Wait() })
	return server{url: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", http: srv.URL, reg: reg}
}

// setup resets the stores and creates a demo-quick lobby with 15 s answer windows.
func setup(t *testing.T) (*quiz.RedisRepository, *quiz.QuestionSet) {
	t.Helper()
	env.Reset(t)
	ctx := context.Background()
	scripts := append(append(append(quiz.Scripts(), session.Scripts()...), scoring.Scripts()...), leaderboard.Scripts()...)
	scripts = append(scripts, history.Scripts()...)
	if err := redisx.LoadScripts(ctx, env.Redis, scripts...); err != nil {
		t.Fatal(err)
	}
	set, err := quiz.NewPostgresStore(env.Postgres).QuestionSet(ctx, "demo-quick")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]quiz.QuestionID, len(set.Questions))
	for i, q := range set.Questions {
		ids[i] = q.ID
	}
	if _, err := env.Postgres.Exec(ctx, `INSERT INTO quizzes (code, question_set_id, host_id, status, window_ms, reveal_ms)
		VALUES ($1, 'demo-quick', 'host_1', 'lobby', 15000, 2000)`, string(code)); err != nil {
		t.Fatal(err)
	}
	qr := quiz.NewRedisRepository(env.Redis)
	if _, err := qr.CreateRoom(ctx, quiz.CreateRoomInput{Code: code, QuestionSetID: "demo-quick", HostID: "host_1",
		QuestionIDs: ids, WindowMs: 15_000, RevealMs: 2_000, LobbyTimeoutMs: 1_800_000, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	return qr, &set
}

type client struct {
	t  *testing.T
	ws *websocket.Conn
}

func dial(t *testing.T, s server, id string, role auth.Role) *client {
	t.Helper()
	tok, _, err := tokens.Issue(id, role)
	if err != nil {
		t.Fatal(err)
	}
	ws, resp, err := websocket.DefaultDialer.Dial(s.url+"?token="+tok, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return &client{t: t, ws: ws}
}

func (c *client) send(id string, typ protocol.Type, payload any) {
	c.t.Helper()
	b, err := json.Marshal(map[string]any{"v": 1, "type": typ, "id": id, "data": payload})
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
		c.t.Fatal(err)
	}
}

// expect reads until a message of type typ arrives, checking every frame against the contract.
func (c *client) expect(typ protocol.Type) map[string]any {
	c.t.Helper()
	_ = c.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, b, err := c.ws.ReadMessage()
		if err != nil {
			c.t.Fatalf("waiting for %s: %v", typ, err)
		}
		if err := protocol.ValidateServer(b); err != nil {
			c.t.Fatalf("frame breaks the contract: %v\n%s", err, b)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if m["type"] == string(typ) {
			return m
		}
		if m["type"] == "error" && typ != protocol.TypeError {
			c.t.Fatalf("error while waiting for %s: %s", typ, b)
		}
	}
}

func d(m map[string]any) map[string]any { return m["data"].(map[string]any) }

func transition(t *testing.T, qr *quiz.RedisRepository, want quiz.Status) {
	t.Helper()
	ctx := context.Background()
	past := env.Redis.Time(ctx).Val().UnixMilli() - 1
	fields := []any{"next_at", past}
	if want == quiz.StatusQuestionClosed {
		fields = append(fields, "close_at", past)
	}
	env.Redis.HSet(ctx, redisx.RoomKey(string(code)), fields...)
	res, err := qr.ApplyTransition(ctx, code)
	if err != nil || res.Status != want {
		t.Fatalf("transition: %+v %v, want %s", res, err, want)
	}
}

// The real-time flow through the real wiring: join, watch, question, answers, leaderboard, reveal,
// ranks, finish, and a late join served from the archived results (FR-8…FR-30).
func TestFlow_RealtimeQuizThroughTheGateway(t *testing.T) {
	qr, set := setup(t)
	gw := startGateway(t)
	ctx := context.Background()

	host := dial(t, gw, "host_1", auth.RoleHost)
	host.send("w", protocol.TypeWatch, map[string]any{"quizCode": string(code)})
	if s := d(host.expect(protocol.TypeSnapshot)); s["role"] != "host" {
		t.Fatalf("host snapshot %v", s)
	}
	players := map[string]*client{}
	for _, id := range []string{"u_1", "u_2", "u_3"} {
		c := dial(t, gw, id, auth.RoleParticipant)
		c.send("j", protocol.TypeJoin, map[string]any{"quizCode": string(code), "displayName": "P " + id})
		if s := c.expect(protocol.TypeSnapshot); s["id"] != "j" || d(s)["quiz"].(map[string]any)["status"] != "lobby" {
			t.Fatalf("%s snapshot %v", id, s)
		}
		players[id] = c
	}

	if err := qr.Start(ctx, code, "host_1"); err != nil {
		t.Fatal(err)
	}
	transition(t, qr, quiz.StatusQuestionOpen)
	q1 := set.Questions[0]
	for id, c := range players {
		if q := d(c.expect(protocol.TypeQuestion))["question"].(map[string]any); q["questionId"] != string(q1.ID) || q["correctOptionId"] != nil {
			t.Fatalf("%s got question %v", id, q)
		}
	}
	host.expect(protocol.TypeQuestion)

	// Presence is refreshed at Redis TIME while connected (TRD §7.7).
	online := redisx.OnlineKey(string(code))
	env.Redis.ZAdd(ctx, online, redisZ(0, "u_1"))
	deadline := time.Now().Add(2 * time.Second)
	for env.Redis.ZScore(ctx, online, "u_1").Val() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("presence not refreshed while connected")
		}
		time.Sleep(20 * time.Millisecond)
	}

	wrong := q1.Options[0].ID
	if wrong == q1.CorrectOptionID {
		wrong = q1.Options[1].ID
	}
	players["u_1"].send("a1", protocol.TypeSubmitAnswer, map[string]any{"questionId": q1.ID, "optionId": q1.CorrectOptionID})
	r1 := d(players["u_1"].expect(protocol.TypeAnswerResult))
	if r1["status"] != "accepted" || r1["correct"] != true || r1["points"].(float64) <= 100 {
		t.Fatalf("correct answer: %v", r1)
	}
	players["u_1"].send("a1", protocol.TypeSubmitAnswer, map[string]any{"questionId": q1.ID, "optionId": q1.CorrectOptionID})
	if dup := d(players["u_1"].expect(protocol.TypeAnswerResult)); dup["status"] != "duplicate" || dup["points"] != r1["points"] {
		t.Fatalf("resend: %v, want the original result (FR-18)", dup)
	}
	players["u_2"].send("a2", protocol.TypeSubmitAnswer, map[string]any{"questionId": q1.ID, "optionId": wrong})
	if r2 := d(players["u_2"].expect(protocol.TypeAnswerResult)); r2["correct"] != false || r2["points"] != 0.0 {
		t.Fatalf("wrong answer: %v", r2)
	}
	players["u_3"].send("a3", protocol.TypeSubmitAnswer, map[string]any{"questionId": q1.ID, "optionId": "not-an-option"})
	if e := d(players["u_3"].expect(protocol.TypeError)); e["code"] != "invalid_option" {
		t.Fatalf("invalid option: %v", e)
	}

	if _, err := leaderboard.NewRedisRepository(env.Redis).PublishSnapshot(ctx, code); err != nil {
		t.Fatal(err)
	}
	for id, c := range players {
		top := d(c.expect(protocol.TypeLeaderboard))["top"].([]any)
		if first := top[0].(map[string]any); first["participantId"] != "u_1" || first["score"] != r1["points"] {
			t.Fatalf("%s leaderboard top %v", id, top)
		}
	}

	transition(t, qr, quiz.StatusQuestionClosed)
	for id, c := range players {
		if qc := d(c.expect(protocol.TypeQuestionClosed)); qc["correctOptionId"] != string(q1.CorrectOptionID) {
			t.Fatalf("%s reveal %v", id, qc)
		}
		wantRank := 2.0
		if id == "u_1" {
			wantRank = 1
		}
		if rk := d(c.expect(protocol.TypeRank)); rk["rank"] != wantRank {
			t.Errorf("%s rank %v, want %v", id, rk, wantRank)
		}
	}

	for range len(set.Questions) - 1 {
		transition(t, qr, quiz.StatusQuestionOpen)
		transition(t, qr, quiz.StatusQuestionClosed)
	}
	transition(t, qr, quiz.StatusFinished)
	for id, c := range players {
		if f := d(c.expect(protocol.TypeQuizFinished)); f["participantCount"] != 3.0 {
			t.Fatalf("%s finished %v", id, f)
		}
	}

	runJobs(t)
	late := dial(t, gw, "u_1", auth.RoleParticipant)
	late.send("j2", protocol.TypeJoin, map[string]any{"quizCode": string(code), "displayName": "P u_1"})
	s := d(late.expect(protocol.TypeSnapshot))
	if s["quiz"].(map[string]any)["status"] != "finished" || s["you"].(map[string]any)["score"] != r1["points"] {
		t.Fatalf("join after release: %v, want the archived final result (FR-13)", s)
	}

	if n := errorsTotal(t, gw.reg, "invalid_option"); n != 1 {
		t.Errorf("errors_total{code=invalid_option} = %v, want 1", n)
	}
	m := metricstest.Scrape(t, gw.http)
	if n := m["answer_duration_seconds_count"]; n < 3 { // two for u_1 (one a duplicate), one for u_2; the invalid option never reaches Redis
		t.Errorf("answer_duration_seconds_count = %v, want every answer that reached Redis timed", n)
	}
	if n := m["broadcast_fanout_seconds_count"]; n < 5 {
		t.Errorf("broadcast_fanout_seconds_count = %v, want each question, reveal, and leaderboard broadcast timed", n)
	}
	metricstest.AssertBoundedLabels(t, m)
}

// A second connection on another gateway closes the first with 4000 through the kick event (FR-12).
func TestKick_AcrossGateways(t *testing.T) {
	setup(t)
	a, b := startGateway(t), startGateway(t)
	first := dial(t, a, "u_1", auth.RoleParticipant)
	first.send("j", protocol.TypeJoin, map[string]any{"quizCode": string(code), "displayName": "Rina"})
	first.expect(protocol.TypeSnapshot)

	second := dial(t, b, "u_1", auth.RoleParticipant)
	second.send("j", protocol.TypeJoin, map[string]any{"quizCode": string(code), "displayName": "Rina"})
	second.expect(protocol.TypeSnapshot)

	_ = first.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := first.ws.ReadMessage()
		if err == nil {
			continue
		}
		if !websocket.IsCloseError(err, 4000) {
			t.Fatalf("first connection ended with %v, want close 4000", err)
		}
		break
	}
	second.send("p", protocol.TypePing, map[string]any{"clientTime": 1})
	if p := d(second.expect(protocol.TypePong)); p["serverTime"].(float64) <= 0 {
		t.Errorf("pong %v", p)
	}
}

func runJobs(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	live := history.NewRedisLiveStore(env.Redis)
	svc := history.NewService(live, history.NewPostgresStore(env.Postgres), history.Options{TTL: time.Hour, FinalRetryDelay: 50 * time.Millisecond})
	for range 100 {
		if env.Redis.Exists(ctx, redisx.RoomKey(string(code))).Val() == 0 {
			return
		}
		jobs, err := live.Claim(ctx, 10, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range jobs {
			if err := svc.Process(ctx, j); err != nil {
				t.Fatalf("job %s: %v", j.ID, err)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("room not released")
}

// ws_connections tracks this gateway's open sockets, so a scaled stack can show how nginx spreads them (task-25).
func TestConnectionsGauge(t *testing.T) {
	setup(t)
	gw := startGateway(t)
	a := dial(t, gw, "u_1", auth.RoleParticipant)
	dial(t, gw, "u_2", auth.RoleParticipant)
	waitGauge(t, gw.reg, 2)
	_ = a.ws.Close()
	waitGauge(t, gw.reg, 1)
}

// waitGauge waits up to 2 s for ws_connections to reach want.
func waitGauge(t *testing.T, reg *prometheus.Registry, want float64) {
	t.Helper()
	var got float64
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		families, err := reg.Gather()
		if err != nil {
			t.Fatal(err)
		}
		got = -1
		for _, f := range families {
			if f.GetName() == "ws_connections" {
				got = f.GetMetric()[0].GetGauge().GetValue()
			}
		}
		if got == want {
			return
		}
	}
	t.Fatalf("ws_connections = %v, want %v", got, want)
}

func redisZ(score float64, member string) redis.Z { return redis.Z{Score: score, Member: member} }

// errorsTotal reads errors_total{service="ws", code} from the gateway's registry.
func errorsTotal(t *testing.T, reg *prometheus.Registry, code string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "errors_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["service"] == "ws" && labels["code"] == code {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}
