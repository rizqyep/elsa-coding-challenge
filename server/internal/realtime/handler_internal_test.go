package realtime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

const hcode = quiz.Code("K7Q2MX")

// recorder keeps the order in which fakes were called.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

type fakeJoiner struct {
	rec      *recorder
	mu       sync.Mutex
	inputs   []session.JoinInput
	result   session.JoinResult
	err      error
	leaveErr error
}

func (j *fakeJoiner) Join(_ context.Context, in session.JoinInput) (session.JoinResult, error) {
	j.rec.add("join")
	j.mu.Lock()
	defer j.mu.Unlock()
	j.inputs = append(j.inputs, in)
	return j.result, j.err
}

func (j *fakeJoiner) Leave(_ context.Context, code quiz.Code, id quiz.ParticipantID) (session.LeaveOutcome, error) {
	j.rec.add("leave " + string(code) + " " + string(id))
	return session.LeftRemoved, j.leaveErr
}

func (j *fakeJoiner) calls() []session.JoinInput {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.inputs)
}

type fakeLookup struct {
	calls atomic.Int64
	room  quiz.RoomRecord
	err   error
}

func (l *fakeLookup) Room(context.Context, quiz.Code) (quiz.RoomRecord, error) {
	l.calls.Add(1)
	return l.room, l.err
}

type fakeAnswers struct {
	mu     sync.Mutex
	inputs []scoring.AnswerInput
	result scoring.AnswerResult
	err    error
}

func (a *fakeAnswers) SubmitAnswer(_ context.Context, in scoring.AnswerInput) (scoring.AnswerResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inputs = append(a.inputs, in)
	return a.result, a.err
}

func (a *fakeAnswers) calls() []scoring.AnswerInput {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.inputs)
}

type fakeFinals struct {
	calls atomic.Int64
	final leaderboard.Final
	err   error
}

func (f *fakeFinals) Final(context.Context, quiz.Code, quiz.ParticipantID) (leaderboard.Final, error) {
	f.calls.Add(1)
	return f.final, f.err
}

type fixedClock int64

func (c fixedClock) NowMs() int64 { return int64(c) }

type errCounter struct {
	mu    sync.Mutex
	codes []protocol.ErrorCode
}

func (c *errCounter) add(code protocol.ErrorCode) {
	c.mu.Lock()
	c.codes = append(c.codes, code)
	c.mu.Unlock()
}
func (c *errCounter) list() []protocol.ErrorCode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.codes)
}

// openRoom is a room with its first question open.
func openRoom() quiz.RoomRecord {
	return quiz.RoomRecord{Code: hcode, QuestionSetID: testSet.ID, QuestionID: "q1",
		Room: quiz.Room{HostID: "host_1", Status: quiz.StatusQuestionOpen, QuestionIndex: 0, QuestionCount: 2,
			OpenedAt: 1_000, Deadline: 11_000, CloseAt: 11_000, StateVersion: 3}}
}

type handlerEnv struct {
	*hubEnv
	h       *MessageHandler
	rec     *recorder
	joiner  *fakeJoiner
	lookup  *fakeLookup
	answers *fakeAnswers
	finals  *fakeFinals
	errs    *errCounter
}

func newHandlerEnv(t *testing.T) *handlerEnv {
	t.Helper()
	e := newHubEnv(t)
	rec := &recorder{}
	e.subs.rec = rec
	room := openRoom()
	he := &handlerEnv{hubEnv: e, rec: rec,
		joiner: &fakeJoiner{rec: rec, result: session.JoinResult{Room: room, Score: 42, Rank: 2, ParticipantCount: 5,
			Top: []leaderboard.Entry{{Rank: 1, ParticipantID: "u_top", DisplayName: "Top", Score: 90}}, ServerTime: 5_000}},
		lookup:  &fakeLookup{room: room},
		answers: &fakeAnswers{result: scoring.AnswerResult{Status: scoring.Accepted, OptionID: "q1-b", Correct: true, Points: 186, Total: 228, ReceivedAt: 2_400}},
		finals:  &fakeFinals{err: quiz.ErrUnknownQuiz},
		errs:    &errCounter{},
	}
	he.h = NewMessageHandler(HandlerDeps{Hub: e.hub, Joiner: he.joiner, Rooms: he.lookup, Answers: he.answers,
		Finals: he.finals, Clock: fixedClock(123_456), DataTTL: time.Hour})
	opts := internalOptions(&fakeClock{now: time.Unix(1_800_000_000, 0)}, 64)
	opts.OnError = he.errs.add
	e.s = newShared(opts, he.h, nil)
	return he
}

