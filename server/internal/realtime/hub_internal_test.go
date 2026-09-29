package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var testSet = &quiz.QuestionSet{
	ID: "demo", Title: "Demo",
	Questions: []quiz.Question{
		{ID: "q1", Prompt: "Synonym of rapid?", CorrectOptionID: "q1-b",
			Options: []quiz.Option{{ID: "q1-a", Text: "slow"}, {ID: "q1-b", Text: "quick"}}},
		{ID: "q2", Prompt: "Antonym of ancient?", CorrectOptionID: "q2-a",
			Options: []quiz.Option{{ID: "q2-a", Text: "modern"}, {ID: "q2-b", Text: "old"}}},
	},
}

type fakeCache struct {
	acquired, released atomic.Int64
	fail               error
}

func (c *fakeCache) Acquire(context.Context, quiz.QuestionSetID) (*quiz.QuestionSet, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	c.acquired.Add(1)
	return testSet, nil
}
func (c *fakeCache) Release(quiz.QuestionSetID) { c.released.Add(1) }
func (c *fakeCache) Get(id quiz.QuestionSetID) (*quiz.QuestionSet, bool) {
	return testSet, id == testSet.ID
}

type fakeSubs struct {
	mu      sync.Mutex
	ensured map[quiz.Code]int
	dropped map[quiz.Code]int
	fail    error
	rec     *recorder // optional: records "subscribe"
}

func newFakeSubs() *fakeSubs {
	return &fakeSubs{ensured: map[quiz.Code]int{}, dropped: map[quiz.Code]int{}}
}
func (s *fakeSubs) Ensure(_ context.Context, code quiz.Code) error {
	if s.rec != nil {
		s.rec.add("subscribe")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensured[code]++
	return s.fail
}
func (s *fakeSubs) Drop(code quiz.Code) { s.mu.Lock(); s.dropped[code]++; s.mu.Unlock() }
func (s *fakeSubs) counts(code quiz.Code) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensured[code], s.dropped[code]
}

type fakeRooms struct {
	mu        sync.Mutex
	view      session.RoomView
	standings map[quiz.ParticipantID]session.Standing
	viewDelay time.Duration
	views     atomic.Int64
	calls     [][]quiz.ParticipantID
	questions []quiz.QuestionID
}

// View honours cancellation as the real Redis call does.
func (r *fakeRooms) View(ctx context.Context, _ quiz.Code) (session.RoomView, error) {
	r.views.Add(1)
	select {
	case <-time.After(r.viewDelay):
		return r.view, nil
	case <-ctx.Done():
		return session.RoomView{}, ctx.Err()
	}
}

func (r *fakeRooms) Standings(_ context.Context, _ quiz.Code, qid quiz.QuestionID, ids []quiz.ParticipantID) (session.Standings, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]quiz.ParticipantID(nil), ids...))
	r.questions = append(r.questions, qid)
	r.mu.Unlock()
	out := session.Standings{ParticipantCount: len(r.standings), ByID: map[quiz.ParticipantID]session.Standing{}}
	for _, id := range ids {
		out.ByID[id] = r.standings[id]
	}
	return out, nil
}

func (r *fakeRooms) standingCalls() [][]quiz.ParticipantID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]quiz.ParticipantID(nil), r.calls...)
}

// hubHandler wires connections to the hub the way the gateway's message handler does.
type hubHandler struct{ h *Hub }

func (hubHandler) Handle(context.Context, *Conn, protocol.ClientMessage) {}
func (a hubHandler) Snapshot(ctx context.Context, c *Conn) ([]byte, error) {
	return a.h.Snapshot(ctx, c)
}
func (a hubHandler) Closed(c *Conn) { a.h.Leave(c) }

type hubEnv struct {
	t     *testing.T
	hub   *Hub
	cache *fakeCache
	subs  *fakeSubs
	rooms *fakeRooms
	s     *shared
}

func newHubEnv(t *testing.T) *hubEnv {
	t.Helper()
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	e := &hubEnv{t: t, cache: &fakeCache{}, subs: newFakeSubs(), rooms: &fakeRooms{}}
	e.hub = NewHub(e.cache, e.rooms, HubOptions{})
	e.hub.Attach(e.subs)
	e.s = newShared(internalOptions(clk, 64), hubHandler{e.hub}, nil)
	return e
}

