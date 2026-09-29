package quiz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store on PostgreSQL (TRD §5).
type PostgresStore struct{ pool *pgxpool.Pool }

// NewPostgresStore returns a store using pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// QuestionSets lists sets that have at least one question, by ID.
func (s *PostgresStore) QuestionSets(ctx context.Context) ([]QuestionSetSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT qs.id, qs.title, count(q.id)
		FROM question_sets qs JOIN questions q ON q.set_id = qs.id
		GROUP BY qs.id, qs.title
		ORDER BY qs.id`)
	if err != nil {
		return nil, fmt.Errorf("list question sets: %w", err)
	}
	sets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (QuestionSetSummary, error) {
		var q QuestionSetSummary
		err := r.Scan(&q.ID, &q.Title, &q.QuestionCount)
		return q, err
	})
	if err != nil {
		return nil, fmt.Errorf("list question sets: %w", err)
	}
	return sets, nil
}

// QuestionIDs returns the set's question IDs by position.
func (s *PostgresStore) QuestionIDs(ctx context.Context, id QuestionSetID) ([]QuestionID, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM questions WHERE set_id = $1 ORDER BY position`, string(id))
	if err != nil {
		return nil, fmt.Errorf("question ids: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[QuestionID])
	if err != nil {
		return nil, fmt.Errorf("question ids: %w", err)
	}
	if len(ids) == 0 {
		return nil, ErrQuestionSetNotFound
	}
	return ids, nil
}

// CreateQuiz inserts the quiz, creates the live room, and commits only if both succeed (TRD §5.2).
func (s *PostgresStore) CreateQuiz(ctx context.Context, q NewQuiz, live func(ctx context.Context) (int64, error)) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO quizzes (code, question_set_id, host_id, status, window_ms, reveal_ms)
			VALUES ($1, $2, $3, 'lobby', $4, $5)`,
			string(q.Code), string(q.QuestionSetID), string(q.HostID), q.WindowMs, q.RevealMs)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "quizzes_pkey" {
			return ErrCodeTaken
		}
		if err != nil {
			return fmt.Errorf("insert quiz: %w", err)
		}
		createdAt, err := live(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE quizzes SET created_at = $2 WHERE code = $1`, string(q.Code), time.UnixMilli(createdAt)); err != nil {
			return fmt.Errorf("set created_at: %w", err)
		}
		return nil
	})
}

// MarkRunning moves a lobby quiz to running.
func (s *PostgresStore) MarkRunning(ctx context.Context, code Code) error {
	if _, err := s.pool.Exec(ctx, `UPDATE quizzes SET status = 'running' WHERE code = $1 AND status = 'lobby'`, string(code)); err != nil {
		return fmt.Errorf("mark running: %w", err)
	}
	return nil
}

// Quiz reads an archived quiz with its question and participant counts.
func (s *PostgresStore) Quiz(ctx context.Context, code Code) (StoredQuiz, error) {
	var q StoredQuiz
	err := s.pool.QueryRow(ctx, `
		SELECT q.code, q.question_set_id, q.status, q.window_ms, q.reveal_ms, q.created_at, q.finished_at,
		       (SELECT count(*) FROM questions WHERE set_id = q.question_set_id),
		       (SELECT count(*) FROM quiz_results WHERE quiz_code = q.code)
		FROM quizzes q WHERE q.code = $1`, string(code)).
		Scan(&q.Code, &q.QuestionSetID, &q.Status, &q.WindowMs, &q.RevealMs, &q.CreatedAt, &q.FinishedAt, &q.QuestionCount, &q.ParticipantCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredQuiz{}, ErrUnknownQuiz
	}
	if err != nil {
		return StoredQuiz{}, fmt.Errorf("read quiz: %w", err)
	}
	return q, nil
}