func (he *handlerEnv) connAs(pid string, role auth.Role) (*Conn, *fakeSocket) {
	he.t.Helper()
	sock := newFakeSocket()
	return startAs(he.t, he.s, sock, auth.Claims{ParticipantID: pid, Role: role}), sock
}

func (he *handlerEnv) send(c *Conn, id string, typ protocol.Type, payload any) {
	he.h.Handle(c.Context(), c, protocol.ClientMessage{ID: id, Type: typ, Payload: payload})
}

// nth waits for the socket's n-th personal frame (1-based), decoded and checked against the contract.
func (he *handlerEnv) nth(sock *fakeSocket, n int) map[string]any {
	he.t.Helper()
	var frames []map[string]any
	waitFor(he.t, fmt.Sprintf("personal frame %d", n), func() bool {
		frames = decoded(he.t, sock)
		return len(frames) >= n
	})
	return frames[n-1]
}

func data(m map[string]any) map[string]any { return m["data"].(map[string]any) }

func wantError(t *testing.T, m map[string]any, id string, code protocol.ErrorCode) {
	t.Helper()
	if m["type"] != "error" || data(m)["code"] != string(code) || m["id"] != id {
		t.Fatalf("got %v, want error %s for request %q", m, code, id)
	}
}

func joinMsg(name string) protocol.Join {
	return protocol.Join{QuizCode: string(hcode), DisplayName: name}
}

func TestJoin_EntersTheRoomBeforeTheJoinScript(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))

	snap := he.nth(sock, 1)
	if got := he.rec.list(); !slices.Equal(got, []string{"subscribe", "join"}) {
		t.Errorf("order %v; the subscription must be confirmed before the join script reads the snapshot", got)
	}
	d := data(snap)
	if snap["type"] != "snapshot" || snap["id"] != "r1" || d["role"] != "participant" {
		t.Fatalf("got %v, want the snapshot replying to r1", snap)
	}
	you := d["you"].(map[string]any)
	if you["participantId"] != "u_1" || you["displayName"] != "Rina" || you["score"] != 42.0 || you["rank"] != 2.0 {
		t.Errorf("you = %v, want u_1 / Rina with the join result's score and rank", you)
	}
	if q, _ := d["question"].(map[string]any); q == nil || q["questionId"] != "q1" {
		t.Errorf("question = %v, want the open q1", d["question"])
	}
	if m, ok := c.Member(); !ok || m.ParticipantID != "u_1" || m.Code != hcode {
		t.Errorf("member = %+v, %v", m, ok)
	}
	in := he.joiner.calls()[0]
	if in.ConnID != c.ID() || in.DisplayName != "Rina" || in.TTL != time.Hour {
		t.Errorf("join input %+v; want this connection's ID for the kick, the name, and the data TTL", in)
	}
}

func TestJoin_KnownRoomSkipsTheRoomLookup(t *testing.T) {
	he := newHandlerEnv(t)
	for i := range 5 {
		c, sock := he.connAs(fmt.Sprintf("u_%d", i), auth.RoleParticipant)
		he.send(c, "r", protocol.TypeJoin, joinMsg("P"))
		he.nth(sock, 1)
	}
	if n := he.lookup.calls.Load(); n != 1 {
		t.Errorf("%d room lookups for 5 joins into one room; a join burst must read the room once per gateway", n)
	}
}

