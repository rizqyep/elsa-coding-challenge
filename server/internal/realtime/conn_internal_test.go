package realtime

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
)

// fakeSocket stands in for *websocket.Conn; writes can be stalled to act like a slow client.
type fakeSocket struct {
	mu       sync.Mutex
	gate     chan struct{} // writes wait until it is closed; nil means writes pass
	written  []string
	labels   map[*websocket.PreparedMessage]string
	closes   []int
	readDone chan struct{}
	once     sync.Once
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{labels: map[*websocket.PreparedMessage]string{}, readDone: make(chan struct{})}
}

func (f *fakeSocket) stall() { f.mu.Lock(); f.gate = make(chan struct{}); f.mu.Unlock() }
func (f *fakeSocket) release() {
	f.mu.Lock()
	if f.gate != nil {
		close(f.gate)
		f.gate = nil
	}
	f.mu.Unlock()
}

func (f *fakeSocket) wait() {
	f.mu.Lock()
	g := f.gate
	f.mu.Unlock()
	if g != nil {
		<-g
	}
}

func (f *fakeSocket) record(s string) { f.mu.Lock(); f.written = append(f.written, s); f.mu.Unlock() }

func (f *fakeSocket) frames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.written...)
}

func (f *fakeSocket) closeCodes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.closes...)
}

func (f *fakeSocket) WriteMessage(_ int, data []byte) error {
	f.wait()
	f.record(string(data))
	return nil
}

func (f *fakeSocket) WritePreparedMessage(pm *websocket.PreparedMessage) error {
	f.wait()
	f.mu.Lock()
	l, ok := f.labels[pm]
	f.mu.Unlock()
	if !ok {
		l = fmt.Sprintf("pm:%p", pm) // a broadcast from the hub: same pointer means the same encoded frame
	}
	f.record(l)
	return nil
}

// WriteControl waits for a stalled write, as gorilla's does: it needs the same write lock.
func (f *fakeSocket) WriteControl(kind int, data []byte, _ time.Time) error {
	f.wait()
	if kind == websocket.CloseMessage && len(data) >= 2 {
		f.mu.Lock()
		f.closes = append(f.closes, int(binary.BigEndian.Uint16(data)))
		f.mu.Unlock()
	}
	return nil
}

func (f *fakeSocket) ReadMessage() (int, []byte, error) {
	<-f.readDone
	return 0, nil, errors.New("closed")
}

func (f *fakeSocket) SetReadDeadline(t time.Time) error {
	if !t.IsZero() && time.Until(t) < 5*time.Second {
		time.AfterFunc(time.Until(t), f.endRead)
	}
	return nil
}

func (f *fakeSocket) endRead()                          { f.once.Do(func() { close(f.readDone) }) }
func (f *fakeSocket) SetWriteDeadline(time.Time) error  { return nil }
func (f *fakeSocket) SetReadLimit(int64)                {}
func (f *fakeSocket) SetPongHandler(func(string) error) {}
func (f *fakeSocket) Close() error                      { f.endRead(); return nil }

func (f *fakeSocket) prepared(t *testing.T, label string) *websocket.PreparedMessage {
	t.Helper()
	pm, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(label))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.labels[pm] = label
	f.mu.Unlock()
	return pm
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type snapHandler struct {
	snapshot func(ctx context.Context) ([]byte, error)
	closed   atomic.Int64
}

func (h *snapHandler) Handle(context.Context, *Conn, protocol.ClientMessage) {}
func (h *snapHandler) Snapshot(ctx context.Context, _ *Conn) ([]byte, error) {
	return h.snapshot(ctx)
}
func (h *snapHandler) Closed(*Conn) { h.closed.Add(1) }

func internalOptions(c *fakeClock, queue int) Options {
	return Options{
		MaxMessageBytes:   4096,
		SendQueueSize:     queue,
		PingInterval:      time.Hour,
		PongTimeout:       2 * time.Hour,
		RatePerSec:        20,
		RateBurst:         40,
		AdmissionPerSec:   1000,
		MaxConnections:    1000,
		ResyncConcurrency: 32,
		CloseGrace:        50 * time.Millisecond,
		Now:               c.Now,
		RandIntN:          func(int) int { return 0 },
	}
}

