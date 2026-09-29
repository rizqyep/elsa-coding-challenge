package quiz_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

type transitionCase struct {
	Name   string    `json:"name"`
	Now    int64     `json:"now"`
	Room   quiz.Room `json:"room"`
	Expect struct {
		Outcome quiz.Outcome `json:"outcome"`
		Event   *quiz.Event  `json:"event"`
		Room    quiz.Room    `json:"room"`
	} `json:"expect"`
}

func TestNext_SharedVectors(t *testing.T) {
	b, err := os.ReadFile("testdata/transition_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Description string           `json:"description"`
		Cases       []transitionCase `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // a renamed field in the vectors must not be silently ignored
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			room, outcome, event := quiz.Next(c.Room, c.Now)
			if outcome != c.Expect.Outcome {
				t.Errorf("outcome = %q, want %q", outcome, c.Expect.Outcome)
			}
			if !reflect.DeepEqual(room, c.Expect.Room) {
				t.Errorf("room =\n  %+v\nwant\n  %+v", room, c.Expect.Room)
			}
			switch {
			case c.Expect.Event == nil && event.Type != quiz.NoEvent:
				t.Errorf("event = %+v, want none", event)
			case c.Expect.Event != nil && event != *c.Expect.Event:
				t.Errorf("event = %+v, want %+v", event, *c.Expect.Event)
			}
		})
	}
}

func lobby() quiz.Room {
	return quiz.Room{
		HostID: "host_1", Status: quiz.StatusLobby, QuestionIndex: -1, QuestionCount: 3,
		WindowMs: 15_000, RevealMs: 5_000, NextTransitionAt: 1_800_000, LobbyExpiresAt: 1_800_000, StateVersion: 1,
	}
}

func TestStart(t *testing.T) {
	const now = 1_000
	already := lobby()
	already.StartRequested, already.NextTransitionAt = true, 500
	running := func(s quiz.Status) quiz.Room {
		r := lobby()
		r.Status, r.StartRequested, r.QuestionIndex = s, true, 0
		return r
	}
	expired := lobby()
	expired.Status = quiz.StatusExpired

	cases := []struct {
		name         string
		room         quiz.Room
		caller       quiz.ParticipantID
		participants int
		wantErr      error
		want         quiz.Room
	}{
		{"host starts a lobby with participants", lobby(), "host_1", 1, nil, func() quiz.Room {
			r := lobby()
			r.StartRequested, r.NextTransitionAt = true, now // due immediately; the version changes only when a worker applies it
			return r
		}()},
		{"not the host", lobby(), "u_1", 1, quiz.ErrNotHost, lobby()},
		{"no participants yet (FR-3)", lobby(), "host_1", 0, quiz.ErrNoParticipants, lobby()},
		{"host retries after question 1 opened (idempotent)", running(quiz.StatusQuestionOpen), "host_1", 5, nil, running(quiz.StatusQuestionOpen)},
		{"host retries during a reveal (idempotent)", running(quiz.StatusQuestionClosed), "host_1", 5, nil, running(quiz.StatusQuestionClosed)},
		{"not the host while running", running(quiz.StatusQuestionOpen), "u_1", 5, quiz.ErrNotHost, running(quiz.StatusQuestionOpen)},
		{"finished quiz", running(quiz.StatusFinished), "host_1", 5, quiz.ErrNotInLobby, running(quiz.StatusFinished)},
		{"lobby expired without a start", expired, "host_1", 5, quiz.ErrNotInLobby, expired},
		{"start requested twice is idempotent", already, "host_1", 1, nil, already},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := quiz.Start(tc.room, tc.caller, tc.participants, now)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("room =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

func TestEarlyClose(t *testing.T) {
	open := lobby()
	open.Status, open.QuestionIndex = quiz.StatusQuestionOpen, 0
	open.OpenedAt, open.Deadline, open.CloseAt, open.NextTransitionAt = 0, 15_000, 15_000, 15_000
	closed := open
	closed.Status = quiz.StatusQuestionClosed

	cases := []struct {
		name             string
		room             quiz.Room
		answered, online int
		now              int64
		closes           bool
	}{
		{"everyone online has answered (FR-5)", open, 3, 3, 4_000, true},
		{"someone went offline after answering", open, 4, 3, 4_000, true},
		{"not everyone has answered", open, 2, 3, 4_000, false},
		{"nobody online", open, 0, 0, 4_000, false},
		{"already at close time", open, 3, 3, 15_000, false},
		{"question not open", closed, 3, 3, 4_000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, closes := quiz.EarlyClose(tc.room, tc.answered, tc.online, tc.now)
			if closes != tc.closes {
				t.Fatalf("closes = %v, want %v", closes, tc.closes)
			}
			want := tc.room
			if tc.closes { // a new version, so gateways and clients don't drop the moved close time (FR-28)
				want.CloseAt, want.NextTransitionAt, want.StateVersion = tc.now, tc.now, tc.room.StateVersion+1
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("room =\n  %+v\nwant\n  %+v", got, want)
			}
			if got.Deadline != tc.room.Deadline {
				t.Error("early close must never move the original deadline (it sets the speed bonus)")
			}
		})
	}
}