func TestJoin_DisplayNameIsNormalised(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("  Rina  "))
	he.nth(sock, 1)
	if got := he.joiner.calls()[0].DisplayName; got != "Rina" {
		t.Errorf("stored name %q, want it trimmed", got)
	}

	c2, sock2 := he.connAs("u_2", auth.RoleParticipant)
	he.send(c2, "r2", protocol.TypeJoin, joinMsg("bad\u0007name"))
	wantError(t, he.nth(sock2, 1), "r2", protocol.CodeInvalidDisplayName)
	if len(he.joiner.calls()) != 1 {
		t.Error("an invalid name reached Redis")
	}
}

func TestJoin_FailureLeavesTheRoomSoTheClientCanRetry(t *testing.T) {
	he := newHandlerEnv(t)
	he.joiner.err = fmt.Errorf("join: %w", context.DeadlineExceeded)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))

	e := he.nth(sock, 1)
	wantError(t, e, "r1", protocol.CodeServerBusy)
	if d := data(e); d["retryable"] != true || d["retryAfterMs"] == nil {
		t.Errorf("error %v; server_busy must be retryable with retryAfterMs", d)
	}
	if _, ok := c.Member(); ok || he.hub.Has(hcode) {
		t.Fatal("a failed join left the connection in the room")
	}
	if he.cache.acquired.Load() != he.cache.released.Load() {
		t.Errorf("question set acquired %d, released %d", he.cache.acquired.Load(), he.cache.released.Load())
	}

	he.joiner.err = nil
	he.send(c, "r2", protocol.TypeJoin, joinMsg("Rina"))
	if m := he.nth(sock, 2); m["type"] != "snapshot" {
		t.Errorf("retry got %v, want a snapshot", m)
	}
}

func TestJoin_OneQuizPerConnection(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
	he.nth(sock, 1)
	he.send(c, "r2", protocol.TypeJoin, protocol.Join{QuizCode: "BBBBBB", DisplayName: "Rina"})
	wantError(t, he.nth(sock, 2), "r2", protocol.CodeAlreadyJoined)
	if m, _ := c.Member(); m.Code != hcode {
		t.Errorf("member moved to %s", m.Code)
	}
}

func TestRoles(t *testing.T) {
	he := newHandlerEnv(t)
	host, hs := he.connAs("host_1", auth.RoleHost)
	he.send(host, "r1", protocol.TypeJoin, joinMsg("Host"))
	wantError(t, he.nth(hs, 1), "r1", protocol.CodeForbidden)

	p, ps := he.connAs("u_1", auth.RoleParticipant)
	he.send(p, "r2", protocol.TypeWatch, protocol.Watch{QuizCode: string(hcode)})
	wantError(t, he.nth(ps, 1), "r2", protocol.CodeForbidden)

	other, os := he.connAs("host_2", auth.RoleHost)
	he.send(other, "r3", protocol.TypeWatch, protocol.Watch{QuizCode: string(hcode)})
	wantError(t, he.nth(os, 1), "r3", protocol.CodeForbidden)
	if he.hub.Has(hcode) {
		t.Error("a rejected caller entered the room")
	}
}

func TestWatch_HostGetsASnapshotAndIsNotAParticipant(t *testing.T) {
	he := newHandlerEnv(t)
	he.rooms.view = session.RoomView{Room: openRoom(), ParticipantCount: 5, ServerTime: 5_000}
	host, sock := he.connAs("host_1", auth.RoleHost)
	he.send(host, "r1", protocol.TypeWatch, protocol.Watch{QuizCode: string(hcode)})

	snap := he.nth(sock, 1)
	d := data(snap)
	if snap["type"] != "snapshot" || snap["id"] != "r1" || d["role"] != "host" || d["you"] != nil {
		t.Fatalf("got %v, want a host snapshot without a participant part", snap)
	}
	if !he.hub.Has(hcode) || len(he.hub.reg.participants(hcode)) != 0 {
		t.Error("the host must be in the room as a watcher, not a participant")
	}
	if len(he.joiner.calls()) != 0 {
		t.Error("watching ran the join script; the host doesn't play")
	}
}

