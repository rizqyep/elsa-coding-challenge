package realtime

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
)

// Close codes the gateway sends (docs/api/asyncapi.yaml).
const (
	CloseReplaced     = 4000
	CloseRateLimited  = 4002
	CloseSlowConsumer = 4003
)

// retryAfterJitterSecs spreads the retries of rejected handshakes over up to this many extra seconds.
const retryAfterJitterSecs = 2

// Options tune the gateway; zero durations and sizes fall back to the TRD defaults.
type Options struct {
	AllowedOrigins    []string
	MaxMessageBytes   int
	ReadBufferBytes   int
	WriteBufferBytes  int
	SendQueueSize     int
	PingInterval      time.Duration
	PongTimeout       time.Duration
	RatePerSec        int
	RateBurst         int
	AdmissionPerSec   int
	MaxConnections    int
	ResyncConcurrency int           // snapshot reads running at once for slow-client resyncs, per gateway
	ResyncJitter      time.Duration // random delay before a resync, so a stall's resyncs don't arrive together
	WriteTimeout      time.Duration
	CloseGrace        time.Duration // how long to wait for the client's close reply
	Now               func() time.Time
	RandIntN          func(n int) int
}

// OptionsFrom maps the gateway configuration (TRD §2.3).
func OptionsFrom(c config.Gateway) Options {
	return Options{
		AllowedOrigins:   c.AllowedOrigins,
		MaxMessageBytes:  c.MaxMessageBytes,
		ReadBufferBytes:  c.ReadBufferBytes,
		WriteBufferBytes: c.WriteBufferBytes,
		SendQueueSize:    c.SendQueueSize,
		PingInterval:     c.PingInterval,
		PongTimeout:      c.PongTimeout,
		RatePerSec:       c.RatePerSec,
		RateBurst:        c.RateBurst,
		AdmissionPerSec:  c.JoinAdmissionPerSec,
		MaxConnections:   c.MaxConnections,
	}
}

func (o Options) withDefaults() Options {
	if o.ResyncConcurrency <= 0 {
		o.ResyncConcurrency = 32
	}
	if o.ResyncJitter == 0 {
		o.ResyncJitter = 250 * time.Millisecond
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 10 * time.Second
	}
	if o.CloseGrace <= 0 {
		o.CloseGrace = time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RandIntN == nil {
		o.RandIntN = rand.IntN
	}
	return o
}

// Verifier checks a connection's token.
type Verifier interface {
	Verify(token string) (auth.Claims, error)
}

// Handler receives a connection's messages; Handle runs on the connection's read goroutine, one message at a time.
type Handler interface {
	Handle(ctx context.Context, c *Conn, m protocol.ClientMessage)
	// Snapshot returns the frame that resyncs a slow client, or nil when there is nothing to resync.
	Snapshot(ctx context.Context, c *Conn) ([]byte, error)
	// Closed is called once, after the connection's goroutines have stopped.
	Closed(c *Conn)
}

// shared is the state every connection of one gateway uses.
type shared struct {
	opts        Options
	handler     Handler
	log         *slog.Logger
	resyncSlots chan struct{}
}

func newShared(o Options, h Handler, log *slog.Logger) *shared {
	o = o.withDefaults()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &shared{opts: o, handler: h, log: log, resyncSlots: make(chan struct{}, o.ResyncConcurrency)}
}

// acquireResync waits a random jitter and then for a free resync slot (TRD §7.6).
func (s *shared) acquireResync(ctx context.Context) bool {
	if ms := int(s.opts.ResyncJitter / time.Millisecond); ms > 0 {
		if d := time.Duration(s.opts.RandIntN(ms+1)) * time.Millisecond; d > 0 {
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return false
			}
		}
	}
	select {
	case s.resyncSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *shared) releaseResync() { <-s.resyncSlots }

// Gateway accepts WebSocket connections at /ws (TRD §7.1).
type Gateway struct {
	s         *shared
	verifier  Verifier
	origins   map[string]bool
	admission *rate.Limiter
	upgrader  websocket.Upgrader
	active    atomic.Int64
}

// New returns a gateway; log may be nil.
func New(o Options, v Verifier, h Handler, log *slog.Logger) *Gateway {
	s := newShared(o, h, log)
	g := &Gateway{
		s:         s,
		verifier:  v,
		origins:   map[string]bool{},
		admission: rate.NewLimiter(rate.Limit(s.opts.AdmissionPerSec), s.opts.AdmissionPerSec),
		upgrader: websocket.Upgrader{
			HandshakeTimeout:  10 * time.Second,
			ReadBufferSize:    s.opts.ReadBufferBytes,
			WriteBufferSize:   s.opts.WriteBufferBytes,
			WriteBufferPool:   &sync.Pool{},
			EnableCompression: false,
			CheckOrigin:       func(*http.Request) bool { return true }, // checked before the upgrade
		},
	}
	for _, o := range s.opts.AllowedOrigins {
		g.origins[o] = true
	}
	return g
}

// Active returns the number of open connections.
func (g *Gateway) Active() int { return int(g.active.Load()) }

// ServeHTTP rejects what it can before upgrading, so a rejected client costs no goroutine or buffer.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" && !g.origins[o] {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	tok, _ := auth.FromQuery(r)
	claims, err := g.verifier.Verify(tok)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if !g.takeSlot() {
		g.reject(w, time.Second)
		return
	}
	now := g.s.opts.Now()
	res := g.admission.ReserveN(now, 1)
	if wait := res.DelayFrom(now); wait > 0 {
		res.CancelAt(now)
		g.active.Add(-1)
		g.reject(w, wait)
		return
	}
	ws, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		g.active.Add(-1) // the upgrader has already written the error response
		return
	}
	newConn(ws, claims, g.s).run()
	g.active.Add(-1)
}

func (g *Gateway) takeSlot() bool {
	limit := int64(g.s.opts.MaxConnections)
	for {
		n := g.active.Load()
		if n >= limit {
			return false
		}
		if g.active.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

// reject answers 503 with a jittered Retry-After, so rejected clients don't retry together.
func (g *Gateway) reject(w http.ResponseWriter, wait time.Duration) {
	secs := max(int(math.Ceil(wait.Seconds())), 1) + g.s.opts.RandIntN(retryAfterJitterSecs+1)
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(w, "try again later", http.StatusServiceUnavailable)
}
