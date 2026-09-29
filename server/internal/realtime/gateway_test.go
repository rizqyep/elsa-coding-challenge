package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/realtime"
)

const testOrigin = "http://localhost:5173"

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

// recorder is a Handler that records dispatched messages and can be made to block.
type recorder struct {
	mu       sync.Mutex
	msgs     []protocol.ClientMessage
	closed   atomic.Int64
	inFlight atomic.Int64
	maxIn    atomic.Int64
	block    chan struct{} // when set, Handle waits on it
}

func (r *recorder) Handle(_ context.Context, c *realtime.Conn, m protocol.ClientMessage) {
	n := r.inFlight.Add(1)
	defer r.inFlight.Add(-1)
	for {
		old := r.maxIn.Load()
		if n <= old || r.maxIn.CompareAndSwap(old, n) {
			break
		}
	}
	r.mu.Lock()
	r.msgs = append(r.msgs, m)
	block := r.block
	r.mu.Unlock()
	if block != nil {
		<-block
	}
	c.Reply(m.ID, protocol.TypePong, protocol.Pong{ClientTime: 1, ServerTime: 2})
}

func (r *recorder) Snapshot(context.Context, *realtime.Conn) ([]byte, error) { return nil, nil }
func (r *recorder) Closed(*realtime.Conn)                                    { r.closed.Add(1) }

func (r *recorder) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.msgs) }

type env struct {
	t      *testing.T
	srv    *httptest.Server
	gw     *realtime.Gateway
	h      *recorder
	clock  *clock
	tokens *auth.Tokens
}

func testOptions(c *clock) realtime.Options {
	return realtime.Options{
		AllowedOrigins:   []string{testOrigin},
		MaxMessageBytes:  4096,
		ReadBufferBytes:  1024,
		WriteBufferBytes: 1024,
		SendQueueSize:    64,
		PingInterval:     25 * time.Second,
		PongTimeout:      60 * time.Second,
		RatePerSec:       20,
		RateBurst:        40,
		AdmissionPerSec:  1000,
		MaxConnections:   1000,
		Now:              c.Now,
		RandIntN:         func(n int) int { return n - 1 },
	}
}

func newEnv(t *testing.T, mod func(*realtime.Options)) *env {
	t.Helper()
	c := &clock{now: time.Unix(1_800_000_000, 0)}
	opts := testOptions(c)
	if mod != nil {
		mod(&opts)
	}
	tokens := &auth.Tokens{Key: []byte(strings.Repeat("k", 32)), TTL: time.Hour, Now: c.Now}
	h := &recorder{}
	gw := realtime.New(opts, tokens, h, nil)
	srv := httptest.NewServer(gw)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, gw: gw, h: h, clock: c, tokens: tokens}
}

func (e *env) token(role auth.Role) string {
	e.t.Helper()
	tok, _, err := e.tokens.Issue("p-"+strconv.FormatInt(time.Now().UnixNano(), 36), role)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok
}

func (e *env) url(token string) string {
	return "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/ws?token=" + token
}

// rejection is what a refused handshake answered.
type rejection struct {
	StatusCode int
	Header     http.Header
}

// dial returns the connection, or the status and headers of a rejected handshake.
func (e *env) dial(token, origin string) (*websocket.Conn, *rejection, error) {
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	c, resp, err := websocket.DefaultDialer.Dial(e.url(token), h)
	if resp == nil {
		return c, nil, err
	}
	_ = resp.Body.Close()
	if err != nil {
		return nil, &rejection{resp.StatusCode, resp.Header}, err
	}
	return c, nil, nil
}

func (e *env) mustDial() *websocket.Conn {
	e.t.Helper()
	c, _, err := e.dial(e.token(auth.RoleParticipant), testOrigin)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = c.Close() })
	return c
}

func ping(id string) []byte {
	return []byte(`{"v":1,"type":"ping","id":"` + id + `","data":{"clientTime":1}}`)
}

