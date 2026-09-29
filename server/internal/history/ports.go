package history

import (
	"context"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

//go:generate go tool -modfile=../../tools/go.mod mockgen -source=ports.go -destination=mocks/ports.go -package=mocks

// LiveStore is the Redis side of persistence jobs.
type LiveStore interface {
	Claim(ctx context.Context, batch int, visibility time.Duration) ([]Job, error)
	ExtendTTL(ctx context.Context, code quiz.Code, questionID quiz.QuestionID, ttl time.Duration) error
	ReadAnswers(ctx context.Context, code quiz.Code, questionID quiz.QuestionID) ([]StoredAnswer, error)
	AckFlush(ctx context.Context, job Job) error
	PendingFlushes(ctx context.Context, code quiz.Code) (int64, error)
	Defer(ctx context.Context, job Job, delay time.Duration) error
	ReadFinal(ctx context.Context, code quiz.Code) (FinalData, error)
	Release(ctx context.Context, job Job) error
}

// Store is the persistent side (PostgreSQL).
type Store interface {
	SaveAnswers(ctx context.Context, code quiz.Code, questionID quiz.QuestionID, answers []StoredAnswer) error
	Finalize(ctx context.Context, in FinalizeInput) ([]Mismatch, error)
}
