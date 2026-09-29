package realtime

import (
	"context"
	"errors"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

// Per-call timeouts (TRD §9.2); the question-set load inside Enter keeps its own 10 s budget.
const (
	redisTimeout   = 500 * time.Millisecond
	enterTimeout   = 10 * time.Second
	finalTimeout   = 2 * time.Second
	busyRetryAfter = time.Second
)

// Joiner adds participants to a room (session.RedisRepository).
type Joiner interface {
	Join(ctx context.Context, in session.JoinInput) (session.JoinResult, error)
}

// RoomLookup reads a room's control record (quiz.RedisRepository).
type RoomLookup interface {
	Room(ctx context.Context, code quiz.Code) (quiz.RoomRecord, error)
}

// AnswerSubmitter records answers (scoring.Service).
type AnswerSubmitter interface {
	SubmitAnswer(ctx context.Context, in scoring.AnswerInput) (scoring.AnswerResult, error)
}

// FinalResults reads archived quizzes (leaderboard.PostgresStore).
type FinalResults interface {
	Final(ctx context.Context, code quiz.Code, participantID quiz.ParticipantID) (leaderboard.Final, error)
}

// Clock gives Redis-aligned time (RedisClock).
type Clock interface{ NowMs() int64 }

// HandlerDeps are what the message handler works with.
type HandlerDeps struct {
	Hub     *Hub
	Joiner  Joiner
	Rooms   RoomLookup
	Answers AnswerSubmitter
	Finals  FinalResults
	Clock   Clock
	DataTTL time.Duration // QUIZ_DATA_TTL, refreshed on join
}

// MessageHandler handles client messages (TRD §7.3).
type MessageHandler struct{ d HandlerDeps }

// NewMessageHandler returns the gateway's handler.
func NewMessageHandler(d HandlerDeps) *MessageHandler { return &MessageHandler{d: d} }

// Handle runs one message on the connection's read goroutine.
func (h *MessageHandler) Handle(ctx context.Context, c *Conn, m protocol.ClientMessage) {
	switch p := m.Payload.(type) {
	case protocol.Join:
		h.join(ctx, c, m.ID, p)
	case protocol.Watch:
		h.watch(ctx, c, m.ID, p)
	case protocol.SubmitAnswer:
		h.submit(ctx, c, m.ID, p)
	case protocol.Ping:
		c.Reply(m.ID, protocol.TypePong, protocol.Pong{ClientTime: p.ClientTime, ServerTime: h.d.Clock.NowMs()})
	}
}

// Snapshot resyncs a slow client.
func (h *MessageHandler) Snapshot(ctx context.Context, c *Conn) ([]byte, error) {
	return h.d.Hub.Snapshot(ctx, c)
}

// Closed takes the connection out of its room.
func (h *MessageHandler) Closed(c *Conn) { h.d.Hub.Leave(c) }

// join follows the order the task-19 end-to-end test proved: set → Enter → join script → snapshot.
func (h *MessageHandler) join(ctx context.Context, c *Conn, id string, p protocol.Join) {
	claims := c.Claims()
	if claims.Role != auth.RoleParticipant {
		h.fail(c, id, rejection(protocol.CodeForbidden))
		return
	}
	if _, ok := c.Member(); ok {
		h.fail(c, id, rejection(protocol.CodeAlreadyJoined))
		return
	}
	name, err := session.NormalizeDisplayName(p.DisplayName)
	if err != nil {
		h.fail(c, id, err)
		return
	}
	code, err := quiz.ParseCode(p.QuizCode)
	if err != nil {
		h.fail(c, id, err)
		return
	}
	pid := quiz.ParticipantID(claims.ParticipantID)
	setID, known := h.d.Hub.reg.setOf(code) // a join burst reads the room once per gateway
	if !known {
		rec, err := call(ctx, redisTimeout, func(ctx context.Context) (quiz.RoomRecord, error) { return h.d.Rooms.Room(ctx, code) })
		switch {
		case errors.Is(err, quiz.ErrUnknownQuiz):
			h.replyArchived(ctx, c, id, code, pid, false)
			return
		case err != nil:
			h.fail(c, id, err)
			return
		case rec.Status == quiz.StatusExpired:
			h.fail(c, id, quiz.ErrQuizExpired)
			return
		case rec.Status == quiz.StatusFinished:
			h.replyFinished(ctx, c, id, code, pid, name)
			return
		}
		setID = rec.QuestionSetID
	}
	m := Member{Code: code, SetID: setID, ParticipantID: pid, DisplayName: name}
	if err := h.enter(ctx, c, m); err != nil {
		h.fail(c, id, err)
		return
	}
	res, err := call(ctx, redisTimeout, func(ctx context.Context) (session.JoinResult, error) {
		return h.d.Joiner.Join(ctx, session.JoinInput{Code: code, ParticipantID: pid, DisplayName: name, ConnID: c.ID(), TTL: h.d.DataTTL})
	})
	switch {
	case errors.Is(err, quiz.ErrUnknownQuiz):
		h.d.Hub.Leave(c)
		h.replyArchived(ctx, c, id, code, pid, false)
		return
	case err != nil:
		h.d.Hub.Leave(c)
		h.fail(c, id, err)
		return
	case res.Finished:
		h.d.Hub.Leave(c)
		h.replyFinished(ctx, c, id, code, pid, name)
		return
	}
	st := session.Standing{Found: true, Score: res.Score, Rank: res.Rank}
	if hasQuestion(res.Room.Status) {
		st.Answer = h.ownAnswer(ctx, c, code, res.Room.QuestionID, pid)
	}
	set, _ := h.d.Hub.cache.Get(setID)
	view := session.RoomView{Room: res.Room, ParticipantCount: res.ParticipantCount, Top: res.Top, ServerTime: res.ServerTime}
	c.Reply(id, protocol.TypeSnapshot, BuildSnapshot(view, set, m, st))
}

// ownAnswer reads the participant's accepted answer to the current question; on failure the
// snapshot goes without it and a resend still gets the original result (FR-30).
func (h *MessageHandler) ownAnswer(ctx context.Context, c *Conn, code quiz.Code, qid quiz.QuestionID, pid quiz.ParticipantID) *session.Answer {
	res, err := call(ctx, redisTimeout, func(ctx context.Context) (session.Standings, error) {
		return h.d.Hub.rooms.Standings(ctx, code, qid, []quiz.ParticipantID{pid})
	})
	if err != nil {
		c.s.log.Warn("own answer lookup failed", "quiz_code", string(code), "participant_id", string(pid), "error", err)
		return nil
	}
	return res.ByID[pid].Answer
}

func (h *MessageHandler) watch(ctx context.Context, c *Conn, id string, p protocol.Watch) {
	claims := c.Claims()
	if claims.Role != auth.RoleHost {
		h.fail(c, id, rejection(protocol.CodeForbidden))
		return
	}
	if _, ok := c.Member(); ok {
		h.fail(c, id, rejection(protocol.CodeAlreadyJoined))
		return
	}
	code, err := quiz.ParseCode(p.QuizCode)
	if err != nil {
		h.fail(c, id, err)
		return
	}
	rec, err := call(ctx, redisTimeout, func(ctx context.Context) (quiz.RoomRecord, error) { return h.d.Rooms.Room(ctx, code) })
	switch {
	case errors.Is(err, quiz.ErrUnknownQuiz):
		h.replyArchived(ctx, c, id, code, "", true)
		return
	case err != nil:
		h.fail(c, id, err)
		return
	case string(rec.HostID) != claims.ParticipantID:
		h.fail(c, id, rejection(protocol.CodeForbidden))
		return
	case rec.Status == quiz.StatusExpired:
		h.fail(c, id, quiz.ErrQuizExpired)
		return
	case rec.Status == quiz.StatusFinished:
		h.replyFinished(ctx, c, id, code, "", "")
		return
	}
	m := Member{Code: code, SetID: rec.QuestionSetID}
	if err := h.enter(ctx, c, m); err != nil {
		h.fail(c, id, err)
		return
	}
	snap, err := h.d.Hub.snapshotFor(ctx, m)
	if err != nil {
		h.d.Hub.Leave(c)
		h.fail(c, id, err)
		return
	}
	c.Reply(id, protocol.TypeSnapshot, snap)
}

func (h *MessageHandler) enter(ctx context.Context, c *Conn, m Member) error {
	ctx, cancel := context.WithTimeout(ctx, enterTimeout)
	defer cancel()
	return h.d.Hub.Enter(ctx, c, m)
}

// replyFinished answers a join or watch (empty pid) of a finished quiz still in Redis, read-only (FR-13).
// The participant's own part uses the name they just sent; standings carry no names.
func (h *MessageHandler) replyFinished(ctx context.Context, c *Conn, id string, code quiz.Code, pid quiz.ParticipantID, name string) {
	host := pid == ""
	rctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()
	v, err := h.d.Hub.view(rctx, code)
	if errors.Is(err, quiz.ErrUnknownQuiz) {
		h.replyArchived(ctx, c, id, code, pid, host) // released in between
		return
	}
	if err != nil {
		h.fail(c, id, err)
		return
	}
	var st session.Standing
	if !host {
		res, err := h.d.Hub.rooms.Standings(rctx, code, "", []quiz.ParticipantID{pid})
		if err != nil {
			h.fail(c, id, err)
			return
		}
		st = res.ByID[pid]
	}
	snap := BuildSnapshot(v, nil, Member{Code: code, ParticipantID: pid, DisplayName: name}, st)
	if !host && !st.Found {
		snap.You = nil // never played
	}
	c.Reply(id, protocol.TypeSnapshot, snap)
}

// replyArchived answers from final results once the live data is released (FR-13). There is no live
// state version any more; clients apply a finished state whatever its version (asyncapi.yaml).
func (h *MessageHandler) replyArchived(ctx context.Context, c *Conn, id string, code quiz.Code, pid quiz.ParticipantID, host bool) {
	f, err := call(ctx, finalTimeout, func(ctx context.Context) (leaderboard.Final, error) { return h.d.Finals.Final(ctx, code, pid) })
	switch {
	case err != nil:
		h.fail(c, id, err)
		return
	case f.Status == quiz.StatusExpired:
		h.fail(c, id, quiz.ErrQuizExpired)
		return
	case host && string(f.HostID) != c.Claims().ParticipantID:
		h.fail(c, id, rejection(protocol.CodeForbidden))
		return
	}
	snap := protocol.Snapshot{
		QuizCode: string(code), Role: protocol.RoleParticipant,
		Quiz:        protocol.QuizState{Status: quiz.StatusFinished, QuestionIndex: f.QuestionCount - 1, QuestionCount: f.QuestionCount},
		Leaderboard: protocol.Leaderboard{ParticipantCount: f.ParticipantCount, Top: wireEntries(f.Top)},
		ServerTime:  h.d.Clock.NowMs(),
	}
	if host {
		snap.Role = protocol.RoleHost
	} else if f.You != nil {
		snap.You = &protocol.You{ParticipantID: string(f.You.ParticipantID), DisplayName: f.You.DisplayName, Score: f.You.Score, Rank: f.You.Rank}
	}
	c.Reply(id, protocol.TypeSnapshot, snap)
}

// submit checks the answer against the cached question set before any Redis call (TRD §7.3).
func (h *MessageHandler) submit(ctx context.Context, c *Conn, id string, p protocol.SubmitAnswer) {
	m, ok := c.Member()
	if !ok || m.ParticipantID == "" {
		h.fail(c, id, scoring.ErrNotJoined)
		return
	}
	set, ok := h.d.Hub.cache.Get(m.SetID)
	if !ok {
		h.fail(c, id, errors.New("question set not cached for a joined connection"))
		return
	}
	q, ok := set.Question(quiz.QuestionID(p.QuestionID))
	if !ok {
		h.fail(c, id, scoring.ErrWrongQuestion)
		return
	}
	opt := quiz.OptionID(p.OptionID)
	if !q.HasOption(opt) {
		h.fail(c, id, rejection(protocol.CodeInvalidOption))
		return
	}
	res, err := h.d.Answers.SubmitAnswer(ctx, scoring.AnswerInput{Code: m.Code, ParticipantID: m.ParticipantID,
		QuestionID: q.ID, OptionID: opt, Correct: q.IsCorrect(opt)})
	if err != nil {
		h.fail(c, id, err)
		return
	}
	status := protocol.AnswerAccepted
	if res.Status == scoring.Duplicate {
		status = protocol.AnswerDuplicate
	}
	c.Reply(id, protocol.TypeAnswerResult, protocol.AnswerResult{QuestionID: string(q.ID), Status: status,
		OptionID: string(res.OptionID), Correct: res.Correct, Points: res.Points, TotalScore: res.Total, ReceivedAt: res.ReceivedAt})
}

// rejection is a request the handler refuses itself.
type rejection protocol.ErrorCode

func (r rejection) Error() string { return string(r) }

// classify maps a failure to its error code (TRD §9.6).
func classify(err error) protocol.ErrorCode {
	var r rejection
	switch {
	case errors.As(err, &r):
		return protocol.ErrorCode(r)
	case errors.Is(err, quiz.ErrUnknownQuiz), errors.Is(err, quiz.ErrInvalidCode):
		return protocol.CodeUnknownQuiz
	case errors.Is(err, quiz.ErrQuizExpired):
		return protocol.CodeQuizExpired
	case errors.Is(err, scoring.ErrQuestionClosed):
		return protocol.CodeQuestionClosed
	case errors.Is(err, scoring.ErrWrongQuestion):
		return protocol.CodeWrongQuestion
	case errors.Is(err, scoring.ErrNotJoined):
		return protocol.CodeNotJoined
	case errors.Is(err, session.ErrInvalidDisplayName):
		return protocol.CodeInvalidDisplayName
	case errors.Is(err, scoring.ErrInternal):
		return protocol.CodeInternal
	case errors.Is(err, scoring.ErrServerBusy), errors.Is(err, retry.ErrBudgetExhausted), retry.Transient(err):
		return protocol.CodeServerBusy
	}
	return protocol.CodeInternal
}

// messages are what clients read; causes stay in the logs.
var messages = map[protocol.ErrorCode]string{
	protocol.CodeForbidden:          "not allowed for this token",
	protocol.CodeUnknownQuiz:        "no quiz with this code",
	protocol.CodeQuizExpired:        "this quiz has expired",
	protocol.CodeAlreadyJoined:      "this connection has already joined a quiz",
	protocol.CodeNotJoined:          "join the quiz as a participant first",
	protocol.CodeInvalidDisplayName: "display name is not allowed",
	protocol.CodeWrongQuestion:      "not the current question",
	protocol.CodeQuestionClosed:     "the question has closed",
	protocol.CodeInvalidOption:      "the option is not part of this question",
	protocol.CodeServerBusy:         "server busy; retry with the same request id",
	protocol.CodeInternal:           "internal error",
}

func (h *MessageHandler) fail(c *Conn, id string, err error) {
	if c.Context().Err() != nil {
		return // closing: nothing more is sent
	}
	code := classify(err)
	var retryAfter time.Duration
	if code == protocol.CodeServerBusy {
		retryAfter = busyRetryAfter
	}
	c.replyError(id, code, messages[code], retryAfter, err)
}

func call[T any](ctx context.Context, timeout time.Duration, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fn(ctx)
}

func hasQuestion(s quiz.Status) bool {
	return s == quiz.StatusQuestionOpen || s == quiz.StatusQuestionClosed
}