func TestJoin_ExpiredAndUnknown(t *testing.T) {
	cases := []struct {
		name  string
		setup func(he *handlerEnv)
		want  protocol.ErrorCode
	}{
		{"room expired", func(he *handlerEnv) { he.lookup.room.Status = quiz.StatusExpired }, protocol.CodeQuizExpired},
		{"released and archived as expired", func(he *handlerEnv) {
			he.lookup.err = quiz.ErrUnknownQuiz
			he.finals.err, he.finals.final = nil, leaderboard.Final{Status: quiz.StatusExpired}
		}, protocol.CodeQuizExpired},
		{"never existed", func(he *handlerEnv) { he.lookup.err = quiz.ErrUnknownQuiz }, protocol.CodeUnknownQuiz},
		{"expired during the join", func(he *handlerEnv) { he.joiner.err = quiz.ErrQuizExpired }, protocol.CodeQuizExpired},
		{"released during the join", func(he *handlerEnv) { he.joiner.err = quiz.ErrUnknownQuiz }, protocol.CodeUnknownQuiz},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			he := newHandlerEnv(t)
			tc.setup(he)
			c, sock := he.connAs("u_1", auth.RoleParticipant)
			he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
			wantError(t, he.nth(sock, 1), "r1", tc.want)
			if he.hub.Has(hcode) {
				t.Error("left in the room after a rejection")
			}
		})
	}
}

func finishedRoom() quiz.RoomRecord {
	r := openRoom()
	r.Status, r.QuestionID, r.QuestionIndex, r.StateVersion = quiz.StatusFinished, "", 1, 9
	return r
}

func TestJoin_FinishedQuizStillInRedis(t *testing.T) {
	he := newHandlerEnv(t)
	he.lookup.room = finishedRoom()
	he.rooms.view = session.RoomView{Room: finishedRoom(), ParticipantCount: 3, ServerTime: 9_000,
		Top: []leaderboard.Entry{{Rank: 1, ParticipantID: "u_1", DisplayName: "Rina", Score: 300}}}
	he.rooms.standings = map[quiz.ParticipantID]session.Standing{"u_1": {Found: true, Score: 300, Rank: 1}}

	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
	snap := he.nth(sock, 1)
	d := data(snap)
	if snap["type"] != "snapshot" || d["quiz"].(map[string]any)["status"] != "finished" || d["quiz"].(map[string]any)["stateVersion"] != 9.0 {
		t.Fatalf("got %v, want a finished snapshot with the room's real version", snap)
	}
	if you, _ := d["you"].(map[string]any); you == nil || you["score"] != 300.0 {
		t.Errorf("you = %v, want the final score", d["you"])
	}
	if len(he.joiner.calls()) != 0 || he.hub.Has(hcode) {
		t.Error("a finished quiz must not be joined; the reply is read-only")
	}
}

