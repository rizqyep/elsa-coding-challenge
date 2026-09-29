package realtime

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// SubscriberHooks is what the subscriber reports to (the Hub).
type SubscriberHooks interface {
	Has(code quiz.Code) bool
	OnMessage(code quiz.Code, payload string)
	// OnResubscribed is called once per room when its subscription is restored after a lost connection.
	OnResubscribed(code quiz.Code)
}

// SubscriberOptions tune the subscriber; zero values use the TRD §9.3 defaults.
type SubscriberOptions struct {
	HealthInterval time.Duration // idle time before a ping; an unanswered ping ends the connection
	Backoff        retry.Policy  // reconnect backoff (full jitter)
	Rand           func(n int64) int64
	Log            *slog.Logger
}

// Subscriber owns this gateway's Redis pub/sub connection (TRD §7.5, §9.3). It runs its own receive
// loop instead of go-redis's Channel(), which reconnects silently and hides what was missed.
type Subscriber struct {
	rdb   redis.UniversalClient
	hooks SubscriberHooks
	opts  SubscriberOptions

	mu      sync.Mutex
	ps      *redis.PubSub // nil while disconnected
	epoch   int           // incremented on every new connection
	chans   map[string]*subState
	pending map[string]int // SUBSCRIBE commands sent on this connection and not yet confirmed
}

type subState struct {
	code           quiz.Code
	confirmedEpoch int
	everConfirmed  bool
	waiters        []chan struct{}
}

// NewSubscriber returns a subscriber; start it with Run.
func NewSubscriber(rdb redis.UniversalClient, hooks SubscriberHooks, o SubscriberOptions) *Subscriber {
	if o.HealthInterval <= 0 {
		o.HealthInterval = 15 * time.Second
	}
	if o.Backoff.Base <= 0 {
		o.Backoff = retry.Policy{Base: 100 * time.Millisecond, Cap: 5 * time.Second}
	}
	if o.Rand == nil {
		o.Rand = rand.Int64N
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Subscriber{rdb: rdb, hooks: hooks, opts: o, chans: map[string]*subState{}, pending: map[string]int{}}
}

// Run keeps a pub/sub connection open until ctx ends, reconnecting with backoff and resubscribing every room.
func (s *Subscriber) Run(ctx context.Context) {
	attempt := 0
	for ctx.Err() == nil {
		ps := s.connect(ctx)
		progressed, err := s.receive(ctx, ps)
		s.mu.Lock()
		if s.ps == ps {
			s.ps = nil
		}
		s.mu.Unlock()
		_ = ps.Close()
		if ctx.Err() != nil {
			return
		}
		if progressed {
			attempt = 0
		}
		s.opts.Log.Warn("pub/sub connection lost; reconnecting", slog.Any("error", err), slog.Int("attempt", attempt))
		t := time.NewTimer(s.opts.Backoff.Delay(attempt, s.opts.Rand))
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return
		}
		attempt++
	}
}

// connect opens a new connection and resubscribes every room in one command.
func (s *Subscriber) connect(ctx context.Context) *redis.PubSub {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	s.pending = map[string]int{}
	ps := s.rdb.Subscribe(ctx)
	s.ps = ps
	if len(s.chans) > 0 {
		chans := make([]string, 0, len(s.chans))
		for ch := range s.chans {
			chans = append(chans, ch)
			s.pending[ch] = 1
		}
		_ = ps.Subscribe(ctx, chans...) // a failure surfaces in receive
	}
	return ps
}

func (s *Subscriber) receive(ctx context.Context, ps *redis.PubSub) (progressed bool, err error) {
	pinged := false
	for {
		msg, err := ps.ReceiveTimeout(ctx, s.opts.HealthInterval)
		if err != nil {
			if isTimeout(err) && !pinged && ctx.Err() == nil {
				if err := ps.Ping(ctx); err != nil {
					return progressed, err
				}
				pinged = true
				continue
			}
			return progressed, err
		}
		progressed, pinged = true, false
		switch m := msg.(type) {
		case *redis.Subscription:
			if m.Kind == "subscribe" {
				s.confirmed(m.Channel)
			}
		case *redis.Message:
			if code, ok := codeOf(m.Channel); ok {
				s.hooks.OnMessage(code, m.Payload)
			}
		}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// confirmed records Redis's confirmation of one SUBSCRIBE for the channel.
func (s *Subscriber) confirmed(ch string) {
	s.mu.Lock()
	if s.pending[ch] > 0 {
		s.pending[ch]--
	}
	st := s.chans[ch]
	if st == nil || s.pending[ch] > 0 { // dropped, or a newer SUBSCRIBE is still in flight
		s.mu.Unlock()
		return
	}
	restored := st.everConfirmed && st.confirmedEpoch != s.epoch
	st.confirmedEpoch, st.everConfirmed = s.epoch, true
	waiters := st.waiters
	st.waiters = nil
	s.mu.Unlock()
	for _, w := range waiters {
		close(w)
	}
	if restored {
		s.hooks.OnResubscribed(st.code)
	}
}

// Ensure subscribes to the room if needed and returns once Redis has confirmed the subscription.
func (s *Subscriber) Ensure(ctx context.Context, code quiz.Code) error {
	ch := redisx.RoomChannel(string(code))
	s.mu.Lock()
	st := s.chans[ch]
	if st == nil {
		st = &subState{code: code}
		s.chans[ch] = st
		if s.ps != nil {
			s.pending[ch]++
			_ = s.ps.Subscribe(ctx, ch) // a failure ends the connection; the reconnect resubscribes
		}
	}
	if st.everConfirmed && st.confirmedEpoch == s.epoch && s.pending[ch] == 0 {
		s.mu.Unlock()
		return nil
	}
	w := make(chan struct{})
	st.waiters = append(st.waiters, w)
	s.mu.Unlock()
	select {
	case <-w:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Drop unsubscribes from the room unless it has local connections again.
func (s *Subscriber) Drop(code quiz.Code) {
	ch := redisx.RoomChannel(string(code))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hooks.Has(code) {
		return
	}
	if _, ok := s.chans[ch]; !ok {
		return
	}
	delete(s.chans, ch)
	if s.ps != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.ps.Unsubscribe(ctx, ch)
	}
}

func codeOf(channel string) (quiz.Code, bool) {
	c, ok := strings.CutPrefix(channel, "room:{")
	if !ok || !strings.HasSuffix(c, "}") {
		return "", false
	}
	return quiz.Code(strings.TrimSuffix(c, "}")), true
}
