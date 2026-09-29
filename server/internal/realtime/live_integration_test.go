//go:build integration

package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/realtime"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var tenv *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &tenv)) }

func fastBackoff() realtime.SubscriberOptions {
	return realtime.SubscriberOptions{
		HealthInterval: 200 * time.Millisecond,
		Backoff:        retry.Policy{Base: 20 * time.Millisecond, Cap: 200 * time.Millisecond},
	}
}

// hooks records what the subscriber delivers; rooms says which rooms "have connections".
type hooks struct {
	mu       sync.Mutex
	rooms    map[quiz.Code]bool
	messages map[quiz.Code][]string
	resubs   map[quiz.Code]int
}

func newHooks() *hooks {
	return &hooks{rooms: map[quiz.Code]bool{}, messages: map[quiz.Code][]string{}, resubs: map[quiz.Code]int{}}
}
func (h *hooks) Has(c quiz.Code) bool { h.mu.Lock(); defer h.mu.Unlock(); return h.rooms[c] }
func (h *hooks) set(c quiz.Code, v bool) {
	h.mu.Lock()
	h.rooms[c] = v
	h.mu.Unlock()
}
func (h *hooks) OnMessage(c quiz.Code, p string) {
	h.mu.Lock()
	h.messages[c] = append(h.messages[c], p)
	h.mu.Unlock()
}
func (h *hooks) OnResubscribed(c quiz.Code) { h.mu.Lock(); h.resubs[c]++; h.mu.Unlock() }
func (h *hooks) got(c quiz.Code) ([]string, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.messages[c]...), h.resubs[c]
}

func runSubscriber(t *testing.T, rdb redis.UniversalClient, h realtime.SubscriberHooks) *realtime.Subscriber {
	t.Helper()
	sub := realtime.NewSubscriber(rdb, h, fastBackoff())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sub.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("subscriber did not stop")
		}
	})
	return sub
}

func numsub(t *testing.T, code quiz.Code) int64 {
	t.Helper()
	ch := redisx.RoomChannel(string(code))
	return tenv.Redis.PubSubNumSub(context.Background(), ch).Val()[ch]
}

func waitUntil(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", within, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ensure(t *testing.T, sub *realtime.Subscriber, code quiz.Code) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sub.Ensure(ctx, code); err != nil {
		t.Fatalf("ensure %s: %v", code, err)
	}
}

func TestSubscriber_EnsureReturnsOnlyOnceRedisConfirmed(t *testing.T) {
	tenv.Reset(t)
	h := newHooks()
	sub := runSubscriber(t, tenv.Redis, h)
	for _, code := range []quiz.Code{"AAAAAA", "BBBBBB", "CCCCCC"} {
		h.set(code, true)
		ensure(t, sub, code)
		// Checked immediately: a publish from here on must reach this gateway (TRD §7.3).
		if n := numsub(t, code); n != 1 {
			t.Fatalf("%s: %d subscribers right after Ensure returned, want 1", code, n)
		}
	}
	tenv.Redis.Publish(context.Background(), redisx.RoomChannel("BBBBBB"), `{"t":"lb","v":1}`)
	waitUntil(t, "the event", 2*time.Second, func() bool { m, _ := h.got("BBBBBB"); return len(m) == 1 })
	if m, _ := h.got("AAAAAA"); len(m) != 0 {
		t.Errorf("another room got the event: %v", m)
	}
}

func TestSubscriber_EnsureWaitsForASlowConfirmation(t *testing.T) {
	// With commands to Redis delayed, returning before the confirmation would leave no subscriber yet.
	tenv.Reset(t)
	h := newHooks()
	sub := runSubscriber(t, tenv.RedisViaProxy, h)
	h.set("AAAAAA", true)
	ensure(t, sub, "AAAAAA") // the connection is up
	if _, err := tenv.Proxy(t, testenv.RedisProxy).AddToxic("slow", "latency", "upstream", 1, toxiclient.Attributes{"latency": 300}); err != nil {
		t.Fatal(err)
	}
	h.set("BBBBBB", true)
	begin := time.Now()
	ensure(t, sub, "BBBBBB")
	if n := numsub(t, "BBBBBB"); n != 1 {
		t.Fatalf("Ensure returned after %s with %d subscribers; it must wait for Redis", time.Since(begin), n)
	}
}