// conn starts a connection over a fake socket that isn't in any room yet.
func (e *hubEnv) conn() (*Conn, *fakeSocket) {
	e.t.Helper()
	sock := newFakeSocket()
	return start(e.t, e.s, sock), sock
}

func (e *hubEnv) enter(code quiz.Code, pid string) (*Conn, *fakeSocket) {
	e.t.Helper()
	c, sock := e.conn()
	m := Member{Code: code, SetID: testSet.ID, ParticipantID: quiz.ParticipantID(pid), DisplayName: "name-" + pid}
	if err := e.hub.Enter(context.Background(), c, m); err != nil {
		e.t.Fatal(err)
	}
	return c, sock
}

// decoded returns the socket's personal frames, decoded.
func decoded(t *testing.T, sock *fakeSocket) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, f := range sock.frames() {
		if strings.HasPrefix(f, "pm:") {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(f), &m); err != nil {
			t.Fatalf("frame %q: %v", f, err)
		}
		if err := protocol.ValidateServer([]byte(f)); err != nil {
			t.Errorf("frame breaks the contract: %v\n%s", err, f)
		}
		out = append(out, m)
	}
	return out
}

func TestHub_SubscribesOnEnterAndDropsOnLastLeave(t *testing.T) {
	e := newHubEnv(t)
	a, _ := e.enter("AAAAAA", "u_1")
	b, _ := e.enter("AAAAAA", "u_2")
	e.enter("BBBBBB", "u_3")

	if ens, _ := e.subs.counts("AAAAAA"); ens == 0 {
		t.Fatal("entering a room didn't subscribe to it")
	}
	e.hub.Leave(a)
	if _, drop := e.subs.counts("AAAAAA"); drop != 0 {
		t.Fatalf("dropped the subscription while u_2 is still in the room")
	}
	e.hub.Leave(b)
	e.hub.Leave(b) // leaving twice is harmless
	if _, drop := e.subs.counts("AAAAAA"); drop != 1 {
		t.Errorf("dropped %d times after the last leave, want 1", drop)
	}
	if _, drop := e.subs.counts("BBBBBB"); drop != 0 {
		t.Error("another room's subscription was dropped")
	}
	if e.cache.acquired.Load() != 3 || e.cache.released.Load() != 2 {
		t.Errorf("question set acquired %d, released %d; want one reference per connection (3, 2)",
			e.cache.acquired.Load(), e.cache.released.Load())
	}
	if e.hub.Has("AAAAAA") || !e.hub.Has("BBBBBB") {
		t.Error("Has doesn't match the local rooms")
	}
}

func TestHub_EnterUndoesItselfWhenSubscribingFails(t *testing.T) {
	e := newHubEnv(t)
	e.subs.fail = errors.New("redis down")
	c, _ := e.conn()
	err := e.hub.Enter(context.Background(), c, Member{Code: "AAAAAA", SetID: testSet.ID, ParticipantID: "u_1"})
	if err == nil {
		t.Fatal("Enter succeeded without a subscription")
	}
	if e.hub.Has("AAAAAA") || e.cache.released.Load() != e.cache.acquired.Load() {
		t.Errorf("left behind: room=%v acquired=%d released=%d", e.hub.Has("AAAAAA"), e.cache.acquired.Load(), e.cache.released.Load())
	}
	if _, drop := e.subs.counts("AAAAAA"); drop != 1 {
		t.Errorf("dropped %d times, want 1 (the room is empty again)", drop)
	}
}

func TestHub_EnterFailsWhenTheQuestionSetCantLoad(t *testing.T) {
	e := newHubEnv(t)
	e.cache.fail = errors.New("postgres down")
	c, _ := e.conn()
	if err := e.hub.Enter(context.Background(), c, Member{Code: "AAAAAA", SetID: testSet.ID, ParticipantID: "u_1"}); err == nil {
		t.Fatal("Enter succeeded without the question set")
	}
	if e.hub.Has("AAAAAA") {
		t.Error("room registered without its question set")
	}
}

func TestHub_SameParticipantReplacesTheOlderConnection(t *testing.T) {
	e := newHubEnv(t)
	old, oldSock := e.enter("AAAAAA", "u_1")
	e.enter("AAAAAA", "u_1")
	waitFor(t, "the old connection to close", func() bool { return old.Context().Err() != nil })
	waitFor(t, "the close frame", func() bool { return len(oldSock.closeCodes()) == 1 })
	if oldSock.closeCodes()[0] != CloseReplaced {
		t.Errorf("close code %v, want %d", oldSock.closeCodes(), CloseReplaced)
	}
	if got := e.hub.reg.participants("AAAAAA"); len(got) != 1 {
		t.Errorf("%d participant entries, want 1", len(got))
	}
}

