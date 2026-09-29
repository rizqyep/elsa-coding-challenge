package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
)

// Message is one server message as a client received it.
type Message struct {
	At   time.Time
	Type string
	ID   string
	Data map[string]any
	Raw  []byte
}

// Client speaks the WebSocket protocol, validates every received message against its JSON Schema,
// applies versions the way a real client must (FR-28), and keeps what it received for assertions.
type Client struct {
	conn *websocket.Conn
	wmu  sync.Mutex
	seq  atomic.Int64
	opts DialOptions

	mu         sync.Mutex
	cond       *sync.Cond
	log        []Message
	violations []string
	stale      int
	stateV     int64
	lbV        int64
	seen       map[int64]string // state version → the event that carried it
	closeCode  int
	done       chan struct{}
}

// DialOptions adjust a connection.
type DialOptions struct {
	Origin    string
	OnMessage func(Message) // run on the read goroutine after each message is recorded
	ReadDelay time.Duration // pause before each read, to act as a slow reader
	NoLog     bool          // keep counters only; WaitFor and Messages see nothing (for large simulations)
}

// Dial connects with token, retrying 503s after their Retry-After until ctx ends (TRD §7.1).
func Dial(ctx context.Context, wsURL, token, origin string) (*Client, error) {
	return DialWith(ctx, wsURL, token, DialOptions{Origin: origin})
}

// DialWith is Dial with options.
func DialWith(ctx context.Context, wsURL, token string, o DialOptions) (*Client, error) {
	h := http.Header{}
	if o.Origin != "" {
		h.Set("Origin", o.Origin)
	}
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 1024}
	for {
		conn, resp, err := d.DialContext(ctx, wsURL+"?token="+token, h)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			c := &Client{conn: conn, opts: o, seen: map[int64]string{}, done: make(chan struct{})}
			c.cond = sync.NewCond(&c.mu)
			go c.read()
			return c, nil
		}
		if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
			if resp != nil {
				return nil, fmt.Errorf("dial: %d: %w", resp.StatusCode, err)
			}
			return nil, fmt.Errorf("dial: %w", err)
		}
		wait := time.Second
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, fmt.Errorf("dial: still busy: %w", ctx.Err())
		}
	}
}

// Send sends a client message with a fresh request id and returns the id.
func (c *Client) Send(typ string, data any) (string, error) {
	id := typ + "-" + strconv.FormatInt(c.seq.Add(1), 10)
	return id, c.SendID(id, typ, data)
}

// SendID sends a client message with the given request id (a resend reuses it, FR-30).
func (c *Client) SendID(id, typ string, data any) error {
	b, err := json.Marshal(map[string]any{"v": protocol.Version, "type": typ, "id": id, "data": data})
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, b)
}

// Close hangs up; safe to call more than once.
func (c *Client) Close() { _ = c.conn.Close() }

// Done is closed once the connection has ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// CloseCode is the close code the server sent, or 1006 when the connection just dropped.
func (c *Client) CloseCode() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCode
}

// Messages returns everything received so far.
func (c *Client) Messages() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Message(nil), c.log...)
}

// Violations lists protocol problems seen: schema failures and conflicting versions.
func (c *Client) Violations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.violations...)
}

// Stale counts messages ignored because a newer version was already applied.
func (c *Client) Stale() int { c.mu.Lock(); defer c.mu.Unlock(); return c.stale }

// StateVersion is the newest quiz state version applied.
func (c *Client) StateVersion() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.stateV }

// LeaderboardVersion is the newest leaderboard version applied.
func (c *Client) LeaderboardVersion() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.lbV }

// WaitFor returns the first message at index ≥ from that matches, waiting for it until ctx ends.
func (c *Client) WaitFor(ctx context.Context, from int, match func(Message) bool) (Message, error) {
	stop := context.AfterFunc(ctx, func() { c.mu.Lock(); c.cond.Broadcast(); c.mu.Unlock() })
	defer stop()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := from; ; {
		for ; i < len(c.log); i++ {
			if match(c.log[i]) {
				return c.log[i], nil
			}
		}
		select {
		case <-c.done:
			return Message{}, fmt.Errorf("connection closed (%d) before a matching message", c.closeCode)
		default:
		}
		if ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
		c.cond.Wait()
	}
}

// Expect waits for the next message of type typ after index from.
func (c *Client) Expect(ctx context.Context, from int, typ string) (Message, error) {
	return c.WaitFor(ctx, from, func(m Message) bool { return m.Type == typ })
}

func (c *Client) read() {
	defer func() {
		c.mu.Lock()
		close(c.done)
		c.cond.Broadcast()
		c.mu.Unlock()
	}()
	for {
		if c.opts.ReadDelay > 0 {
			time.Sleep(c.opts.ReadDelay)
		}
		_, b, err := c.conn.ReadMessage()
		if err != nil {
			code := websocket.CloseAbnormalClosure
			var ce *websocket.CloseError
			if errors.As(err, &ce) {
				code = ce.Code
			}
			c.mu.Lock()
			c.closeCode = code
			c.mu.Unlock()
			return
		}
		m := Message{At: time.Now(), Raw: b}
		var env struct {
			Type string         `json:"type"`
			ID   string         `json:"id"`
			Data map[string]any `json:"data"`
		}
		verr := protocol.ValidateServer(b)
		if err := json.Unmarshal(b, &env); err == nil {
			m.Type, m.ID, m.Data = env.Type, env.ID, env.Data
		}
		c.mu.Lock()
		if verr != nil {
			c.violations = append(c.violations, fmt.Sprintf("%s failed its schema: %v", m.Type, verr))
		}
		c.apply(m)
		if !c.opts.NoLog {
			c.log = append(c.log, m)
			c.cond.Broadcast()
		}
		c.mu.Unlock()
		if c.opts.OnMessage != nil {
			c.opts.OnMessage(m)
		}
	}
}

// apply follows the client version rules (asyncapi): state and leaderboard versions only move forward,
// "finished" always applies, and the same state version must always describe the same state. Holds c.mu.
func (c *Client) apply(m Message) {
	switch m.Type {
	case "snapshot":
		if q, ok := m.Data["quiz"].(map[string]any); ok {
			c.applyState(num(q["stateVersion"]), q["status"] == "finished", "")
		}
		if lb, ok := m.Data["leaderboard"].(map[string]any); ok {
			c.applyLeaderboard(num(lb["version"]))
		}
	case "question", "question_closed", "quiz_state", "quiz_finished":
		sig := m.Type + string(mustJSON(m.Data))
		c.applyState(num(m.Data["stateVersion"]), m.Type == "quiz_finished" || m.Data["status"] == "finished", sig)
	case "leaderboard":
		c.applyLeaderboard(num(m.Data["version"]))
	}
}

func (c *Client) applyState(v int64, finished bool, sig string) {
	if sig != "" {
		if prev, ok := c.seen[v]; ok && prev != sig {
			c.violations = append(c.violations, fmt.Sprintf("state version %d carried two different events", v))
		}
		c.seen[v] = sig
	}
	switch {
	case v > c.stateV:
		c.stateV = v
	case v < c.stateV && !finished:
		c.stale++
	}
}

func (c *Client) applyLeaderboard(v int64) {
	if v > c.lbV {
		c.lbV = v
	} else if v < c.lbV {
		c.stale++
	}
}

func num(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
