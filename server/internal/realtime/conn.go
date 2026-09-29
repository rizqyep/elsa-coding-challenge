package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
)

const (
	slowConsumerWindow = 30 * time.Second // a second overflow within it closes with 4003 (TRD §7.6)
	violationWindow    = 10 * time.Second
	maxViolations      = 100 // more than this within violationWindow closes with 4002 (TRD §7.3)
	maxErrorMessage    = 300 // docs/api/schemas/ws/server/error.json
)

// socket is the part of *websocket.Conn a connection uses.
type socket interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(kind int, data []byte) error
	WritePreparedMessage(pm *websocket.PreparedMessage) error
	WriteControl(kind int, data []byte, deadline time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetReadLimit(limit int64)
	SetPongHandler(h func(appData string) error)
	Close() error
}

// outbound is a queued frame: a shared broadcast or a personal message.
type outbound struct {
	prepared *websocket.PreparedMessage
	data     []byte
}

// Conn is one client connection: a read goroutine and the socket's only writer (TRD §7.2).
type Conn struct {
	sock    socket
	claims  auth.Claims
	s       *shared
	ctx     context.Context
	cancel  context.CancelFunc
	queue   chan outbound
	resync  chan struct{}
	limiter *rate.Limiter

	closeOnce  sync.Once
	writerDone chan struct{}

	mu            sync.Mutex
	lastOverflow  time.Time
	resyncPending atomic.Bool

	violations []time.Time // read goroutine only
}

func newConn(sock socket, claims auth.Claims, s *shared) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		sock:       sock,
		claims:     claims,
		s:          s,
		ctx:        ctx,
		cancel:     cancel,
		queue:      make(chan outbound, s.opts.SendQueueSize),
		resync:     make(chan struct{}, 1),
		limiter:    rate.NewLimiter(rate.Limit(s.opts.RatePerSec), s.opts.RateBurst),
		writerDone: make(chan struct{}),
	}
}

// Claims identify the connection's caller.
func (c *Conn) Claims() auth.Claims { return c.claims }

// Context is cancelled when the connection starts closing.
func (c *Conn) Context() context.Context { return c.ctx }

// run serves the connection until it ends; the calling goroutine is the reader.
func (c *Conn) run() {
	c.sock.SetReadLimit(int64(c.s.opts.MaxMessageBytes))
	_ = c.sock.SetReadDeadline(time.Now().Add(c.s.opts.PongTimeout))
	c.sock.SetPongHandler(func(string) error {
		if c.ctx.Err() != nil {
			return nil // closing: keep the close grace deadline
		}
		return c.sock.SetReadDeadline(time.Now().Add(c.s.opts.PongTimeout))
	})
	go c.writeLoop()
	c.readLoop()
	c.cancel()
	_ = c.sock.Close()
	<-c.writerDone
	c.s.handler.Closed(c)
}

func (c *Conn) readLoop() {
	for {
		kind, data, err := c.sock.ReadMessage()
		if err != nil {
			return
		}
		if c.ctx.Err() != nil {
			continue // closing: discard until the client's close reply or the grace deadline
		}
		if !c.allow(data) {
			continue
		}
		if kind != websocket.TextMessage {
			c.ReplyError("", protocol.CodeInvalidMessage, "only text frames are accepted", 0)
			continue
		}
		m, err := protocol.DecodeClient(data)
		if err != nil {
			code := protocol.CodeInvalidMessage
			var de *protocol.DecodeError
			if errors.As(err, &de) {
				code = de.Code
			}
			c.ReplyError("", code, err.Error(), 0)
			continue
		}
		c.s.handler.Handle(c.ctx, c, m)
	}
}

// allow applies the per-connection rate limit, before any decoding work (NFR-21).
func (c *Conn) allow(frame []byte) bool {
	now := c.s.opts.Now()
	res := c.limiter.ReserveN(now, 1)
	wait := res.DelayFrom(now)
	if wait == 0 {
		return true
	}
	res.CancelAt(now)
	if c.violated(now) {
		c.Close(CloseRateLimited, "too many requests")
		return false
	}
	var head struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(frame, &head)
	c.ReplyError(head.ID, protocol.CodeRateLimited, "rate limit exceeded", wait)
	return false
}

