package protocol

import (
	"encoding/json"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// Version is the protocol version carried in every envelope.
const Version = 1

// Type is a message type (docs/api/asyncapi.yaml).
type Type string

// Client → server message types.
const (
	TypeJoin         Type = "join"
	TypeWatch        Type = "watch"
	TypeSubmitAnswer Type = "submit_answer"
	TypePing         Type = "ping"
)

// Server → client message types.
const (
	TypeSnapshot       Type = "snapshot"
	TypeQuestion       Type = "question"
	TypeQuestionClosed Type = "question_closed"
	TypeAnswerResult   Type = "answer_result"
	TypeRank           Type = "rank"
	TypeLeaderboard    Type = "leaderboard"
	TypeQuizFinished   Type = "quiz_finished"
	TypeQuizState      Type = "quiz_state"
	TypeError          Type = "error"
	TypePong           Type = "pong"
)

// ClientTypes lists the message types a client may send.
func ClientTypes() []Type { return []Type{TypeJoin, TypeWatch, TypeSubmitAnswer, TypePing} }

// ServerTypes lists the message types the server sends.
func ServerTypes() []Type {
	return []Type{TypeSnapshot, TypeQuestion, TypeQuestionClosed, TypeAnswerResult, TypeRank,
		TypeLeaderboard, TypeQuizFinished, TypeQuizState, TypeError, TypePong}
}

// Client payloads.
type (
	// Join is sent by a participant to join a quiz.
	Join struct {
		QuizCode    string `json:"quizCode"`
		DisplayName string `json:"displayName"`
	}
	// Watch is sent by the host to watch their quiz.
	Watch struct {
		QuizCode string `json:"quizCode"`
	}
	// SubmitAnswer answers the open question.
	SubmitAnswer struct {
		QuestionID string `json:"questionId"`
		OptionID   string `json:"optionId"`
	}
	// Ping lets the client estimate its clock offset.
	Ping struct {
		ClientTime int64 `json:"clientTime"`
	}
)

// Shared payload parts.
type (
	// PublicQuestion is a question as clients see it; it has no answer key (FR-21).
	PublicQuestion struct {
		QuestionID string         `json:"questionId"`
		Index      int            `json:"index"`
		Count      int            `json:"count"`
		Prompt     string         `json:"prompt"`
		Options    []PublicOption `json:"options"`
		OpenedAt   int64          `json:"openedAt"`
		Deadline   int64          `json:"deadline"`
		CloseAt    int64          `json:"closeAt"`
	}
	// PublicOption is one answer choice.
	PublicOption struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	// LeaderboardEntry is one ranked participant.
	LeaderboardEntry struct {
		Rank          int    `json:"rank"`
		ParticipantID string `json:"participantId"`
		DisplayName   string `json:"displayName"`
		Score         int    `json:"score"`
	}
)

// QuestionFrom builds the wire form of a question from its public (key-free) form.
func QuestionFrom(q quiz.PublicQuestion, index, count int, openedAt, deadline, closeAt int64) PublicQuestion {
	opts := make([]PublicOption, len(q.Options))
	for i, o := range q.Options {
		opts[i] = PublicOption{ID: string(o.ID), Text: o.Text}
	}
	return PublicQuestion{
		QuestionID: string(q.ID), Index: index, Count: count, Prompt: q.Prompt, Options: opts,
		OpenedAt: openedAt, Deadline: deadline, CloseAt: closeAt,
	}
}

// Server payloads.
type (
	// Snapshot is the full room view: reply to join/watch, and resync.
	Snapshot struct {
		QuizCode        string          `json:"quizCode"`
		Role            string          `json:"role"`
		Quiz            QuizState       `json:"quiz"`
		Question        *PublicQuestion `json:"question"`
		You             *You            `json:"you"`
		YourAnswer      *YourAnswer     `json:"yourAnswer"`
		CorrectOptionID *string         `json:"correctOptionId"`
		Leaderboard     Leaderboard     `json:"leaderboard"`
		ServerTime      int64           `json:"serverTime"`
	}
	// You is the participant's own standing in a snapshot.
	You struct {
		ParticipantID string `json:"participantId"`
		DisplayName   string `json:"displayName"`
		Score         int    `json:"score"`
		Rank          int    `json:"rank"`
	}
	// YourAnswer is the participant's accepted answer to the current question, if any.
	YourAnswer struct {
		QuestionID string `json:"questionId"`
		OptionID   string `json:"optionId"`
		Correct    bool   `json:"correct"`
		Points     int    `json:"points"`
	}
	// QuestionMsg announces an opened question, or an earlier close time.
	QuestionMsg struct {
		Question     PublicQuestion `json:"question"`
		StateVersion int64          `json:"stateVersion"`
	}
	// QuestionClosed reveals the correct option.
	QuestionClosed struct {
		QuestionID       string `json:"questionId"`
		CorrectOptionID  string `json:"correctOptionId"`
		NextTransitionAt int64  `json:"nextTransitionAt"`
		StateVersion     int64  `json:"stateVersion"`
	}
	// AnswerResult replies to submit_answer.
	AnswerResult struct {
		QuestionID string       `json:"questionId"`
		Status     AnswerStatus `json:"status"`
		OptionID   string       `json:"optionId"`
		Correct    bool         `json:"correct"`
		Points     int          `json:"points"`
		TotalScore int          `json:"totalScore"`
		ReceivedAt int64        `json:"receivedAt"`
	}
	// Rank is the participant's own rank after a question closes.
	Rank struct {
		QuestionID       string `json:"questionId"`
		Score            int    `json:"score"`
		Rank             int    `json:"rank"`
		ParticipantCount int    `json:"participantCount"`
	}
	// Leaderboard is the shared top of the room.
	Leaderboard struct {
		Version          int64              `json:"version"`
		ParticipantCount int                `json:"participantCount"`
		Top              []LeaderboardEntry `json:"top"`
	}
	// QuizFinished carries the final standings.
	QuizFinished struct {
		FinalTop         []LeaderboardEntry `json:"finalTop"`
		ParticipantCount int                `json:"participantCount"`
		StateVersion     int64              `json:"stateVersion"`
	}
	// QuizState is a lifecycle change without a question.
	QuizState struct {
		Status        quiz.Status `json:"status"`
		QuestionIndex int         `json:"questionIndex"`
		QuestionCount int         `json:"questionCount"`
		StateVersion  int64       `json:"stateVersion"`
	}
	// Error reports a failed request or a connection problem.
	Error struct {
		Code         ErrorCode `json:"code"`
		Message      string    `json:"message"`
		Retryable    bool      `json:"retryable"`
		RetryAfterMs *int64    `json:"retryAfterMs,omitempty"`
	}
	// Pong replies to ping.
	Pong struct {
		ClientTime int64 `json:"clientTime"`
		ServerTime int64 `json:"serverTime"`
	}
)

// AnswerStatus distinguishes a new answer from a resend (FR-18).
type AnswerStatus string

// Answer statuses.
const (
	AnswerAccepted  AnswerStatus = "accepted"
	AnswerDuplicate AnswerStatus = "duplicate"
)

// Roles in a snapshot.
const (
	RoleParticipant = "participant"
	RoleHost        = "host"
)

// MarshalJSON encodes an empty list as [] (the schema doesn't allow null).
func (l Leaderboard) MarshalJSON() ([]byte, error) {
	type plain Leaderboard
	if l.Top == nil {
		l.Top = []LeaderboardEntry{}
	}
	return json.Marshal(plain(l))
}

// MarshalJSON encodes an empty list as [] (the schema doesn't allow null).
func (f QuizFinished) MarshalJSON() ([]byte, error) {
	type plain QuizFinished
	if f.FinalTop == nil {
		f.FinalTop = []LeaderboardEntry{}
	}
	return json.Marshal(plain(f))
}
