package realtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sync/singleflight"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

// QuestionCache is the gateway's question-set cache (quiz.Cache).
type QuestionCache interface {
	Acquire(ctx context.Context, id quiz.QuestionSetID) (*quiz.QuestionSet, error)
	Release(id quiz.QuestionSetID)
	Get(id quiz.QuestionSetID) (*quiz.QuestionSet, bool)
}

// RoomReader reads snapshot parts from Redis (session.RedisRepository).
type RoomReader interface {
	View(ctx context.Context, code quiz.Code) (session.RoomView, error)
	Standings(ctx context.Context, code quiz.Code, questionID quiz.QuestionID, ids []quiz.ParticipantID) (session.Standings, error)
}

// Subscriptions keeps this gateway subscribed to the rooms it has connections in (Subscriber).
type Subscriptions interface {
	// Ensure subscribes to the room and returns once Redis has confirmed it.
	Ensure(ctx context.Context, code quiz.Code) error
	Drop(code quiz.Code)
}

// HubOptions tune the hub; zero values use the defaults.
type HubOptions struct {
	Shards            int           // REGISTRY_SHARDS
	FanoutConcurrency int           // Redis reads for rank and resubscribe fan-outs running at once
	ReadTimeout       time.Duration // per Redis read made for a fan-out or a shared room view
	Log               *slog.Logger
}

// Hub connects local connections to their rooms: registry, room events, and snapshots (TRD §7.4, §7.5).
type Hub struct {
	reg         *registry
	subs        Subscriptions
	cache       QuestionCache
	rooms       RoomReader
	log         *slog.Logger
	fanout      chan struct{}
	readTimeout time.Duration
	views       singleflight.Group
}

