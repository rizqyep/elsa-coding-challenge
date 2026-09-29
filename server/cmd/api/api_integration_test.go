//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/gen"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/health"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics/metricstest"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/postgres"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

// api builds the real cmd/api wiring over the given Redis client and PostgreSQL pool.
func api(t *testing.T, rdb redis.UniversalClient, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	vars := map[string]string{"POSTGRES_DSN": env.PostgresDSN, "AUTH_SIGNING_KEY": "0123456789abcdef0123456789abcdef"}
	cfg, err := config.LoadAPI(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	ready := health.NewReadiness(func(ctx context.Context) error {
		return errors.Join(redisx.Ping(ctx, rdb, time.Second), postgres.Ping(ctx, pool, time.Second))
	})
	h, err := newHandler(context.Background(), cfg, rdb, pool, slog.New(slog.DiscardHandler), prometheus.NewRegistry(), ready)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

type client struct {
	t *testing.T
	h http.Handler
}

type reply struct {
	status int
	header http.Header
	body   []byte
}

func (c client) call(method, path, token string, body any) reply {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return reply{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes()}
}

func (c client) expect(r reply, status int, code gen.ErrorCode, v any) {
	c.t.Helper()
	if r.status != status {
		c.t.Fatalf("status %d, want %d: %s", r.status, status, r.body)
	}
	if code != "" {
		var p gen.Problem
		_ = json.Unmarshal(r.body, &p)
		if p.Code != code {
			c.t.Fatalf("code %q, want %q", p.Code, code)
		}
	}
	if v != nil {
		if err := json.Unmarshal(r.body, v); err != nil {
			c.t.Fatalf("decode %s: %v", r.body, err)
		}
	}
}

func (c client) token(id string, role gen.Role) string {
	c.t.Helper()
	var tok gen.DevToken
	c.expect(c.call("POST", "/api/v1/dev/tokens", "", map[string]any{"role": role, "participantId": id}), http.StatusCreated, "", &tok)
	return tok.Token
}

// clock drives a room the way the worker does, fast-forwarding each timer.
type clock struct {
	t    *testing.T
	code quiz.Code
	qr   *quiz.RedisRepository
}

func (k clock) advance(want quiz.Status) {
	k.t.Helper()
	ctx := context.Background()
	env.Redis.HSet(ctx, redisx.RoomKey(string(k.code)), "close_at", 0, "next_at", 0)
	res, err := k.qr.ApplyTransition(ctx, k.code)
	if err != nil || res.Status != want {
		k.t.Fatalf("transition: %+v, %v; want %s", res, err, want)
	}
}

// The whole leaderboard flow through the REST API, from creation to the archived results (FR-1…FR-35).
func TestFlow_CreateToArchivedLeaderboard(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	c := client{t: t, h: api(t, env.Redis, env.Postgres)}
	host, viewer := c.token("host_e2e", gen.Host), c.token("u_viewer", gen.Participant)

	var created gen.Quiz
	r := c.call("POST", "/api/v1/quizzes", host, map[string]any{"questionSetId": "demo-quick", "questionWindowSeconds": 10, "revealSeconds": 2})
	c.expect(r, http.StatusCreated, "", &created)
	code := quiz.Code(created.Code)
	if r.header.Get("Location") != "/api/v1/quizzes/"+string(code) || created.Status != gen.Lobby || created.QuestionCount != 3 {
		t.Fatalf("created %+v, Location %q", created, r.header.Get("Location"))
	}
	var q gen.Quiz
	c.expect(c.call("GET", "/api/v1/quizzes/"+string(code), viewer, nil), http.StatusOK, "", &q)
	if q.ParticipantCount != 0 || q.LobbyExpiresAt == nil || !q.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("lobby quiz %+v", q)
	}
	start := "/api/v1/quizzes/" + string(code) + "/start"
	c.expect(c.call("POST", start, host, nil), http.StatusConflict, gen.ErrorCodeNoParticipants, nil)

	players := map[string]string{"u_rina": "Rina", "u_tomas": "Tomás", "u_aiko": "Aiko", "u_zed": "Zed"}
	sr := session.NewRedisRepository(env.Redis)
	for id, name := range players {
		if _, err := sr.Join(ctx, session.JoinInput{Code: code, ParticipantID: quiz.ParticipantID(id), DisplayName: name, TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
	c.expect(c.call("GET", "/api/v1/quizzes/"+string(code), viewer, nil), http.StatusOK, "", &q)
	if q.ParticipantCount != 4 {
		t.Errorf("participantCount %d, want 4", q.ParticipantCount)
	}
	c.expect(c.call("POST", start, viewer, nil), http.StatusForbidden, gen.ErrorCodeForbidden, nil)
	c.expect(c.call("POST", start, c.token("host_other", gen.Host), nil), http.StatusForbidden, gen.ErrorCodeNotHost, nil)
	c.expect(c.call("POST", start, host, nil), http.StatusAccepted, "", nil)
	c.expect(c.call("POST", start, host, nil), http.StatusAccepted, "", nil)

	k := clock{t: t, code: code, qr: quiz.NewRedisRepository(env.Redis)}
	k.advance(quiz.StatusQuestionOpen)
	// A host retrying a timed-out start after question 1 opened still gets 202.
	c.expect(c.call("POST", start, host, nil), http.StatusAccepted, "", nil)

	ar := scoring.NewRedisRepository(env.Redis, scoring.RedisOptions{OnlineWindow: 30 * time.Second, TTL: time.Hour})
	totals := map[string]int{"u_rina": 0, "u_tomas": 0, "u_aiko": 0, "u_zed": 0}
	accepted := 0
	answer := func(q int, correct map[string]bool) {
		t.Helper()
		qid := quiz.QuestionID(fmt.Sprintf("dq-%02d", q))
		for id, ok := range correct {
			option := string(qid) + "-a"
			if ok {
				option = string(qid) + "-b"
			}
			res, err := ar.RecordAnswer(ctx, scoring.AnswerInput{Code: code, ParticipantID: quiz.ParticipantID(id), QuestionID: qid, OptionID: quiz.OptionID(option), Correct: ok})
			if err != nil {
				t.Fatal(err)
			}
			totals[id] += res.Points
			accepted++
		}
	}
	answer(1, map[string]bool{"u_rina": true, "u_tomas": true, "u_aiko": false})

	board := "/api/v1/quizzes/" + string(code) + "/leaderboard"
	var live gen.LeaderboardPage
	c.expect(c.call("GET", board, viewer, nil), http.StatusOK, "", &live)
	checkBoard(t, live, totals, gen.QuestionOpen)

	k.advance(quiz.StatusQuestionClosed)
	k.advance(quiz.StatusQuestionOpen)
	answer(2, map[string]bool{"u_rina": false, "u_tomas": true, "u_zed": false})
	k.advance(quiz.StatusQuestionClosed)
	k.advance(quiz.StatusQuestionOpen)
	answer(3, map[string]bool{"u_rina": true, "u_tomas": false, "u_aiko": false, "u_zed": false})
	k.advance(quiz.StatusQuestionClosed)
	k.advance(quiz.StatusFinished)

	c.expect(c.call("GET", board+"?limit=1000", viewer, nil), http.StatusOK, "", &live)
	checkBoard(t, live, totals, gen.Finished)
	var paged []gen.LeaderboardEntry
	for off := 0; off < 4; off += 3 {
		var p gen.LeaderboardPage
		c.expect(c.call("GET", fmt.Sprintf("%s?offset=%d&limit=3", board, off), viewer, nil), http.StatusOK, "", &p)
		paged = append(paged, p.Entries...)
	}
	if !reflect.DeepEqual(paged, live.Entries) {
		t.Errorf("pages differ from the whole leaderboard:\n%+v\n%+v", paged, live.Entries)
	}

	runJobs(t, code)
	if n := env.Redis.Exists(ctx, redisx.RoomKey(string(code))).Val(); n != 0 {
		t.Fatal("room still in Redis after the final job")
	}

	var archived gen.Quiz
	c.expect(c.call("GET", "/api/v1/quizzes/"+string(code), viewer, nil), http.StatusOK, "", &archived)
	if archived.Status != gen.Finished || archived.FinishedAt == nil || archived.ParticipantCount != 4 ||
		!archived.CreatedAt.Equal(created.CreatedAt) || archived.QuestionWindowSeconds != 10 {
		t.Errorf("archived quiz %+v (created %v)", archived, created.CreatedAt)
	}
	var final gen.LeaderboardPage
	c.expect(c.call("GET", board+"?limit=1000", viewer, nil), http.StatusOK, "", &final)
	if !reflect.DeepEqual(final.Entries, live.Entries) || final.Status != gen.Finished || final.ParticipantCount != 4 {
		t.Errorf("archived leaderboard differs from the live one:\nlive  %+v\nfinal %+v", live, final)
	}
	var stored int
	if err := env.Postgres.QueryRow(ctx, `SELECT count(*) FROM answers WHERE quiz_code = $1`, string(code)).Scan(&stored); err != nil || stored != accepted {
		t.Errorf("stored %d answers, accepted %d (%v)", stored, accepted, err)
	}
	c.expect(c.call("POST", start, host, nil), http.StatusNotFound, gen.ErrorCodeUnknownQuiz, nil)
}

// checkBoard asserts totals match the points each answer returned, and ranks and order follow FR-24.
func checkBoard(t *testing.T, p gen.LeaderboardPage, totals map[string]int, status gen.QuizStatus) {
	t.Helper()
	if p.Status != status || p.ParticipantCount != len(totals) || len(p.Entries) != len(totals) {
		t.Fatalf("page %+v, want %s with %d entries", p, status, len(totals))
	}
	for i, e := range p.Entries {
		if e.Score != totals[e.ParticipantId] {
			t.Errorf("%s: score %d, answers sum to %d", e.ParticipantId, e.Score, totals[e.ParticipantId])
		}
		want := 1
		for _, other := range totals {
			if other > e.Score {
				want++
			}
		}
		if e.Rank != want {
			t.Errorf("%s: rank %d, want %d", e.ParticipantId, e.Rank, want)
		}
		if i > 0 && p.Entries[i-1].Score < e.Score {
			t.Errorf("entry %d out of order", i)
		}
	}
}

// runJobs processes flush and final jobs the way the worker does, until the room is released.
func runJobs(t *testing.T, code quiz.Code) {
	t.Helper()
	ctx := context.Background()
	live := history.NewRedisLiveStore(env.Redis)
	svc := history.NewService(live, history.NewPostgresStore(env.Postgres), history.Options{
		TTL: time.Hour, FinalRetryDelay: 50 * time.Millisecond,
		OnMismatch: func(c quiz.Code, m []history.Mismatch) { t.Errorf("reconciliation mismatch for %s: %+v", c, m) },
	})
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
	t.Fatal("room not released after 100 job rounds")
}

// A Redis outage returns 503 + Retry-After and leaves nothing behind; reads never fall back to
// PostgreSQL, which would report a running quiz as expired (TRD §9.4).
func TestOutage_Redis(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	rdb := redisx.NewClient(env.RedisViaProxy.Options().Addr, "")
	defer func() { _ = rdb.Close() }()
	c := client{t: t, h: api(t, rdb, env.Postgres)}
	host := c.token("host_1", gen.Host)
	var q gen.Quiz
	c.expect(c.call("POST", "/api/v1/quizzes", host, map[string]any{"questionSetId": "demo-quick"}), http.StatusCreated, "", &q)

	proxy := env.Proxy(t, testenv.RedisProxy)
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	r := c.call("POST", "/api/v1/quizzes", host, map[string]any{"questionSetId": "demo-quick"})
	c.expect(r, http.StatusServiceUnavailable, gen.ErrorCodeUnavailable, nil)
	if r.header.Get("Retry-After") == "" {
		t.Error("503 without Retry-After")
	}
	var rows int
	if err := env.Postgres.QueryRow(ctx, `SELECT count(*) FROM quizzes`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("%d quiz rows, want only the one created before the outage (the failed create rolled back)", rows)
	}
	for _, path := range []string{"/api/v1/quizzes/" + q.Code, "/api/v1/quizzes/" + q.Code + "/leaderboard"} {
		c.expect(c.call("GET", path, host, nil), http.StatusServiceUnavailable, gen.ErrorCodeUnavailable, nil)
	}
	if r := c.call("GET", "/readyz", "", nil); r.status != http.StatusServiceUnavailable {
		t.Errorf("readyz %d during the outage", r.status)
	}

	if err := proxy.Enable(); err != nil {
		t.Fatal(err)
	}
	c.expect(c.call("POST", "/api/v1/quizzes", host, map[string]any{"questionSetId": "demo-quick"}), http.StatusCreated, "", nil)
	c.expect(c.call("GET", "/readyz", "", nil), http.StatusOK, "", nil)
}

// A PostgreSQL outage blocks creation but not live quizzes, which run from Redis (TRD §9.4).
func TestOutage_Postgres(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, env.PostgresViaProxyDSN+"&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	c := client{t: t, h: api(t, env.Redis, pool)}
	host := c.token("host_1", gen.Host)
	var q gen.Quiz
	c.expect(c.call("POST", "/api/v1/quizzes", host, map[string]any{"questionSetId": "demo-quick"}), http.StatusCreated, "", &q)
	if _, err := session.NewRedisRepository(env.Redis).Join(ctx, session.JoinInput{Code: quiz.Code(q.Code), ParticipantID: "u_1", DisplayName: "Rina", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	rooms := func() int { return len(env.Redis.Keys(ctx, "quiz:{*}:room").Val()) }
	before := rooms()

	proxy := env.Proxy(t, testenv.PostgresProxy)
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxy.Enable() }()
	c.expect(c.call("POST", "/api/v1/quizzes", host, map[string]any{"questionSetId": "demo-quick"}), http.StatusServiceUnavailable, gen.ErrorCodeUnavailable, nil)
	if rooms() != before {
		t.Error("a Redis room was created although the PostgreSQL insert failed")
	}
	var live gen.Quiz
	c.expect(c.call("GET", "/api/v1/quizzes/"+q.Code, host, nil), http.StatusOK, "", &live)
	c.expect(c.call("POST", "/api/v1/quizzes/"+q.Code+"/start", host, nil), http.StatusAccepted, "", nil)
	c.expect(c.call("GET", "/api/v1/quizzes/"+q.Code+"/leaderboard", host, nil), http.StatusOK, "", nil)
	if r := c.call("GET", "/readyz", "", nil); r.status != http.StatusServiceUnavailable {
		t.Errorf("readyz %d during the outage", r.status)
	}
}

// A slow Redis fails fast with 503 instead of holding the request (TRD §9.2).
func TestSlowRedis_FailsFast(t *testing.T) {
	env.Reset(t)
	rdb := redisx.NewClient(env.RedisViaProxy.Options().Addr, "")
	defer func() { _ = rdb.Close() }()
	c := client{t: t, h: api(t, rdb, env.Postgres)}
	proxy := env.Proxy(t, testenv.RedisProxy)
	if _, err := proxy.AddToxic("slow", "latency", "downstream", 1, toxiclient.Attributes{"latency": 3000}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	c.expect(c.call("GET", "/api/v1/quizzes/K7Q2MX", c.token("u_1", gen.Participant), nil), http.StatusServiceUnavailable, gen.ErrorCodeUnavailable, nil)
	if d := time.Since(started); d > 2500*time.Millisecond {
		t.Errorf("took %v; want the Redis read timeout (~1s), not the full latency", d)
	}
}

// An orphan Redis room (a create whose commit failed after Redis succeeded) holds its code
// until it expires; creation must skip that code, not fail (TRD §5.2, D16).
func TestCreate_SkipsCodesHeldByOrphanRooms(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	qr := quiz.NewRedisRepository(env.Redis)
	if err := redisx.LoadScripts(ctx, env.Redis, quiz.Scripts()...); err != nil {
		t.Fatal(err)
	}
	if _, err := qr.CreateRoom(ctx, quiz.CreateRoomInput{Code: "222222", QuestionSetID: "demo-quick", HostID: "ghost",
		QuestionIDs: []quiz.QuestionID{"dq-01"}, WindowMs: 10_000, RevealMs: 3_000, LobbyTimeoutMs: 60_000, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	svc := quiz.NewService(qr, quiz.NewPostgresStore(env.Postgres), quiz.Settings{
		DefaultWindow: 15 * time.Second, DefaultReveal: 5 * time.Second, LobbyTimeout: time.Minute, DataTTL: time.Hour,
		CodeAttempts: 3, CodeSource: bytes.NewReader(append(bytes.Repeat([]byte{0}, 6), bytes.Repeat([]byte{1}, 6)...)),
	}, slog.New(slog.DiscardHandler))
	got, err := svc.Create(ctx, quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick"})
	if err != nil || got.Code != "333333" {
		t.Fatalf("got %+v, %v; want code 333333", got, err)
	}
	var codes []string
	rows, _ := env.Postgres.Query(ctx, `SELECT code FROM quizzes ORDER BY code`)
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		codes = append(codes, s)
	}
	if strings.Join(codes, ",") != "333333" {
		t.Errorf("quiz rows %v; want only 333333 (the collision rolled back)", codes)
	}
	if host := env.Redis.HGet(ctx, redisx.RoomKey("222222"), "host_id").Val(); host != "ghost" {
		t.Errorf("orphan room overwritten: host %q", host)
	}
}

// The operational endpoints answer, and metrics carry no per-quiz labels (task-23, NFR-29).
func TestOperationalEndpoints(t *testing.T) {
	env.Reset(t)
	srv := httptest.NewServer(api(t, env.Redis, env.Postgres))
	defer srv.Close()
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
	metricstest.AssertBoundedLabels(t, metricstest.Scrape(t, srv.URL))
}