// start runs a connection over a fake socket until the test ends.
func start(t *testing.T, s *shared, sock *fakeSocket) *Conn {
	t.Helper()
	c := newConn(sock, auth.Claims{ParticipantID: "p-1", Role: auth.RoleParticipant}, s)
	done := make(chan struct{})
	go func() { c.run(); close(done) }()
	t.Cleanup(func() {
		sock.release()
		sock.endRead()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("connection did not stop")
		}
	})
	return c
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// within fails the test, instead of hanging it, when fn doesn't return in time.
func within(t *testing.T, limit time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("%s blocked for more than %s", what, limit)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSend_FullQueueNeverBlocksTheBroadcaster(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	h := &snapHandler{snapshot: func(context.Context) ([]byte, error) { return nil, nil }}
	sock := newFakeSocket()
	sock.stall() // the client never drains
	c := start(t, newShared(internalOptions(clk, 4), h, nil), sock)
	pm := sock.prepared(t, "lb")

	within(t, time.Second, "80,000 sends to a stalled client", func() {
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for range 10_000 {
					c.SendPrepared(pm)
				}
			})
		}
		wg.Wait()
	})
}

func TestSlowClient_QueueDroppedThenResyncedWithASnapshot(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	h := &snapHandler{snapshot: func(context.Context) ([]byte, error) { return []byte("snapshot"), nil }}
	sock := newFakeSocket()
	sock.stall()
	c := start(t, newShared(internalOptions(clk, 4), h, nil), sock)

	if !c.SendPrepared(sock.prepared(t, "A")) {
		t.Fatal("first send refused")
	}
	waitFor(t, "the writer to pick up A and stall", func() bool { return len(c.queue) == 0 })
	for _, l := range []string{"B", "C", "D", "E"} {
		if !c.SendPrepared(sock.prepared(t, l)) {
			t.Fatalf("send %s refused before the queue was full", l)
		}
	}
	if c.SendPrepared(sock.prepared(t, "F")) {
		t.Fatal("send into a full queue reported success")
	}
	if c.SendPrepared(sock.prepared(t, "G")) {
		t.Fatal("send while a resync is pending reported success; the snapshot supersedes it")
	}

	sock.release()
	waitFor(t, "the snapshot", func() bool { return len(sock.frames()) == 2 })
	c.SendPrepared(sock.prepared(t, "H"))
	waitFor(t, "H", func() bool { return len(sock.frames()) == 3 })

	if got, want := sock.frames(), []string{"A", "snapshot", "H"}; !equal(got, want) {
		t.Errorf("written %v, want %v: dropped messages replaced by one snapshot", got, want)
	}
	if len(sock.closeCodes()) != 0 {
		t.Errorf("first overflow closed the connection: %v", sock.closeCodes())
	}
}

// overflow stalls the writer and overfills the queue.
func overflow(t *testing.T, c *Conn, sock *fakeSocket) {
	t.Helper()
	sock.stall()
	c.SendPrepared(sock.prepared(t, "stuck"))
	waitFor(t, "the writer to stall", func() bool { return len(c.queue) == 0 })
	for i := range cap(c.queue) + 1 {
		c.SendPrepared(sock.prepared(t, fmt.Sprint("x", i)))
	}
}

func TestSlowClient_SecondOverflowWithin30sClosesWith4003(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	h := &snapHandler{snapshot: func(context.Context) ([]byte, error) { return []byte("snapshot"), nil }}
	sock := newFakeSocket()
	c := start(t, newShared(internalOptions(clk, 4), h, nil), sock)

	overflow(t, c, sock)
	sock.release()
	waitFor(t, "the first resync", func() bool {
		f := sock.frames()
		return len(f) > 0 && f[len(f)-1] == "snapshot"
	})

	clk.Advance(29 * time.Second)
	// Closes with 4003 while the writer is still stalled; the sender must not wait for it.
	within(t, 500*time.Millisecond, "the overflow that closes a stalled connection", func() { overflow(t, c, sock) })
	sock.release()
	waitFor(t, "the close", func() bool { return h.closed.Load() == 1 })
	if codes := sock.closeCodes(); len(codes) != 1 || codes[0] != CloseSlowConsumer {
		t.Errorf("close codes %v, want [%d]", codes, CloseSlowConsumer)
	}
}