// violated records a violation and reports whether it is one too many within the window.
func (c *Conn) violated(now time.Time) bool {
	cutoff := now.Add(-violationWindow)
	kept := c.violations[:0]
	for _, t := range c.violations {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	c.violations = append(kept, now)
	return len(c.violations) > maxViolations
}

func (c *Conn) writeLoop() {
	defer close(c.writerDone)
	ping := time.NewTicker(c.s.opts.PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-c.resync: // a resync goes before anything still queued
			if !c.resyncNow() {
				return
			}
			continue
		default:
		}
		select {
		case <-c.ctx.Done():
			return
		case <-c.resync:
			if !c.resyncNow() {
				return
			}
		case it := <-c.queue:
			if err := c.write(it); err != nil {
				c.fail()
				return
			}
		case <-ping.C:
			if err := c.sock.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.s.opts.WriteTimeout)); err != nil {
				c.fail()
				return
			}
		}
	}
}

func (c *Conn) write(it outbound) error {
	_ = c.sock.SetWriteDeadline(time.Now().Add(c.s.opts.WriteTimeout))
	if it.prepared != nil {
		return c.sock.WritePreparedMessage(it.prepared)
	}
	return c.sock.WriteMessage(websocket.TextMessage, it.data)
}

// fail ends a connection whose socket broke; closing the socket unblocks the reader.
func (c *Conn) fail() {
	c.cancel()
	_ = c.sock.Close()
}

// SendPrepared queues a broadcast frame; it never blocks (NFR-5d).
func (c *Conn) SendPrepared(pm *websocket.PreparedMessage) bool {
	return c.send(outbound{prepared: pm})
}

// SendBytes queues a personal frame; it never blocks.
func (c *Conn) SendBytes(b []byte) bool { return c.send(outbound{data: b}) }

func (c *Conn) send(it outbound) bool {
	if c.ctx.Err() != nil || c.resyncPending.Load() {
		return false // a pending resync's snapshot supersedes it
	}
	select {
	case c.queue <- it:
		return true
	default:
	}
	c.overflow()
	return false
}

// overflow applies the slow-client policy (TRD §7.6).
func (c *Conn) overflow() {
	c.mu.Lock()
	if c.resyncPending.Load() {
		c.mu.Unlock()
		return
	}
	now := c.s.opts.Now()
	if !c.lastOverflow.IsZero() && now.Sub(c.lastOverflow) < slowConsumerWindow {
		c.mu.Unlock()
		c.Close(CloseSlowConsumer, "slow consumer")
		return
	}
	c.lastOverflow = now
	c.resyncPending.Store(true)
	c.mu.Unlock()
drain:
	for {
		select {
		case <-c.queue:
		default:
			break drain
		}
	}
	select {
	case c.resync <- struct{}{}:
	default:
	}
}

// resyncNow sends a fresh snapshot in place of the dropped frames; false ends the writer.
func (c *Conn) resyncNow() bool {
	if !c.s.acquireResync(c.ctx) {
		return false
	}
	c.resyncPending.Store(false) // frames sent from here on follow the snapshot
	snap, err := c.s.handler.Snapshot(c.ctx, c)
	c.s.releaseResync()
	if err != nil {
		if c.ctx.Err() == nil {
			c.s.log.Warn("resync snapshot failed", slog.String("participant_id", c.claims.ParticipantID), slog.Any("error", err))
			c.Close(CloseSlowConsumer, "resync failed")
		}
		return false
	}
	if snap == nil {
		return true
	}
	if err := c.write(outbound{data: snap}); err != nil {
		c.fail()
		return false
	}
	return true
}

// Close sends a close frame and ends the connection once the client replies or the grace period passes; it never blocks.
func (c *Conn) Close(code int, reason string) {
	c.closeOnce.Do(func() {
		c.cancel()
		go func() {
			_ = c.sock.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
			_ = c.sock.SetReadDeadline(time.Now().Add(c.s.opts.CloseGrace))
		}()
	})
}

// Reply queues a personal message answering request id.
func (c *Conn) Reply(id string, t protocol.Type, payload any) bool {
	b, err := protocol.Encode(t, id, payload)
	if err != nil {
		c.s.log.Error("encode reply", slog.String("type", string(t)), slog.Any("error", err))
		return false
	}
	return c.SendBytes(b)
}

// ReplyError queues an error message; retryAfter > 0 sets retryAfterMs.
func (c *Conn) ReplyError(id string, code protocol.ErrorCode, message string, retryAfter time.Duration) bool {
	if r := []rune(message); len(r) > maxErrorMessage {
		message = string(r[:maxErrorMessage])
	}
	e := protocol.Error{Code: code, Message: message, Retryable: retryable(code)}
	if retryAfter > 0 {
		ms := int64(math.Ceil(float64(retryAfter) / float64(time.Millisecond)))
		e.RetryAfterMs = &ms
	}
	return c.Reply(id, protocol.TypeError, e)
}

// retryable reports whether the same request may succeed later (TRD §9.6).
func retryable(code protocol.ErrorCode) bool {
	return code == protocol.CodeRateLimited || code == protocol.CodeServerBusy
}