func TestSubscriber_DropUnsubscribesOnlyAnEmptyRoom(t *testing.T) {
	tenv.Reset(t)
	h := newHooks()
	sub := runSubscriber(t, tenv.Redis, h)
	h.set("AAAAAA", true)
	ensure(t, sub, "AAAAAA")

	sub.Drop("AAAAAA") // the room still has a connection (it was re-entered meanwhile)
	time.Sleep(100 * time.Millisecond)
	if n := numsub(t, "AAAAAA"); n != 1 {
		t.Fatalf("dropped a room that still has connections (numsub %d)", n)
	}
	h.set("AAAAAA", false)
	sub.Drop("AAAAAA")
	waitUntil(t, "the unsubscribe", 2*time.Second, func() bool { return numsub(t, "AAAAAA") == 0 })
}

func TestSubscriber_ChurnEndsSubscribedExactlyToRoomsWithConnections(t *testing.T) {
	tenv.Reset(t)
	h := newHooks()
	sub := runSubscriber(t, tenv.Redis, h)
	codes := []quiz.Code{"AAAAAA", "BBBBBB", "CCCCCC"}
	var locks [3]sync.Mutex // one room changes membership at a time, as the registry's shard lock does
	var refs [3]int
	var wg sync.WaitGroup
	for g := range 12 {
		wg.Go(func() {
			for i := range 40 {
				k := (g + i) % 3
				locks[k].Lock()
				refs[k]++
				h.set(codes[k], true)
				locks[k].Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				if err := sub.Ensure(ctx, codes[k]); err != nil {
					t.Error(err)
				}
				cancel()
				locks[k].Lock()
				refs[k]--
				last := refs[k] == 0
				if last {
					h.set(codes[k], false)
				}
				locks[k].Unlock()
				if last {
					sub.Drop(codes[k])
				}
			}
		})
	}
	wg.Wait()
	for _, code := range codes {
		waitUntil(t, string(code)+" to be unsubscribed", 2*time.Second, func() bool { return numsub(t, code) == 0 })
	}
	h.set("BBBBBB", true)
	ensure(t, sub, "BBBBBB")
	if numsub(t, "BBBBBB") != 1 {
		t.Error("room with a connection not subscribed after the churn")
	}
}

func TestSubscriber_EnsureFailsWhileRedisIsDown(t *testing.T) {
	tenv.Reset(t)
	if err := tenv.Proxy(t, testenv.RedisProxy).Disable(); err != nil {
		t.Fatal(err)
	}
	h := newHooks()
	sub := runSubscriber(t, tenv.RedisViaProxy, h)
	h.set("AAAAAA", true)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := sub.Ensure(ctx, "AAAAAA"); err == nil {
		t.Fatal("Ensure succeeded with Redis unreachable")
	}
}

func TestSubscriber_OutageResubscribesEveryRoomAndSignalsEachOnce(t *testing.T) {
	tenv.Reset(t)
	proxy := tenv.Proxy(t, testenv.RedisProxy)
	h := newHooks()
	sub := runSubscriber(t, tenv.RedisViaProxy, h)
	for _, code := range []quiz.Code{"AAAAAA", "BBBBBB"} {
		h.set(code, true)
		ensure(t, sub, code)
	}
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond) // several failed reconnects
	if err := proxy.Enable(); err != nil {
		t.Fatal(err)
	}
	for _, code := range []quiz.Code{"AAAAAA", "BBBBBB"} {
		waitUntil(t, string(code)+" to be resubscribed", 5*time.Second, func() bool { _, n := h.got(code); return n >= 1 })
	}
	time.Sleep(300 * time.Millisecond)
	for _, code := range []quiz.Code{"AAAAAA", "BBBBBB"} {
		if _, n := h.got(code); n != 1 {
			t.Errorf("%s resubscribe signalled %d times, want 1 (one snapshot push per room)", code, n)
		}
	}
	tenv.Redis.Publish(context.Background(), redisx.RoomChannel("AAAAAA"), `{"t":"lb","v":1}`)
	waitUntil(t, "an event after the outage", 2*time.Second, func() bool { m, _ := h.got("AAAAAA"); return len(m) == 1 })
}

func TestSubscriber_IdleHealthyConnectionStaysUp(t *testing.T) {
	// A quiet room is normal; the health check must answer it with a ping, not a reconnect.
	tenv.Reset(t)
	h := newHooks()
	sub := runSubscriber(t, tenv.Redis, h)
	h.set("AAAAAA", true)
	ensure(t, sub, "AAAAAA")
	time.Sleep(1200 * time.Millisecond) // six health intervals with no traffic
	if _, n := h.got("AAAAAA"); n != 0 {
		t.Errorf("an idle, healthy connection was replaced %d times", n)
	}
}