func TestRegistry_ConcurrentEnterAndLeave(t *testing.T) {
	e := newHubEnv(t)
	codes := []quiz.Code{"AAAAAA", "BBBBBB", "CCCCCC", "DDDDDD"}
	var wg sync.WaitGroup
	for g := range 32 {
		wg.Go(func() {
			for i := range 50 {
				code := codes[(g+i)%len(codes)]
				c, _ := e.conn()
				m := Member{Code: code, SetID: testSet.ID, ParticipantID: quiz.ParticipantID(fmt.Sprintf("u_%d_%d", g, i))}
				if err := e.hub.Enter(context.Background(), c, m); err != nil {
					t.Error(err)
					return
				}
				e.hub.Leave(c)
			}
		})
	}
	wg.Wait()
	for _, code := range codes {
		if e.hub.Has(code) {
			t.Errorf("%s still registered after every connection left", code)
		}
	}
	if e.cache.acquired.Load() != 1600 || e.cache.released.Load() != 1600 {
		t.Errorf("acquired %d, released %d; want 1600 each", e.cache.acquired.Load(), e.cache.released.Load())
	}
}

func TestHub_BroadcastsOneSharedFrameToTheRoomOnly(t *testing.T) {
	e := newHubEnv(t)
	_, s1 := e.enter("AAAAAA", "u_1")
	_, s2 := e.enter("AAAAAA", "u_2")
	c3, s3 := e.conn()
	if err := e.hub.Enter(context.Background(), c3, Member{Code: "AAAAAA", SetID: testSet.ID}); err != nil { // the host, watching
		t.Fatal(err)
	}
	_, other := e.enter("BBBBBB", "u_9")

	e.hub.OnMessage("AAAAAA", `{"t":"lb","v":3,"n":2,"top":[["u_1","Rina",200],["u_2","Budi",100]]}`)
	waitFor(t, "the broadcast", func() bool { return len(s1.frames()) == 1 && len(s2.frames()) == 1 && len(s3.frames()) == 1 })
	f := s1.frames()[0]
	if !strings.HasPrefix(f, "pm:") || s2.frames()[0] != f || s3.frames()[0] != f {
		t.Errorf("frames %q %q %q: want the same prepared message for every connection", f, s2.frames()[0], s3.frames()[0])
	}
	time.Sleep(20 * time.Millisecond)
	if len(other.frames()) != 0 {
		t.Error("another room received the broadcast")
	}
}

