package quiz

import "errors"

// Status is a room's lifecycle state (TRD §3.3).
type Status string

// Room statuses.
const (
	StatusLobby          Status = "lobby"
	StatusQuestionOpen   Status = "question_open"
	StatusQuestionClosed Status = "question_closed"
	StatusFinished       Status = "finished"
	StatusExpired        Status = "expired"
)

// Room is the state the state machine works on (TRD §3.3). Times are epoch milliseconds.
type Room struct {
	HostID           ParticipantID `json:"hostId"`
	Status           Status        `json:"status"`
	QuestionIndex    int           `json:"questionIndex"` // -1 in the lobby
	QuestionCount    int           `json:"questionCount"`
	WindowMs         int64         `json:"windowMs"`
	RevealMs         int64         `json:"revealMs"`
	OpenedAt         int64         `json:"openedAt"`
	Deadline         int64         `json:"deadline"` // original; the speed bonus uses it
	CloseAt          int64         `json:"closeAt"`  // answers accepted until; ≤ Deadline
	NextTransitionAt int64         `json:"nextTransitionAt"`
	StartRequested   bool          `json:"startRequested"`
	LobbyExpiresAt   int64         `json:"lobbyExpiresAt"`
	StateVersion     int64         `json:"stateVersion"`
}

// Outcome says what Next did.
type Outcome string

// Next outcomes.
const (
	Applied  Outcome = "applied"
	NotDue   Outcome = "not_due"
	Terminal Outcome = "terminal"
)

// EventType names what a transition announces to the room (TRD §3.7).
type EventType string

// Event types.
const (
	NoEvent        EventType = ""
	QuestionOpened EventType = "question_opened"
	QuestionClosed EventType = "question_closed"
	QuizFinished   EventType = "quiz_finished"
	QuizExpired    EventType = "quiz_expired"
)

// Event is announced after an applied transition.
type Event struct {
	Type          EventType `json:"type"`
	QuestionIndex int       `json:"questionIndex"`
}

// Next applies the transition due at now, if any. Mirrored by transition.lua (testdata/transition_cases.json).
func Next(r Room, now int64) (Room, Outcome, Event) {
	if r.Status == StatusFinished || r.Status == StatusExpired {
		return r, Terminal, Event{}
	}
	if now < r.NextTransitionAt {
		return r, NotDue, Event{}
	}
	var ev Event
	switch r.Status {
	case StatusLobby:
		switch {
		case r.StartRequested:
			ev = r.openQuestion(0, now)
		case now >= r.LobbyExpiresAt:
			r.Status, r.NextTransitionAt = StatusExpired, 0
			ev = Event{Type: QuizExpired, QuestionIndex: r.QuestionIndex}
		default:
			return r, NotDue, Event{}
		}
	case StatusQuestionOpen:
		if now < r.CloseAt {
			return r, NotDue, Event{}
		}
		r.Status, r.NextTransitionAt = StatusQuestionClosed, now+r.RevealMs
		ev = Event{Type: QuestionClosed, QuestionIndex: r.QuestionIndex}
	case StatusQuestionClosed:
		if r.QuestionIndex < r.QuestionCount-1 {
			ev = r.openQuestion(r.QuestionIndex+1, now)
		} else {
			r.Status, r.NextTransitionAt = StatusFinished, 0
			ev = Event{Type: QuizFinished, QuestionIndex: r.QuestionIndex}
		}
	}
	r.StateVersion++
	return r, Applied, ev
}

func (r *Room) openQuestion(i int, now int64) Event {
	r.Status, r.QuestionIndex, r.OpenedAt = StatusQuestionOpen, i, now
	r.Deadline = now + r.WindowMs
	r.CloseAt, r.NextTransitionAt = r.Deadline, r.Deadline
	return Event{Type: QuestionOpened, QuestionIndex: i}
}

// Start errors (FR-3).
var (
	ErrNotHost        = errors.New("only the quiz's host can start it")
	ErrNotInLobby     = errors.New("the quiz is not in the lobby")
	ErrNoParticipants = errors.New("the quiz has no participants yet")
)

// Start records the host's start request (FR-3). Repeating it while the quiz runs is a no-op (openapi.yaml startQuiz).
func Start(r Room, caller ParticipantID, participants int, now int64) (Room, error) {
	running := r.Status == StatusLobby || r.Status == StatusQuestionOpen || r.Status == StatusQuestionClosed
	switch {
	case caller != r.HostID:
		return r, ErrNotHost
	case r.StartRequested && running:
		return r, nil
	case r.Status != StatusLobby:
		return r, ErrNotInLobby
	case participants < 1:
		return r, ErrNoParticipants
	}
	r.StartRequested, r.NextTransitionAt = true, now
	return r, nil
}

// EarlyClose closes the open question once every online participant has answered (FR-5).
func EarlyClose(r Room, answered, online int, now int64) (Room, bool) {
	if r.Status != StatusQuestionOpen || online < 1 || answered < online || now >= r.CloseAt {
		return r, false
	}
	r.CloseAt, r.NextTransitionAt = now, now
	r.StateVersion++ // otherwise the moved close time looks stale to gateways and clients (FR-28)
	return r, true
}