func TestSlowClient_OverflowsMoreThan30sApartBothResync(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	var snaps atomic.Int64
	h := &snapHandler{snapshot: func(context.Context) ([]byte, error) { snaps.Add(1); return []byte("snapshot"), nil }}
	sock := newFakeSocket()
	c := start(t, newShared(internalOptions(clk, 4), h, nil), sock)

	overflow(t, c, sock)
	sock.release()
	waitFor(t, "the first resync", func() bool { return snaps.Load() == 1 })
	clk.Advance(31 * time.Second)
	overflow(t, c, sock)
	sock.release()
	waitFor(t, "the second resync", func() bool { return snaps.Load() == 2 })
	if len(sock.closeCodes()) != 0 || h.closed.Load() != 0 {
		t.Errorf("closed after overflows 31 s apart: %v", sock.closeCodes())
	}
}

func TestSlowClient_FailedSnapshotClosesWith4003(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	h := &snapHandler{snapshot: func(context.Context) ([]byte, error) { return nil, errors.New("redis timeout") }}
	sock := newFakeSocket()
	c := start(t, newShared(internalOptions(clk, 4), h, nil), sock)
	overflow(t, c, sock)
	sock.release()
	waitFor(t, "the close", func() bool { return h.closed.Load() == 1 })
	if codes := sock.closeCodes(); len(codes) != 1 || codes[0] != CloseSlowConsumer {
		t.Errorf("close codes %v, want [%d]", codes, CloseSlowConsumer)
	}
}

func TestSlowClient_NothingToResyncBeforeJoining(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	var snaps atomic.Int64
	h := &snapHandler{snapshot: func(context.Context) ([]byte, error) { snaps.Add(1); return nil, nil }}
	sock := newFakeSocket()
	c := start(t, newShared(internalOptions(clk, 4), h, nil), sock)
	overflow(t, c, sock)
	sock.release()
	waitFor(t, "the resync attempt", func() bool { return snaps.Load() == 1 })
	c.SendBytes([]byte("after"))
	waitFor(t, "the next message", func() bool {
		f := sock.frames()
		return len(f) > 0 && f[len(f)-1] == "after"
	})
	if len(sock.closeCodes()) != 0 {
		t.Errorf("closed: %v", sock.closeCodes())
	}
}

func TestResync_StampedeIsBoundedPerGateway(t *testing.T) {
	// Redis stalls → every connection overflows at once; resync reads must not all hit Redis together.
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	var inFlight, peak, total atomic.Int64
	h := &snapHandler{snapshot: func(context.Context) ([]byte, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		total.Add(1)
		return []byte("snapshot"), nil
	}}
	opts := internalOptions(clk, 2)
	opts.ResyncConcurrency = 4
	s := newShared(opts, h, nil)

	const conns = 60
	socks := make([]*fakeSocket, conns)
	cs := make([]*Conn, conns)
	for i := range conns {
		socks[i] = newFakeSocket()
		cs[i] = start(t, s, socks[i])
	}
	for i := range conns {
		overflow(t, cs[i], socks[i])
	}
	for _, sk := range socks {
		sk.release()
	}
	waitFor(t, "every connection to resync", func() bool { return total.Load() == conns })
	if p := peak.Load(); p > 4 {
		t.Errorf("%d snapshot reads ran at once, limit is 4", p)
	}
}

func TestResync_WaitingForASlotStopsWhenTheConnectionCloses(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	hold := make(chan struct{})
	h := &snapHandler{snapshot: func(ctx context.Context) ([]byte, error) {
		select {
		case <-hold:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	}}
	opts := internalOptions(clk, 2)
	opts.ResyncConcurrency = 1
	s := newShared(opts, h, nil)

	a, sa := newFakeSocket(), newFakeSocket()
	ca := start(t, s, a)
	cb := start(t, s, sa)
	overflow(t, ca, a)
	a.release()
	time.Sleep(20 * time.Millisecond) // a holds the only slot
	overflow(t, cb, sa)
	sa.release()
	time.Sleep(20 * time.Millisecond) // b waits for it

	cb.Close(websocket.CloseGoingAway, "bye")
	waitFor(t, "b to stop while waiting for a slot", func() bool { return h.closed.Load() == 1 })
	close(hold)
}

func TestConn_IDsAreUniqueAndNonEmpty(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	s := newShared(internalOptions(clk, 4), &snapHandler{snapshot: func(context.Context) ([]byte, error) { return nil, nil }}, nil)
	seen := map[string]bool{}
	for range 1000 {
		id := newConn(newFakeSocket(), auth.Claims{}, s).ID()
		if id == "" || seen[id] {
			t.Fatalf("connection id %q empty or repeated", id)
		}
		seen[id] = true
	}
}