func TestJoin_FinishedBetweenLookupAndJoin(t *testing.T) {
	he := newHandlerEnv(t)
	he.joiner.result = session.JoinResult{Finished: true}
	he.rooms.view = session.RoomView{Room: finishedRoom(), ParticipantCount: 1}
	c, sock := he.connAs("u_new", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Late"))
	snap := he.nth(sock, 1)
	d := data(snap)
	if d["quiz"].(map[string]any)["status"] != "finished" || d["role"] != "participant" || d["you"] != nil {
		t.Fatalf("got %v; want a finished snapshot, no own part for someone who never played", snap)
	}
	if he.hub.Has(hcode) || he.cache.acquired.Load() != he.cache.released.Load() {
		t.Error("the connection stayed in the room of a finished quiz")
	}
}

func TestJoin_FinishedAndReleasedIsServedFromFinalResults(t *testing.T) {
	he := newHandlerEnv(t)
	he.lookup.err = quiz.ErrUnknownQuiz
	you := leaderboard.Entry{Rank: 4, ParticipantID: "u_1", DisplayName: "Rina", Score: 120}
	he.finals.err = nil
	he.finals.final = leaderboard.Final{Status: quiz.StatusFinished, HostID: "host_1", QuestionCount: 2, ParticipantCount: 15,
		Top: []leaderboard.Entry{{Rank: 1, ParticipantID: "u_top", DisplayName: "Top", Score: 390}}, You: &you}

	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
	d := data(he.nth(sock, 1))
	q := d["quiz"].(map[string]any)
	if q["status"] != "finished" || q["questionCount"] != 2.0 || q["questionIndex"] != 1.0 {
		t.Errorf("quiz = %v, want finished at the last of 2 questions", q)
	}
	lb := d["leaderboard"].(map[string]any)
	if lb["participantCount"] != 15.0 || len(lb["top"].([]any)) != 1 {
		t.Errorf("leaderboard = %v", lb)
	}
	if y := d["you"].(map[string]any); y["rank"] != 4.0 || y["score"] != 120.0 {
		t.Errorf("you = %v, want the archived rank and score", y)
	}

	host, hs := he.connAs("host_1", auth.RoleHost)
	he.send(host, "r2", protocol.TypeWatch, protocol.Watch{QuizCode: string(hcode)})
	if d := data(he.nth(hs, 1)); d["role"] != "host" || d["you"] != nil {
		t.Errorf("host got %v", d)
	}
	stranger, ss := he.connAs("host_9", auth.RoleHost)
	he.send(stranger, "r3", protocol.TypeWatch, protocol.Watch{QuizCode: string(hcode)})
	wantError(t, he.nth(ss, 1), "r3", protocol.CodeForbidden)
}

func (he *handlerEnv) joined(pid string) (*Conn, *fakeSocket) {
	he.t.Helper()
	c, sock := he.connAs(pid, auth.RoleParticipant)
	he.send(c, "j", protocol.TypeJoin, joinMsg("P"))
	he.nth(sock, 1)
	return c, sock
}

func TestSubmit_CacheChecksNeverReachRedis(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.joined("u_1")
	he.send(c, "a1", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q9", OptionID: "q9-a"})
	wantError(t, he.nth(sock, 2), "a1", protocol.CodeWrongQuestion)
	he.send(c, "a2", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q1", OptionID: "q2-a"})
	wantError(t, he.nth(sock, 3), "a2", protocol.CodeInvalidOption)
	if n := len(he.answers.calls()); n != 0 {
		t.Errorf("%d answer script calls; both are answered from the cache", n)
	}
}

func TestSubmit_CorrectnessComesFromTheAnswerKey(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.joined("u_1")
	he.send(c, "a1", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q1", OptionID: "q1-b"})
	he.send(c, "a2", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q1", OptionID: "q1-a"})
	he.nth(sock, 3)
	calls := he.answers.calls()
	want := []scoring.AnswerInput{
		{Code: hcode, ParticipantID: "u_1", QuestionID: "q1", OptionID: "q1-b", Correct: true},
		{Code: hcode, ParticipantID: "u_1", QuestionID: "q1", OptionID: "q1-a", Correct: false},
	}
	if !slices.Equal(calls, want) {
		t.Errorf("answer inputs %+v, want %+v", calls, want)
	}
	res := he.nth(sock, 2)
	d := data(res)
	if res["type"] != "answer_result" || res["id"] != "a1" || d["status"] != "accepted" || d["points"] != 186.0 ||
		d["totalScore"] != 228.0 || d["receivedAt"] != 2400.0 || d["correct"] != true {
		t.Errorf("got %v", res)
	}
}

func TestSubmit_RequiresAJoinedParticipant(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "a1", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q1", OptionID: "q1-b"})
	wantError(t, he.nth(sock, 1), "a1", protocol.CodeNotJoined)

	he.rooms.view = session.RoomView{Room: openRoom()}
	host, hs := he.connAs("host_1", auth.RoleHost)
	he.send(host, "w", protocol.TypeWatch, protocol.Watch{QuizCode: string(hcode)})
	he.nth(hs, 1)
	he.send(host, "a2", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q1", OptionID: "q1-b"})
	wantError(t, he.nth(hs, 2), "a2", protocol.CodeNotJoined)
	if len(he.answers.calls()) != 0 {
		t.Error("an answer from a non-participant reached Redis")
	}
}

// TRD §9.6: every failure maps to one error code.
func TestSubmit_ErrorMapping(t *testing.T) {
	cases := []struct {
		err       error
		code      protocol.ErrorCode
		retryable bool
	}{
		{scoring.ErrQuestionClosed, protocol.CodeQuestionClosed, false},
		{scoring.ErrWrongQuestion, protocol.CodeWrongQuestion, false},
		{scoring.ErrNotJoined, protocol.CodeNotJoined, false},
		{fmt.Errorf("%w: %w", scoring.ErrServerBusy, context.DeadlineExceeded), protocol.CodeServerBusy, true},
		{fmt.Errorf("%w: secret detail", scoring.ErrInternal), protocol.CodeInternal, false},
		{errors.New("never seen before"), protocol.CodeInternal, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.code)+"/"+tc.err.Error(), func(t *testing.T) {
			he := newHandlerEnv(t)
			c, sock := he.joined("u_1")
			he.answers.err = tc.err
			he.send(c, "a1", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q1", OptionID: "q1-b"})
			m := he.nth(sock, 2)
			wantError(t, m, "a1", tc.code)
			d := data(m)
			if d["retryable"] != tc.retryable || (d["retryAfterMs"] != nil) != tc.retryable {
				t.Errorf("error %v; retryable must be %v, with retryAfterMs only when retryable", d, tc.retryable)
			}
			if strings.Contains(d["message"].(string), "secret") {
				t.Errorf("message %q leaks the internal cause", d["message"])
			}
		})
	}
}

func TestJoin_QuestionSetLoadFailures(t *testing.T) {
	cases := []struct {
		err  error
		want protocol.ErrorCode
	}{
		{fmt.Errorf("load: %w", retry.ErrBudgetExhausted), protocol.CodeServerBusy},
		{quiz.ErrMalformedSet, protocol.CodeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			he := newHandlerEnv(t)
			he.cache.fail = tc.err
			c, sock := he.connAs("u_1", auth.RoleParticipant)
			he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
			wantError(t, he.nth(sock, 1), "r1", tc.want)
		})
	}
}

func TestErrors_CountedOncePerError(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "a1", protocol.TypeSubmitAnswer, protocol.SubmitAnswer{QuestionID: "q1", OptionID: "q1-b"})
	he.send(c, "r1", protocol.TypeJoin, joinMsg("bad\u0007name"))
	c.ReplyError("", protocol.CodeInvalidMessage, "bad frame", 0)
	he.nth(sock, 3)
	want := []protocol.ErrorCode{protocol.CodeNotJoined, protocol.CodeInvalidDisplayName, protocol.CodeInvalidMessage}
	if got := he.errs.list(); !slices.Equal(got, want) {
		t.Errorf("counted %v, want %v", got, want)
	}
}

func TestPing_ServerTimeFromTheRedisAlignedClock(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "p1", protocol.TypePing, protocol.Ping{ClientTime: 99})
	m := he.nth(sock, 1)
	if d := data(m); m["type"] != "pong" || m["id"] != "p1" || d["clientTime"] != 99.0 || d["serverTime"] != 123456.0 {
		t.Errorf("got %v, want pong with clientTime 99 and the clock's 123456", m)
	}
}

type fakePresence struct {
	mu    sync.Mutex
	calls map[quiz.Code][]quiz.ParticipantID
	n     int
}

func (p *fakePresence) RefreshPresence(_ context.Context, code quiz.Code, ids []quiz.ParticipantID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	got := slices.Clone(ids)
	slices.Sort(got)
	p.calls[code] = got
	return nil
}

func TestPresence_OneCallPerRoomForLocalParticipantsOnly(t *testing.T) {
	e := newHubEnv(t)
	e.enter("AAAAAA", "u_1")
	e.enter("AAAAAA", "u_2")
	e.enter("BBBBBB", "u_3")
	e.enter("CCCCCC", "") // only a watcher
	p := &fakePresence{calls: map[quiz.Code][]quiz.ParticipantID{}}
	e.hub.refreshPresence(context.Background(), p)

	want := map[quiz.Code][]quiz.ParticipantID{"AAAAAA": {"u_1", "u_2"}, "BBBBBB": {"u_3"}}
	if p.n != 2 || fmt.Sprint(p.calls) != fmt.Sprint(want) {
		t.Errorf("%d calls %v, want one per room with participants: %v", p.n, p.calls, want)
	}
}

func TestJoin_MidQuestionRejoinShowsTheOwnAnswer(t *testing.T) {
	he := newHandlerEnv(t)
	he.rooms.standings = map[quiz.ParticipantID]session.Standing{
		"u_1": {Found: true, Score: 228, Rank: 1, Answer: &session.Answer{OptionID: "q1-b", Correct: true, Points: 186}}}
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
	ya, _ := data(he.nth(sock, 1))["yourAnswer"].(map[string]any)
	if ya == nil || ya["questionId"] != "q1" || ya["optionId"] != "q1-b" || ya["points"] != 186.0 {
		t.Errorf("yourAnswer = %v; a rejoin must show the answer already accepted (FR-30)", ya)
	}
	if qs := he.rooms.questions; !slices.Equal(qs, []quiz.QuestionID{"q1"}) {
		t.Errorf("standings read for %v, want the current question q1", qs)
	}
}

func TestJoin_LobbyJoinMakesNoExtraRead(t *testing.T) {
	he := newHandlerEnv(t)
	lobby := quiz.RoomRecord{Code: hcode, QuestionSetID: testSet.ID,
		Room: quiz.Room{HostID: "host_1", Status: quiz.StatusLobby, QuestionIndex: -1, QuestionCount: 2, StateVersion: 1}}
	he.lookup.room, he.joiner.result.Room = lobby, lobby
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
	if d := data(he.nth(sock, 1)); d["question"] != nil || d["yourAnswer"] != nil {
		t.Errorf("lobby snapshot %v", d)
	}
	if n := len(he.rooms.standingCalls()); n != 0 {
		t.Errorf("%d standings reads for a lobby join; a join burst must cost one script per participant", n)
	}
}

// Leaving records it in Redis, takes the connection out of its room, and closes with 1000 (asyncapi receive_leave).
func TestLeave_RecordsLeavesTheRoomAndCloses(t *testing.T) {
	he := newHandlerEnv(t)
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
	he.nth(sock, 1)

	he.send(c, "", protocol.TypeLeave, protocol.Leave{})
	waitFor(t, "close frame", func() bool { return len(sock.closeCodes()) == 1 })
	if code := sock.closeCodes()[0]; code != websocket.CloseNormalClosure {
		t.Errorf("closed with %d, want 1000", code)
	}
	if got := he.rec.list(); !slices.Contains(got, "leave "+string(hcode)+" u_1") {
		t.Errorf("events %v, want the leave recorded for u_1 in %s", got, hcode)
	}
	if _, ok := c.Member(); ok {
		t.Error("connection still a member of the room after leaving")
	}
}

// A failed leave still closes the socket: the player is gone from this tab either way.
func TestLeave_RedisFailureStillCloses(t *testing.T) {
	he := newHandlerEnv(t)
	he.joiner.leaveErr = errors.New("redis down")
	c, sock := he.connAs("u_1", auth.RoleParticipant)
	he.send(c, "r1", protocol.TypeJoin, joinMsg("Rina"))
	he.nth(sock, 1)
	he.send(c, "", protocol.TypeLeave, protocol.Leave{})
	waitFor(t, "close frame", func() bool { return len(sock.closeCodes()) == 1 })
}

// Leaving before joining, or as the host, touches nothing in Redis.
func TestLeave_WithoutAPlayerOnlyCloses(t *testing.T) {
	he := newHandlerEnv(t)
	for _, role := range []auth.Role{auth.RoleParticipant, auth.RoleHost} {
		c, sock := he.connAs("u_x", role)
		he.send(c, "", protocol.TypeLeave, protocol.Leave{})
		waitFor(t, "close frame", func() bool { return len(sock.closeCodes()) == 1 })
	}
	for _, e := range he.rec.list() {
		if strings.HasPrefix(e, "leave") {
			t.Errorf("recorded %q for a connection with no player in a room", e)
		}
	}
}
