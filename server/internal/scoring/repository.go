package scoring

import "context"

//go:generate go tool -modfile=../../tools/go.mod mockgen -source=repository.go -destination=mocks/repository.go -package=mocks

// Repository records answers atomically (NFR-12).
type Repository interface {
	RecordAnswer(ctx context.Context, in AnswerInput) (AnswerResult, error)
}