// NewHub returns a hub; call Attach before use.
func NewHub(cache QuestionCache, rooms RoomReader, o HubOptions) *Hub {
	if o.Shards <= 0 {
		o.Shards = 64
	}
	if o.FanoutConcurrency <= 0 {
		o.FanoutConcurrency = 16
	}
	if o.ReadTimeout <= 0 {
		o.ReadTimeout = 2 * time.Second
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Hub{reg: newRegistry(o.Shards), cache: cache, rooms: rooms, log: o.Log,
		fanout: make(chan struct{}, o.FanoutConcurrency), readTimeout: o.ReadTimeout}
}

// Attach sets the subscriptions; the subscriber and the hub refer to each other.
func (h *Hub) Attach(s Subscriptions) { h.subs = s }

// Enter puts c in its room: a question-set reference, the registry, and a confirmed subscription.
// The caller reads the room's snapshot only after Enter returns, so no event falls in between.
func (h *Hub) Enter(ctx context.Context, c *Conn, m Member) error {
	if _, err := h.cache.Acquire(ctx, m.SetID); err != nil {
		return fmt.Errorf("question set %s: %w", m.SetID, err)
	}
	c.member.Store(&m)
	if old := h.reg.add(c, m); old != nil {
		old.Close(CloseReplaced, "replaced by a newer connection")
	}
	if err := h.subs.Ensure(ctx, m.Code); err != nil {
		h.Leave(c)
		return fmt.Errorf("subscribe to %s: %w", m.Code, err)
	}
	return nil
}

// Leave takes c out of its room; the last connection out drops the subscription. Safe to call twice.
func (h *Hub) Leave(c *Conn) {
	m := c.member.Swap(nil)
	if m == nil {
		return
	}
	removed, last := h.reg.remove(c, *m)
	if !removed {
		return
	}
	h.cache.Release(m.SetID)
	if last {
		h.subs.Drop(m.Code)
	}
}

// Has reports whether this gateway has a connection in the room.
func (h *Hub) Has(code quiz.Code) bool { return h.reg.has(code) }

// Codes lists the rooms this gateway has connections in.
func (h *Hub) Codes() []quiz.Code { return h.reg.codes() }

// OnMessage handles one room event; it runs on the subscriber goroutine, so Redis reads go to fan-outs.
func (h *Hub) OnMessage(code quiz.Code, payload string) {
	ev, err := decodeEvent([]byte(payload))
	if err != nil {
		h.log.Warn("undecodable room event", slog.String("quiz_code", string(code)), slog.Any("error", err))
		return
	}
	switch ev.T {
	case eventKick:
		if c := h.reg.participant(code, ev.P); c != nil && c.ID() != ev.K {
			c.Close(CloseReplaced, "replaced by a newer connection")
		}
		return
	case eventState, eventFinished:
		if !h.reg.advance(code, stateCounter, int64(ev.V)) {
			return
		}
	case eventLeaderboard:
		if !h.reg.advance(code, lbCounter, int64(ev.V)) {
			return
		}
	default:
		return // unknown types are ignored (TRD §4.3)
	}
	setID, ok := h.reg.setOf(code)
	if !ok {
		return
	}
	typ, msg, ok := h.clientMessage(setID, ev)
	if !ok {
		return
	}
	h.broadcast(code, typ, msg)
	if ev.T == eventState && ev.S == quiz.StatusQuestionClosed {
		h.fanOut(func(ctx context.Context) { h.sendRanks(ctx, code, ev.Q) })
	}
}

// clientMessage maps a room event to the client message it becomes (TRD §7.5).
func (h *Hub) clientMessage(setID quiz.QuestionSetID, ev roomEvent) (protocol.Type, any, bool) {
	switch ev.T {
	case eventState:
		switch ev.S {
		case quiz.StatusQuestionOpen, quiz.StatusQuestionClosed:
			q, ok := h.question(setID, ev.Q)
			if !ok {
				return "", nil, false
			}
			if ev.S == quiz.StatusQuestionClosed {
				return protocol.TypeQuestionClosed, protocol.QuestionClosed{QuestionID: string(q.ID),
					CorrectOptionID: string(q.CorrectOptionID), NextTransitionAt: int64(ev.X), StateVersion: int64(ev.V)}, true
			}
			pq := protocol.QuestionFrom(q.Public(), int(ev.I), int(ev.N), int64(ev.O), int64(ev.D), int64(ev.C))
			return protocol.TypeQuestion, protocol.QuestionMsg{Question: pq, StateVersion: int64(ev.V)}, true
		default:
			return protocol.TypeQuizState, protocol.QuizState{Status: ev.S, QuestionIndex: int(ev.I),
				QuestionCount: int(ev.N), StateVersion: int64(ev.V)}, true
		}
	case eventFinished:
		return protocol.TypeQuizFinished, protocol.QuizFinished{FinalTop: wireEntries(ev.Top),
			ParticipantCount: int(ev.N), StateVersion: int64(ev.V)}, true
	case eventLeaderboard:
		return protocol.TypeLeaderboard, protocol.Leaderboard{Version: int64(ev.V),
			ParticipantCount: int(ev.N), Top: wireEntries(ev.Top)}, true
	}
	return "", nil, false
}

func (h *Hub) question(setID quiz.QuestionSetID, id quiz.QuestionID) (quiz.Question, bool) {
	set, ok := h.cache.Get(setID)
	if !ok {
		h.log.Error("question set not cached for a local room", slog.String("set_id", string(setID)))
		return quiz.Question{}, false
	}
	q, ok := set.Question(id)
	if !ok {
		h.log.Error("room event names a question not in its set", slog.String("set_id", string(setID)), slog.String("question_id", string(id)))
	}
	return q, ok
}

// broadcast encodes a message once and queues the same prepared frame on every local connection (NFR-5c).
func (h *Hub) broadcast(code quiz.Code, typ protocol.Type, msg any) {
	frame, err := protocol.Encode(typ, "", msg)
	if err != nil {
		h.log.Error("encode broadcast", slog.String("type", string(typ)), slog.Any("error", err))
		return
	}
	pm, err := websocket.NewPreparedMessage(websocket.TextMessage, frame)
	if err != nil {
		h.log.Error("prepare broadcast", slog.Any("error", err))
		return
	}
	for _, c := range h.reg.conns(code) {
		c.SendPrepared(pm)
	}
}

// fanOut runs fn off the subscriber goroutine, with a bounded number running at once.
func (h *Hub) fanOut(fn func(ctx context.Context)) {
	go func() {
		h.fanout <- struct{}{}
		defer func() { <-h.fanout }()
		ctx, cancel := context.WithTimeout(context.Background(), h.readTimeout)
		defer cancel()
		fn(ctx)
	}()
}

// sendRanks sends each local participant their own rank after a close, from one Redis read (FR-25).
func (h *Hub) sendRanks(ctx context.Context, code quiz.Code, qid quiz.QuestionID) {
	local := h.reg.participants(code)
	if len(local) == 0 {
		return
	}
	st, err := h.rooms.Standings(ctx, code, "", ids(local))
	if err != nil {
		h.log.Warn("rank lookup failed", slog.String("quiz_code", string(code)), slog.Any("error", err))
		return
	}
	for id, c := range local {
		s, ok := st.ByID[id]
		if !ok || !s.Found {
			continue
		}
		c.Reply("", protocol.TypeRank, protocol.Rank{QuestionID: string(qid), Score: s.Score, Rank: s.Rank, ParticipantCount: st.ParticipantCount})
	}
}

// OnResubscribed pushes a fresh snapshot to every local connection of a room whose subscription was
// restored, reading the room once and all standings once (TRD §7.10, §9.3).
func (h *Hub) OnResubscribed(code quiz.Code) {
	h.fanOut(func(ctx context.Context) {
		conns := h.reg.conns(code)
		if len(conns) == 0 {
			return
		}
		v, err := h.view(ctx, code)
		if err != nil {
			h.log.Warn("resubscribe snapshot failed", slog.String("quiz_code", string(code)), slog.Any("error", err))
			return
		}
		st, err := h.rooms.Standings(ctx, code, v.Room.QuestionID, ids(h.reg.participants(code)))
		if err != nil {
			h.log.Warn("resubscribe standings failed", slog.String("quiz_code", string(code)), slog.Any("error", err))
			return
		}
		set, _ := h.cache.Get(v.Room.QuestionSetID)
		for _, c := range conns {
			m := c.member.Load()
			if m == nil {
				continue
			}
			if frame, err := protocol.Encode(protocol.TypeSnapshot, "", BuildSnapshot(v, set, *m, st.ByID[m.ParticipantID])); err == nil {
				c.SendBytes(frame)
			}
		}
	})
}

// Snapshot builds c's resync snapshot; concurrent resyncs in one room share a single room read (TRD §7.6).
func (h *Hub) Snapshot(ctx context.Context, c *Conn) ([]byte, error) {
	m := c.member.Load()
	if m == nil {
		return nil, nil
	}
	v, err := h.view(ctx, m.Code)
	if err != nil {
		return nil, err
	}
	var st session.Standing
	if m.ParticipantID != "" {
		res, err := h.rooms.Standings(ctx, m.Code, v.Room.QuestionID, []quiz.ParticipantID{m.ParticipantID})
		if err != nil {
			return nil, err
		}
		st = res.ByID[m.ParticipantID]
	}
	set, _ := h.cache.Get(m.SetID)
	return protocol.Encode(protocol.TypeSnapshot, "", BuildSnapshot(v, set, *m, st))
}

// view reads a room's shared part once for all concurrent callers; the read outlives a caller that gives up.
func (h *Hub) view(ctx context.Context, code quiz.Code) (session.RoomView, error) {
	ch := h.views.DoChan(string(code), func() (any, error) {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.readTimeout)
		defer cancel()
		return h.rooms.View(rctx, code)
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return session.RoomView{}, r.Err
		}
		return r.Val.(session.RoomView), nil
	case <-ctx.Done():
		return session.RoomView{}, ctx.Err()
	}
}

func ids(m map[quiz.ParticipantID]*Conn) []quiz.ParticipantID {
	out := make([]quiz.ParticipantID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}
