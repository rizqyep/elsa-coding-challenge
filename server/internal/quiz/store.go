package quiz

import (
	"context"
	"errors"
	"time"
)

//go:generate go tool -modfile=../../tools/go.mod mockgen -source=store.go -destination=mocks/store.go -package=mocks

// Store is the persistent side of quizzes and question sets (PostgreSQL, TRD §5).
type Store interface {
	// QuestionSets lists sets that have at least one question.
	QuestionSets(ctx context.Context) ([]QuestionSetSummary, error)
	// QuestionIDs returns a set's question IDs in order, or ErrQuestionSetNotFound.
	QuestionIDs(ctx context.Context, id QuestionSetID) ([]QuestionID, error)
	// CreateQuiz inserts the quiz, runs live inside the same transaction, stores the creation time
	// live returns, and commits. ErrCodeTaken if the code exists; any error from live rolls back (TRD §5.2).
	CreateQuiz(ctx context.Context, q NewQuiz, live func(ctx context.Context) (createdAtMs int64, err error)) error
	// MarkRunning moves a quiz from lobby to running; other statuses are left alone.
	MarkRunning(ctx context.Context, code Code) error
	// Quiz reads an archived quiz, or ErrUnknownQuiz.
	Quiz(ctx context.Context, code Code) (StoredQuiz, error)
}

// Store errors.
var (
	ErrCodeTaken           = errors.New("quiz code already taken")
	ErrQuestionSetNotFound = errors.New("question set not found")
)

// QuestionSetSummary is a question set as hosts see it when choosing one.
type QuestionSetSummary struct {
	ID            QuestionSetID
	Title         string
	QuestionCount int
}

// NewQuiz is the row written when a quiz is created.
type NewQuiz struct {
	Code          Code
	QuestionSetID QuestionSetID
	HostID        ParticipantID
	WindowMs      int64
	RevealMs      int64
}

// Archive statuses in PostgreSQL (TRD §5.1); the fine-grained state lives only in Redis.
const (
	ArchiveLobby    = "lobby"
	ArchiveRunning  = "running"
	ArchiveFinished = "finished"
	ArchiveExpired  = "expired"
)

// StoredQuiz is a quiz as recorded in PostgreSQL.
type StoredQuiz struct {
	Code             Code
	QuestionSetID    QuestionSetID
	Status           string
	WindowMs         int64
	RevealMs         int64
	QuestionCount    int
	ParticipantCount int
	CreatedAt        time.Time
	FinishedAt       *time.Time
}