func TestHub_MapsEveryEventType(t *testing.T) {
	e := newHubEnv(t)
	entries := []protocol.LeaderboardEntry{
		{Rank: 1, ParticipantID: "u_1", DisplayName: "Rina", Score: 300},
		{Rank: 1, ParticipantID: "u_2", DisplayName: "Budi", Score: 300},
		{Rank: 3, ParticipantID: "u_3", DisplayName: "Sari", Score: 100},
	}
	q1 := protocol.PublicQuestion{QuestionID: "q1", Index: 0, Count: 2, Prompt: "Synonym of rapid?",
		Options: []protocol.PublicOption{{ID: "q1-a", Text: "slow"}, {ID: "q1-b", Text: "quick"}}, OpenedAt: 1000, Deadline: 16000, CloseAt: 16000}
	early := q1
	early.CloseAt = 9000
	cases := []struct {
		name  string
		event string
		typ   protocol.Type
		want  any
	}{
		{"question opened", `{"t":"state","v":2,"s":"question_open","i":0,"n":2,"q":"q1","o":1000,"d":16000,"c":16000,"x":16000}`,
			protocol.TypeQuestion, protocol.QuestionMsg{Question: q1, StateVersion: 2}},
		{"early close moves closeAt only", `{"t":"state","v":3,"s":"question_open","i":0,"n":2,"q":"q1","o":1000,"d":16000,"c":9000,"x":9000}`,
			protocol.TypeQuestion, protocol.QuestionMsg{Question: early, StateVersion: 3}},
		{"question closed", `{"t":"state","v":4,"s":"question_closed","i":0,"n":2,"q":"q1","o":1000,"d":16000,"c":9000,"x":12000}`,
			protocol.TypeQuestionClosed, protocol.QuestionClosed{QuestionID: "q1", CorrectOptionID: "q1-b", NextTransitionAt: 12000, StateVersion: 4}},
		{"expired", `{"t":"state","v":2,"s":"expired","i":-1,"n":2,"q":"","o":0,"d":0,"c":0,"x":0}`,
			protocol.TypeQuizState, protocol.QuizState{Status: quiz.StatusExpired, QuestionIndex: -1, QuestionCount: 2, StateVersion: 2}},
		{"finished", `{"t":"finished","v":9,"n":3,"top":[["u_1","Rina",300],["u_2","Budi",300],["u_3","Sari",100]]}`,
			protocol.TypeQuizFinished, protocol.QuizFinished{FinalTop: entries, ParticipantCount: 3, StateVersion: 9}},
		{"leaderboard", `{"t":"lb","v":5,"n":3,"top":[["u_1","Rina",300],["u_2","Budi",300],["u_3","Sari",100]]}`,
			protocol.TypeLeaderboard, protocol.Leaderboard{Version: 5, ParticipantCount: 3, Top: entries}},
		{"leaderboard, cjson's empty table", `{"t":"lb","v":1,"n":0,"top":{}}`,
			protocol.TypeLeaderboard, protocol.Leaderboard{Version: 1, ParticipantCount: 0, Top: []protocol.LeaderboardEntry{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := decodeEvent([]byte(tc.event))
			if err != nil {
				t.Fatal(err)
			}
			typ, got, ok := e.hub.clientMessage(testSet.ID, ev)
			if !ok || typ != tc.typ || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v %v %+v\nwant %v %+v", ok, typ, got, tc.typ, tc.want)
			}
			frame, err := protocol.Encode(typ, "", got)
			if err != nil {
				t.Fatal(err)
			}
			if err := protocol.ValidateServer(frame); err != nil {
				t.Errorf("breaks the contract: %v\n%s", err, frame)
			}
		})
	}

	for name, event := range map[string]string{
		"unknown event type":        `{"t":"confetti","v":1}`,
		"question not in the set":   `{"t":"state","v":2,"s":"question_open","i":0,"n":2,"q":"nope","o":1,"d":2,"c":2,"x":2}`,
		"closed question not found": `{"t":"state","v":2,"s":"question_closed","i":0,"n":2,"q":"nope","o":1,"d":2,"c":2,"x":3}`,
	} {
		ev, err := decodeEvent([]byte(event))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, _, ok := e.hub.clientMessage(testSet.ID, ev); ok {
			t.Errorf("%s: mapped to a client message", name)
		}
	}
	if _, err := decodeEvent([]byte(`not json`)); err == nil {
		t.Error("garbage decoded")
	}
}

func TestHub_DropsStaleEvents(t *testing.T) {
	e := newHubEnv(t)
	_, sock := e.enter("AAAAAA", "u_1")
	send := func(ev string) { e.hub.OnMessage("AAAAAA", ev) }
	send(`{"t":"state","v":5,"s":"question_open","i":0,"n":2,"q":"q1","o":1,"d":2,"c":2,"x":2}`)
	send(`{"t":"state","v":4,"s":"question_open","i":0,"n":2,"q":"q1","o":1,"d":2,"c":2,"x":2}`) // older
	send(`{"t":"state","v":5,"s":"question_open","i":0,"n":2,"q":"q1","o":1,"d":2,"c":2,"x":2}`) // repeat
	send(`{"t":"lb","v":3,"n":1,"top":[["u_1","Rina",0]]}`)                                      // own counter
	send(`{"t":"lb","v":2,"n":1,"top":[["u_1","Rina",0]]}`)                                      // older
	send(`{"t":"finished","v":5,"n":1,"top":[["u_1","Rina",0]]}`)                                // shares the state counter
	send(`{"t":"finished","v":6,"n":1,"top":[["u_1","Rina",0]]}`)
	waitFor(t, "the newer events", func() bool { return len(sock.frames()) == 3 })
	time.Sleep(20 * time.Millisecond)
	if n := len(sock.frames()); n != 3 {
		t.Errorf("%d frames, want 3 (question v5, leaderboard v3, finished v6)", n)
	}
}