// readFrame reads one data message, failing the test after two seconds.
func readFrame(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, b, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// closeCode reads until the connection closes and returns the close code.
func closeCode(t *testing.T, c *websocket.Conn, within time.Duration) int {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(within))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		var ce *websocket.CloseError
		if errors.As(err, &ce) {
			return ce.Code
		}
		t.Fatalf("connection ended without a close frame: %v", err)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHandshake_RejectsBeforeUpgrade(t *testing.T) {
	e := newEnv(t, nil)
	good := e.token(auth.RoleParticipant)
	expired := func() string {
		old := &auth.Tokens{Key: e.tokens.Key, TTL: time.Minute, Now: func() time.Time { return e.clock.Now().Add(-time.Hour) }}
		tok, _, err := old.Issue("p-old", auth.RoleParticipant)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}()

	cases := []struct {
		name   string
		token  string
		origin string
		want   int
	}{
		{"origin not allowed", good, "http://evil.example", http.StatusForbidden},
		{"missing token", "", testOrigin, http.StatusUnauthorized},
		{"garbage token", "not-a-jwt", testOrigin, http.StatusUnauthorized},
		{"expired token", expired, testOrigin, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp, err := e.dial(tc.token, tc.origin)
			if err == nil || resp == nil {
				t.Fatalf("handshake accepted, want %d", tc.want)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("status %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
	if n := e.gw.Active(); n != 0 {
		t.Errorf("active connections %d after only rejections, want 0", n)
	}
}

func TestHandshake_NoOriginIsAllowed(t *testing.T) {
	// Non-browser clients (simulator, test kit) send no Origin; the token still authenticates them.
	e := newEnv(t, nil)
	c, _, err := e.dial(e.token(auth.RoleParticipant), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

func TestHandshake_AdmissionLimitReturns503WithJitteredRetryAfter(t *testing.T) {
	jitter := 0
	e := newEnv(t, func(o *realtime.Options) {
		o.AdmissionPerSec = 2
		o.RandIntN = func(n int) int { jitter = (jitter + 1) % n; return jitter }
	})
	e.mustDial()
	e.mustDial()

	seen := map[string]bool{}
	for range 6 {
		_, resp, err := e.dial(e.token(auth.RoleParticipant), testOrigin)
		if err == nil {
			t.Fatal("third connection within a second was admitted")
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status %d, want 503", resp.StatusCode)
		}
		secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil || secs < 1 || secs > 3 {
			t.Fatalf("Retry-After %q, want 1–3 s (wait + jitter)", resp.Header.Get("Retry-After"))
		}
		seen[resp.Header.Get("Retry-After")] = true
	}
	if len(seen) < 2 {
		t.Errorf("every rejection got the same Retry-After %v; retries would arrive together", seen)
	}
	if n := e.gw.Active(); n != 2 {
		t.Errorf("active %d, want 2: rejections must not count", n)
	}

	e.clock.Advance(time.Second)
	e.mustDial()
}

func TestHandshake_ConnectionCap(t *testing.T) {
	e := newEnv(t, func(o *realtime.Options) { o.MaxConnections = 2 })
	first := e.mustDial()
	e.mustDial()

	_, resp, err := e.dial(e.token(auth.RoleParticipant), testOrigin)
	if err == nil || resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over the cap: err=%v resp=%v, want 503 with Retry-After", err, resp)
	}

	_ = first.Close()
	eventually(t, "the closed connection to free its slot", func() bool { return e.gw.Active() == 1 })
	e.mustDial()
}

func TestHandshake_BurstNeverExceedsCap(t *testing.T) {
	e := newEnv(t, func(o *realtime.Options) { o.MaxConnections = 50 })
	var admitted atomic.Int64
	var wg sync.WaitGroup
	conns := make(chan *websocket.Conn, 200)
	for range 200 {
		wg.Go(func() {
			c, _, err := e.dial(e.token(auth.RoleParticipant), testOrigin)
			if err == nil {
				admitted.Add(1)
				conns <- c
			}
		})
	}
	wg.Wait()
	close(conns)
	if admitted.Load() != 50 || e.gw.Active() != 50 {
		t.Errorf("admitted %d, active %d; want exactly the cap of 50", admitted.Load(), e.gw.Active())
	}
	for c := range conns {
		_ = c.Close()
	}
	eventually(t, "all connections to close", func() bool { return e.gw.Active() == 0 })
}

func TestConn_OversizedMessageClosesWith1009(t *testing.T) {
	e := newEnv(t, nil)
	c := e.mustDial()
	big := `{"v":1,"type":"ping","id":"x","data":{"clientTime":1},"pad":"` + strings.Repeat("a", 5000) + `"}`
	if err := c.WriteMessage(websocket.TextMessage, []byte(big)); err != nil {
		t.Fatal(err)
	}
	if code := closeCode(t, c, 2*time.Second); code != websocket.CloseMessageTooBig {
		t.Errorf("close code %d, want 1009", code)
	}
	if e.h.count() != 0 {
		t.Error("oversized message was dispatched")
	}
}

func TestConn_InvalidMessagesGetAnErrorAndKeepTheConnection(t *testing.T) {
	e := newEnv(t, nil)
	c := e.mustDial()
	cases := []struct {
		frame []byte
		kind  int
		want  protocol.ErrorCode
	}{
		{[]byte(`not json`), websocket.TextMessage, protocol.CodeInvalidMessage},
		{[]byte(`{"v":2,"type":"ping","data":{}}`), websocket.TextMessage, protocol.CodeUnsupportedVersion},
		{[]byte(`{"v":1,"type":"dance","data":{}}`), websocket.TextMessage, protocol.CodeUnknownType},
		{ping("b"), websocket.BinaryMessage, protocol.CodeInvalidMessage},
	}
	for _, tc := range cases {
		if err := c.WriteMessage(tc.kind, tc.frame); err != nil {
			t.Fatal(err)
		}
		m := readFrame(t, c)
		data, _ := m["data"].(map[string]any)
		if m["type"] != "error" || data["code"] != string(tc.want) {
			t.Errorf("%s: got %v, want error %s", tc.frame, m, tc.want)
		}
	}
	if err := c.WriteMessage(websocket.TextMessage, ping("ok")); err != nil {
		t.Fatal(err)
	}
	if m := readFrame(t, c); m["type"] != "pong" || m["id"] != "ok" {
		t.Errorf("after invalid frames, ping got %v", m)
	}
}

func TestConn_RateLimited(t *testing.T) {
	e := newEnv(t, func(o *realtime.Options) { o.RatePerSec = 1; o.RateBurst = 2 })
	c := e.mustDial()
	for i := range 3 {
		if err := c.WriteMessage(websocket.TextMessage, ping(strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	got := []map[string]any{readFrame(t, c), readFrame(t, c), readFrame(t, c)}
	if got[0]["type"] != "pong" || got[1]["type"] != "pong" {
		t.Fatalf("within the burst: %v", got[:2])
	}
	data, _ := got[2]["data"].(map[string]any)
	if got[2]["type"] != "error" || data["code"] != "rate_limited" || data["retryable"] != true || got[2]["id"] != "2" {
		t.Fatalf("over the limit: %v", got[2])
	}
	if ms, _ := data["retryAfterMs"].(float64); ms < 1 || ms > 1000 {
		t.Errorf("retryAfterMs %v, want 1–1000", data["retryAfterMs"])
	}
	if e.h.count() != 2 {
		t.Errorf("dispatched %d, want 2: a rate-limited message must not reach the handler", e.h.count())
	}

	e.clock.Advance(time.Second)
	if err := c.WriteMessage(websocket.TextMessage, ping("later")); err != nil {
		t.Fatal(err)
	}
	if m := readFrame(t, c); m["type"] != "pong" {
		t.Errorf("after the bucket refilled: %v", m)
	}
}

func TestConn_RepeatedViolationsCloseWith4002(t *testing.T) {
	e := newEnv(t, func(o *realtime.Options) { o.RatePerSec = 1; o.RateBurst = 1 })

	// Exactly 100 violations within 10 s is tolerated.
	c := e.mustDial()
	for i := range 101 {
		_ = c.WriteMessage(websocket.TextMessage, ping(strconv.Itoa(i)))
	}
	for range 101 {
		readFrame(t, c)
	}
	e.clock.Advance(10 * time.Second) // the window slides past all of them; the bucket refills
	_ = c.WriteMessage(websocket.TextMessage, ping("still-open"))
	if m := readFrame(t, c); m["type"] != "pong" {
		t.Fatalf("after 100 violations: %v", m)
	}

	// The 101st violation within 10 s closes.
	c2 := e.mustDial()
	for i := range 102 {
		_ = c2.WriteMessage(websocket.TextMessage, ping(strconv.Itoa(i)))
	}
	if code := closeCode(t, c2, 2*time.Second); code != realtime.CloseRateLimited {
		t.Errorf("close code %d, want %d", code, realtime.CloseRateLimited)
	}
}

func TestConn_MessagesAreHandledOneAtATime(t *testing.T) {
	e := newEnv(t, nil)
	e.h.block = make(chan struct{})
	c := e.mustDial()
	for i := range 5 {
		_ = c.WriteMessage(websocket.TextMessage, ping(strconv.Itoa(i)))
	}
	eventually(t, "the first message to be dispatched", func() bool { return e.h.count() == 1 })
	time.Sleep(50 * time.Millisecond)
	if e.h.count() != 1 {
		t.Fatalf("dispatched %d while the first was still being handled", e.h.count())
	}
	close(e.h.block)
	for range 5 {
		readFrame(t, c)
	}
	if e.h.maxIn.Load() != 1 {
		t.Errorf("max concurrent handlers per connection %d, want 1", e.h.maxIn.Load())
	}
}

func TestConn_HeartbeatClosesDeadConnections(t *testing.T) {
	e := newEnv(t, func(o *realtime.Options) {
		o.PingInterval = 30 * time.Millisecond
		o.PongTimeout = 150 * time.Millisecond
	})

	// A live client: gorilla answers pings while it reads.
	live := e.mustDial()
	go func() {
		for {
			if _, _, err := live.ReadMessage(); err != nil {
				return
			}
		}
	}()
	// A dead client: never reads, so it never answers a ping.
	dead := e.mustDial()
	_ = dead

	eventually(t, "the dead connection to be dropped", func() bool { return e.h.closed.Load() == 1 })
	time.Sleep(300 * time.Millisecond) // two more pong timeouts
	if e.h.closed.Load() != 1 || e.gw.Active() != 1 {
		t.Errorf("closed %d, active %d; want only the dead connection dropped", e.h.closed.Load(), e.gw.Active())
	}
}

func TestConn_ServerCloseCodeReachesAClientThatKeepsSending(t *testing.T) {
	var server atomic.Pointer[realtime.Conn]
	e := newEnvWith(t, &closer{conn: &server})
	c := e.mustDial()
	_ = c.WriteMessage(websocket.TextMessage, ping("first"))
	eventually(t, "the server connection", func() bool { return server.Load() != nil })

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				if c.WriteMessage(websocket.TextMessage, ping("spam")) != nil {
					return
				}
			}
		}
	}()
	server.Load().Close(realtime.CloseReplaced, "replaced")
	code := closeCode(t, c, 3*time.Second)
	close(stop)
	if code != realtime.CloseReplaced {
		t.Errorf("close code %d, want %d", code, realtime.CloseReplaced)
	}
}

type closer struct {
	conn *atomic.Pointer[realtime.Conn]
	n    atomic.Int64
}

func (h *closer) Handle(_ context.Context, c *realtime.Conn, _ protocol.ClientMessage) {
	h.conn.CompareAndSwap(nil, c)
}
func (h *closer) Snapshot(context.Context, *realtime.Conn) ([]byte, error) { return nil, nil }
func (h *closer) Closed(*realtime.Conn)                                    { h.n.Add(1) }

func newEnvWith(t *testing.T, h realtime.Handler) *env {
	t.Helper()
	c := &clock{now: time.Unix(1_800_000_000, 0)}
	tokens := &auth.Tokens{Key: []byte(strings.Repeat("k", 32)), TTL: time.Hour, Now: c.Now}
	gw := realtime.New(testOptions(c), tokens, h, nil)
	srv := httptest.NewServer(gw)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, gw: gw, clock: c, tokens: tokens}
}

func TestConn_ClosedOnceAndNoGoroutineLeak(t *testing.T) {
	e := newEnv(t, nil)
	base := runtime.NumGoroutine()
	var conns []*websocket.Conn
	for range 200 {
		c, _, err := e.dial(e.token(auth.RoleParticipant), testOrigin)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	eventually(t, "every connection to close", func() bool { return e.gw.Active() == 0 })
	if n := e.h.closed.Load(); n != 200 {
		t.Errorf("Closed called %d times for 200 connections", n)
	}
	eventually(t, "goroutines to return to baseline", func() bool { return runtime.NumGoroutine() <= base+5 })
}
