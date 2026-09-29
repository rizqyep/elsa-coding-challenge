package protocol

// ErrorCode is the machine-readable code of an error message (TRD §9.6).
type ErrorCode string

// Error codes; must match the ErrorCode enum in docs/api/schemas/common.json.
const (
	CodeInvalidMessage     ErrorCode = "invalid_message"
	CodeUnsupportedVersion ErrorCode = "unsupported_version"
	CodeUnknownType        ErrorCode = "unknown_type"
	CodeRateLimited        ErrorCode = "rate_limited"
	CodeForbidden          ErrorCode = "forbidden"
	CodeUnknownQuiz        ErrorCode = "unknown_quiz"
	CodeQuizExpired        ErrorCode = "quiz_expired"
	CodeAlreadyJoined      ErrorCode = "already_joined"
	CodeNotJoined          ErrorCode = "not_joined"
	CodeInvalidDisplayName ErrorCode = "invalid_display_name"
	CodeWrongQuestion      ErrorCode = "wrong_question"
	CodeQuestionClosed     ErrorCode = "question_closed"
	CodeInvalidOption      ErrorCode = "invalid_option"
	CodeServerBusy         ErrorCode = "server_busy"
	CodeInternal           ErrorCode = "internal"
)

// ErrorCodes lists every error code.
func ErrorCodes() []ErrorCode {
	return []ErrorCode{
		CodeInvalidMessage, CodeUnsupportedVersion, CodeUnknownType, CodeRateLimited, CodeForbidden,
		CodeUnknownQuiz, CodeQuizExpired, CodeAlreadyJoined, CodeNotJoined, CodeInvalidDisplayName,
		CodeWrongQuestion, CodeQuestionClosed, CodeInvalidOption, CodeServerBusy, CodeInternal,
	}
}

// DecodeError is a client frame that can't be accepted, with the code to reply with.
type DecodeError struct {
	Code ErrorCode
	Err  error
}

func (e *DecodeError) Error() string { return string(e.Code) + ": " + e.Err.Error() }

func (e *DecodeError) Unwrap() error { return e.Err }