func TestHub_RanksAfterCloseComeFromOneRead(t *testing.T) {
	e := newHubEnv(t)
	e.rooms.standings = map[quiz.ParticipantID]session.Standing{
		"u_1": {Found: true, Score: 300, Rank: 1}, "u_2": {Found: true, Score: 300, Rank: 1}, "u_3": {Found: true, Score: 100, Rank: 3},
	}
	var socks []*fakeSocket
	for _, id := range []string{"u_1", "u_2", "u_3"} {
		_, s := e.enter("AAAAAA", id)
		socks = append(socks, s)
	}
	host, hostSock := e.conn()
	if err := e.hub.Enter(context.Background(), host, Member{Code: "AAAAAA", SetID: testSet.ID}); err != nil {
		t.Fatal(err)
	}

	e.hub.OnMessage("AAAAAA", `{"t":"state","v":4,"s":"question_closed","i":0,"n":2,"q":"q1","o":1000,"d":16000,"c":9000,"x":12000}`)
	for i, s := range socks {
		waitFor(t, "the reveal and the rank", func() bool { return len(s.frames()) == 2 })
		ranks := decoded(t, s)
		want := e.rooms.standings[quiz.ParticipantID(fmt.Sprintf("u_%d", i+1))]
		d := ranks[0]["data"].(map[string]any)
		if ranks[0]["type"] != "rank" || d["questionId"] != "q1" || d["score"] != float64(want.Score) ||
			d["rank"] != float64(want.Rank) || d["participantCount"] != 3.0 {
			t.Errorf("u_%d rank %v", i+1, ranks[0])
		}
	}
	if calls := e.rooms.standingCalls(); len(calls) != 1 || len(calls[0]) != 3 {
		t.Errorf("standings read %d times (%v), want once for all 3 participants", len(calls), calls)
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(hostSock.frames()); n != 1 {
		t.Errorf("the host got %d frames, want only the reveal", n)
	}
}

func TestHub_KickClosesOnlyOtherConnections(t *testing.T) {
	e := newHubEnv(t)
	keep, keepSock := e.enter("AAAAAA", "u_1")
	e.hub.OnMessage("AAAAAA", fmt.Sprintf(`{"t":"kick","p":"u_1","k":%q}`, keep.ID()))
	time.Sleep(20 * time.Millisecond)
	if keep.Context().Err() != nil || len(keepSock.closeCodes()) != 0 {
		t.Fatal("the connection named to keep was closed")
	}
	e.hub.OnMessage("AAAAAA", `{"t":"kick","p":"u_1","k":"a-connection-on-another-gateway"}`)
	waitFor(t, "the kick", func() bool { return len(keepSock.closeCodes()) == 1 })
	if keepSock.closeCodes()[0] != CloseReplaced {
		t.Errorf("close code %v, want %d", keepSock.closeCodes(), CloseReplaced)
	}
}

func roomView(status quiz.Status) session.RoomView {
	v := session.RoomView{
		Room: quiz.RoomRecord{Code: "AAAAAA", QuestionSetID: testSet.ID, QuestionID: "q1", LeaderboardVersion: 7,
			Room: quiz.Room{Status: status, QuestionIndex: 0, QuestionCount: 2, OpenedAt: 1000, Deadline: 16000, CloseAt: 16000, StateVersion: 3}},
		ParticipantCount: 2,
		Top:              []leaderboard.Entry{{Rank: 1, ParticipantID: "u_1", DisplayName: "Rina", Score: 186}, {Rank: 2, ParticipantID: "u_2", DisplayName: "Budi", Score: 0}},
		ServerTime:       5000,
	}
	return v
}

func TestHub_ResubscribePushesSnapshotsFromOneViewAndOneStandingsRead(t *testing.T) {
	e := newHubEnv(t)
	e.rooms.view = roomView(quiz.StatusQuestionOpen)
	e.rooms.standings = map[quiz.ParticipantID]session.Standing{}
	var socks []*fakeSocket
	for i := range 40 {
		id := quiz.ParticipantID(fmt.Sprintf("u_%d", i))
		e.rooms.standings[id] = session.Standing{Found: true, Score: i, Rank: 40 - i}
		_, s := e.enter("AAAAAA", string(id))
		socks = append(socks, s)
	}
	e.rooms.standings["u_0"] = session.Standing{Found: true, Score: 186, Rank: 1,
		Answer: &session.Answer{OptionID: "q1-b", Correct: true, Points: 186}}
	host, hostSock := e.conn()
	if err := e.hub.Enter(context.Background(), host, Member{Code: "AAAAAA", SetID: testSet.ID}); err != nil {
		t.Fatal(err)
	}

	e.hub.OnResubscribed("AAAAAA")
	for _, s := range append(socks, hostSock) {
		waitFor(t, "every connection's snapshot", func() bool { return len(s.frames()) == 1 })
	}
	if n := e.rooms.views.Load(); n != 1 {
		t.Errorf("room view read %d times for 41 connections, want 1", n)
	}
	calls := e.rooms.standingCalls()
	if len(calls) != 1 || len(calls[0]) != 40 || e.rooms.questions[0] != "q1" {
		t.Errorf("standings reads %d (%d ids, question %v), want 1 read for 40 participants of q1", len(calls), len(calls[0]), e.rooms.questions)
	}

	snap := decoded(t, socks[0])[0]["data"].(map[string]any)
	you := snap["you"].(map[string]any)
	ans := snap["yourAnswer"].(map[string]any)
	q := snap["question"].(map[string]any)
	if snap["role"] != "participant" || you["participantId"] != "u_0" || you["displayName"] != "name-u_0" || you["score"] != 186.0 ||
		you["rank"] != 1.0 || ans["optionId"] != "q1-b" || ans["correct"] != true || q["prompt"] != "Synonym of rapid?" ||
		snap["correctOptionId"] != nil || snap["serverTime"] != 5000.0 {
		t.Errorf("participant snapshot %v", snap)
	}
	hs := decoded(t, hostSock)[0]["data"].(map[string]any)
	if hs["role"] != "host" || hs["you"] != nil || hs["yourAnswer"] != nil {
		t.Errorf("host snapshot %v", hs)
	}
}

func TestHub_SnapshotRevealsTheAnswerOnlyAfterClose(t *testing.T) {
	e := newHubEnv(t)
	e.rooms.view = roomView(quiz.StatusQuestionClosed)
	e.rooms.standings = map[quiz.ParticipantID]session.Standing{"u_1": {Found: true, Score: 0, Rank: 2}}
	c, _ := e.enter("AAAAAA", "u_1")
	frame, err := e.hub.Snapshot(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateServer(frame); err != nil {
		t.Fatalf("%v\n%s", err, frame)
	}
	if !strings.Contains(string(frame), `"correctOptionId":"q1-b"`) {
		t.Errorf("closed-question snapshot doesn't reveal the answer: %s", frame)
	}
}

func TestHub_ResyncSnapshotsShareOneRoomView(t *testing.T) {
	e := newHubEnv(t)
	e.rooms.view = roomView(quiz.StatusQuestionOpen)
	e.rooms.viewDelay = 50 * time.Millisecond
	e.rooms.standings = map[quiz.ParticipantID]session.Standing{}
	var conns []*Conn
	for i := range 20 {
		c, _ := e.enter("AAAAAA", fmt.Sprintf("u_%d", i))
		conns = append(conns, c)
	}
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Go(func() {
			if _, err := e.hub.Snapshot(context.Background(), c); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := e.rooms.views.Load(); n != 1 {
		t.Errorf("room view read %d times for 20 concurrent resyncs, want 1", n)
	}
}

func TestHub_ResyncViewSurvivesTheFirstCallerGivingUp(t *testing.T) {
	e := newHubEnv(t)
	e.rooms.view = roomView(quiz.StatusQuestionOpen)
	e.rooms.viewDelay = 50 * time.Millisecond
	e.rooms.standings = map[quiz.ParticipantID]session.Standing{}
	first, _ := e.enter("AAAAAA", "u_1")
	second, _ := e.enter("AAAAAA", "u_2")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = e.hub.Snapshot(ctx, first) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if _, err := e.hub.Snapshot(context.Background(), second); err != nil {
		t.Errorf("a cancelled resync failed the one sharing its view: %v", err)
	}
}

func TestHub_NothingToResyncOutsideARoom(t *testing.T) {
	e := newHubEnv(t)
	c, _ := e.conn()
	if frame, err := e.hub.Snapshot(context.Background(), c); frame != nil || err != nil {
		t.Errorf("got %s, %v", frame, err)
	}
}
