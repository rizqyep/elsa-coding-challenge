package scoring

import (
	"errors"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// AnswerInput is one submitted answer; Correct comes from the gateway's answer-key cache.
type AnswerInput struct {
	Code          quiz.Code
	ParticipantID quiz.ParticipantID
	QuestionID    quiz.QuestionID
	OptionID      quiz.OptionID
	Correct       bool
}

// AnswerStatus says whether the answer is new or a resend (FR-18).
type AnswerStatus string

// Answer statuses.
const (
	Accepted  AnswerStatus = "accepted"
	Duplicate AnswerStatus = "duplicate"
)

// AnswerResult is the recorded answer. A duplicate carries the original answer's values.
type AnswerResult struct {
	Status     AnswerStatus
	OptionID   quiz.OptionID
	Correct    bool
	Points     int
	Total      int
	ReceivedAt int64
	Replicated bool // a replica acknowledged it (only when WAIT is enabled)
}

// Rejections (TRD §3.4 acceptance order).
var (
	ErrQuestionClosed = errors.New("question closed")
	ErrWrongQuestion  = errors.New("not the current question")
	ErrNotJoined      = errors.New("not joined")
)

// Failures.
var (
	ErrUnexpectedReply = errors.New("unexpected script reply")
	ErrServerBusy      = errors.New("server busy; retry with the same request id")
	ErrInternal        = errors.New("internal error")
)