func TestSubscriber_DetectsAConnectionThatSilentlyStopped(t *testing.T) {
	// A half-open connection never errors; only the health-check ping notices it.
	tenv.Reset(t)
	proxy := tenv.Proxy(t, testenv.RedisProxy)
	h := newHooks()
	sub := runSubscriber(t, tenv.RedisViaProxy, h)
	h.set("AAAAAA", true)
	ensure(t, sub, "AAAAAA")

	if _, err := proxy.AddToxic("blackhole", "timeout", "downstream", 1, toxiclient.Attributes{"timeout": 0}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond) // > 2 health intervals: an idle wait, then an unanswered ping
	if err := proxy.RemoveToxic("blackhole"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the resubscribe after the silent stall", 5*time.Second, func() bool { _, n := h.got("AAAAAA"); return n >= 1 })
	tenv.Redis.Publish(context.Background(), redisx.RoomChannel("AAAAAA"), `{"t":"lb","v":1}`)
	waitUntil(t, "an event after the stall", 2*time.Second, func() bool { m, _ := h.got("AAAAAA"); return len(m) >= 1 })
}

// --- end to end: real scripts, question cache, subscriber, and sockets ---

const code = quiz.Code("K7Q2MX")

type live struct {
	t       *testing.T
	srv     *httptest.Server
	tokens  *auth.Tokens
	quizzes *quiz.RedisRepository
	hub     *realtime.Hub
}

// joiner handles join and watch the way the gateway's handler will (task-20): Enter first, then the snapshot.
type joiner struct {
	hub      *realtime.Hub
	sessions *session.RedisRepository
	quizzes  *quiz.RedisRepository
	cache    *quiz.Cache
}

func (j *joiner) Handle(ctx context.Context, c *realtime.Conn, m protocol.ClientMessage) {
	var code quiz.Code
	switch p := m.Payload.(type) {
	case protocol.Join:
		code = quiz.Code(p.QuizCode)
	case protocol.Watch:
		code = quiz.Code(p.QuizCode)
	default:
		return
	}
	rec, err := j.quizzes.Room(ctx, code)
	if err != nil {
		c.ReplyError(m.ID, protocol.CodeUnknownQuiz, err.Error(), 0)
		return
	}
	member := realtime.Member{Code: code, SetID: rec.QuestionSetID}
	if p, ok := m.Payload.(protocol.Join); ok {
		member.ParticipantID, member.DisplayName = quiz.ParticipantID(c.Claims().ParticipantID), p.DisplayName
	}
	if err := j.hub.Enter(ctx, c, member); err != nil {
		c.ReplyError(m.ID, protocol.CodeServerBusy, err.Error(), time.Second)
		return
	}
	if member.ParticipantID == "" {
		frame, _ := j.hub.Snapshot(ctx, c)
		c.SendBytes(frame)
		return
	}
	res, err := j.sessions.Join(ctx, session.JoinInput{Code: code, ParticipantID: member.ParticipantID, DisplayName: member.DisplayName, TTL: time.Hour})
	if err != nil {
		c.ReplyError(m.ID, protocol.CodeServerBusy, err.Error(), time.Second)
		return
	}
	set, _ := j.cache.Get(rec.QuestionSetID)
	view := session.RoomView{Room: res.Room, ParticipantCount: res.ParticipantCount, Top: res.Top, ServerTime: res.ServerTime}
	c.Reply(m.ID, protocol.TypeSnapshot, realtime.BuildSnapshot(view, set, member, session.Standing{Found: true, Score: res.Score, Rank: res.Rank}))
}
func (j *joiner) Snapshot(ctx context.Context, c *realtime.Conn) ([]byte, error) {
	return j.hub.Snapshot(ctx, c)
}
func (j *joiner) Closed(c *realtime.Conn) { j.hub.Leave(c) }

func startLive(t *testing.T) *live {
	t.Helper()
	tenv.Reset(t)
	ctx := context.Background()
	scripts := append(append(append(quiz.Scripts(), session.Scripts()...), scoring.Scripts()...), leaderboard.Scripts()...)
	if err := redisx.LoadScripts(ctx, tenv.Redis, scripts...); err != nil {
		t.Fatal(err)
	}
	quizzes := quiz.NewRedisRepository(tenv.Redis)
	_, err := quizzes.CreateRoom(ctx, quiz.CreateRoomInput{Code: code, QuestionSetID: "demo-quick", HostID: "host_1",
		QuestionIDs: []quiz.QuestionID{"dq-01", "dq-02"}, WindowMs: 15_000, RevealMs: 3_000, LobbyTimeoutMs: 1_800_000, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	cache := quiz.NewCache(quiz.NewPostgresStore(tenv.Postgres), quiz.CacheOptions{MaxSets: 8,
		Retry: retry.New(retry.Policy{Base: 10 * time.Millisecond, Cap: 100 * time.Millisecond, Budget: 2 * time.Second}, nil), AttemptTimeout: time.Second})
	sessions := session.NewRedisRepository(tenv.Redis)
	hub := realtime.NewHub(cache, sessions, realtime.HubOptions{Log: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	sub := runSubscriber(t, tenv.Redis, hub)
	hub.Attach(sub)

	tokens := &auth.Tokens{Key: []byte(strings.Repeat("k", 32)), TTL: time.Hour}
	gw := realtime.New(realtime.Options{MaxMessageBytes: 4096, SendQueueSize: 64, PingInterval: 25 * time.Second, PongTimeout: time.Minute,
		RatePerSec: 50, RateBurst: 50, AdmissionPerSec: 1000, MaxConnections: 1000}, tokens,
		&joiner{hub: hub, sessions: sessions, quizzes: quizzes, cache: cache}, nil)
	srv := httptest.NewServer(gw)
	t.Cleanup(srv.Close)
	return &live{t: t, srv: srv, tokens: tokens, quizzes: quizzes, hub: hub}
}

type client struct {
	t  *testing.T
	ws *websocket.Conn
}

func (l *live) connect(id string, role auth.Role, first string) *client {
	l.t.Helper()
	tok, _, err := l.tokens.Issue(id, role)
	if err != nil {
		l.t.Fatal(err)
	}
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(l.srv.URL, "http")+"/ws?token="+tok, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		l.t.Fatal(err)
	}
	l.t.Cleanup(func() { _ = ws.Close() })
	c := &client{t: l.t, ws: ws}
	if err := ws.WriteMessage(websocket.TextMessage, []byte(first)); err != nil {
		l.t.Fatal(err)
	}
	c.expect(protocol.TypeSnapshot)
	return c
}

// expect reads until a message of type typ arrives; every frame must satisfy the contract.
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
			d, _ := m["data"].(map[string]any)
			return d
		}
		if m["type"] == "error" {
			c.t.Fatalf("error while waiting for %s: %s", typ, b)
		}
	}
}

// expectNext fails unless the very next message is of type typ.
func (c *client) expectNext(typ protocol.Type) map[string]any {
	c.t.Helper()
	_ = c.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, b, err := c.ws.ReadMessage()
	if err != nil {
		c.t.Fatalf("waiting for %s: %v", typ, err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["type"] != string(typ) {
		c.t.Fatalf("next message is %s, want %s: %s", m["type"], typ, b)
	}
	d, _ := m["data"].(map[string]any)
	return d
}

func forceDue(t *testing.T, fields ...any) {
	t.Helper()
	ctx := context.Background()
	past := tenv.Redis.Time(ctx).Val().UnixMilli() - 1
	tenv.Redis.HSet(ctx, redisx.RoomKey(string(code)), append([]any{"next_at", past}, fields...)...)
}

func (l *live) transition(want quiz.Status) {
	l.t.Helper()
	res, err := l.quizzes.ApplyTransition(context.Background(), code)
	if err != nil || res.Outcome != quiz.Applied || res.Status != want {
		l.t.Fatalf("transition: %+v %v, want %s", res, err, want)
	}
}

func TestLive_RoomEventsReachClientsAsContractMessages(t *testing.T) {
	l := startLive(t)
	ctx := context.Background()
	rina := l.connect("u_1", auth.RoleParticipant, `{"v":1,"type":"join","id":"j1","data":{"quizCode":"K7Q2MX","displayName":"Rina"}}`)
	budi := l.connect("u_2", auth.RoleParticipant, `{"v":1,"type":"join","id":"j2","data":{"quizCode":"K7Q2MX","displayName":"Budi"}}`)
	host := l.connect("host_1", auth.RoleHost, `{"v":1,"type":"watch","id":"w1","data":{"quizCode":"K7Q2MX"}}`)
	all := []*client{rina, budi, host}

	if err := l.quizzes.Start(ctx, code, "host_1"); err != nil {
		t.Fatal(err)
	}
	forceDue(t)
	l.transition(quiz.StatusQuestionOpen)
	for _, c := range all {
		q := c.expect(protocol.TypeQuestion)["question"].(map[string]any)
		if q["questionId"] != "dq-01" || q["prompt"] != "Choose the synonym of 'rapid'" || len(q["options"].([]any)) != 4 {
			t.Errorf("question %v", q)
		}
	}

	answers := scoring.NewRedisRepository(tenv.Redis, scoring.RedisOptions{OnlineWindow: 30 * time.Second, TTL: time.Hour})
	if _, err := answers.RecordAnswer(ctx, scoring.AnswerInput{Code: code, ParticipantID: "u_1", QuestionID: "dq-01", OptionID: "dq-01-b", Correct: true}); err != nil {
		t.Fatal(err)
	}
	last, err := answers.RecordAnswer(ctx, scoring.AnswerInput{Code: code, ParticipantID: "u_2", QuestionID: "dq-01", OptionID: "dq-01-a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all { // both online participants answered: the close time moves to now (FR-5)
		q := c.expect(protocol.TypeQuestion)["question"].(map[string]any)
		if int64(q["closeAt"].(float64)) != last.ReceivedAt {
			t.Errorf("early close: closeAt %v, want %d", q["closeAt"], last.ReceivedAt)
		}
	}

	l.transition(quiz.StatusQuestionClosed)
	rec, _ := l.quizzes.Room(ctx, code)
	for _, c := range all {
		d := c.expect(protocol.TypeQuestionClosed)
		if d["correctOptionId"] != "dq-01-b" || int64(d["nextTransitionAt"].(float64)) != rec.NextTransitionAt {
			t.Errorf("question_closed %v, want next transition %d", d, rec.NextTransitionAt)
		}
	}
	if r := rina.expect(protocol.TypeRank); r["rank"] != 1.0 || r["participantCount"] != 2.0 || r["score"].(float64) <= 100 {
		t.Errorf("Rina's rank %v", r)
	}
	if r := budi.expect(protocol.TypeRank); r["rank"] != 2.0 || r["score"] != 0.0 {
		t.Errorf("Budi's rank %v", r)
	}

	if _, err := leaderboard.NewRedisRepository(tenv.Redis).PublishSnapshot(ctx, code); err != nil {
		t.Fatal(err)
	}
	host.expectNext(protocol.TypeLeaderboard) // the host watches; it gets no rank of its own
	for _, c := range []*client{rina, budi} {
		top := c.expect(protocol.TypeLeaderboard)["top"].([]any)
		if len(top) != 2 || top[0].(map[string]any)["displayName"] != "Rina" || top[0].(map[string]any)["rank"] != 1.0 {
			t.Errorf("leaderboard %v", top)
		}
	}

	forceDue(t)
	l.transition(quiz.StatusQuestionOpen) // dq-02
	forceDue(t, "close_at", int64(0))
	l.transition(quiz.StatusQuestionClosed)
	forceDue(t)
	l.transition(quiz.StatusFinished)
	for _, c := range all {
		d := c.expect(protocol.TypeQuizFinished)
		if d["participantCount"] != 2.0 || len(d["finalTop"].([]any)) != 2 {
			t.Errorf("quiz_finished %v", d)
		}
	}
}

func TestLive_SecondConnectionForTheSameParticipantReplacesTheFirst(t *testing.T) {
	l := startLive(t)
	first := l.connect("u_1", auth.RoleParticipant, `{"v":1,"type":"join","id":"a","data":{"quizCode":"K7Q2MX","displayName":"Rina"}}`)
	l.connect("u_1", auth.RoleParticipant, `{"v":1,"type":"join","id":"b","data":{"quizCode":"K7Q2MX","displayName":"Rina"}}`)
	_ = first.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := first.ws.ReadMessage()
		if err == nil {
			continue
		}
		var ce *websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != realtime.CloseReplaced {
			t.Errorf("first connection ended with %v, want close %d", err, realtime.CloseReplaced)
		}
		break
	}
}
// Stopping must not wait out a blocked read; with the 15 s default it would stall every shutdown.
func TestSubscriber_StopsPromptlyWithTheDefaultHealthInterval(t *testing.T) {
	tenv.Reset(t)
	sub := realtime.NewSubscriber(tenv.Redis, newHooks(), realtime.SubscriberOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sub.Run(ctx); close(done) }()
	ensure(t, sub, "AAAAAA")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber still running 2 s after cancel")
		<-done
	}
}
