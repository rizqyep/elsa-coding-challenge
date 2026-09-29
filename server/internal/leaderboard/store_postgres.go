package leaderboard

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// PostgresStore reads final results (TRD §5.1).
type PostgresStore struct{ pool *pgxpool.Pool }

// NewPostgresStore returns a store using pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// FinalPage reads a page of final results. Ties order by participant ID in byte order, as Redis does.
func (s *PostgresStore) FinalPage(ctx context.Context, code quiz.Code, offset, limit int) (Page, error) {
	p := Page{Code: code, Offset: offset, Limit: limit, Entries: []Entry{}}
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT q.status, (SELECT count(*) FROM quiz_results WHERE quiz_code = q.code)
		FROM quizzes q WHERE q.code = $1`, string(code)).Scan(&status, &p.ParticipantCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, quiz.ErrUnknownQuiz
	}
	if err != nil {
		return Page{}, fmt.Errorf("final page: %w", err)
	}
	p.Status = quiz.ArchivedStatus(status)
	rows, err := s.pool.Query(ctx, `
		SELECT rank, participant_id, display_name, total_score FROM quiz_results
		WHERE quiz_code = $1 ORDER BY rank, participant_id COLLATE "C" DESC OFFSET $2 LIMIT $3`,
		string(code), offset, limit)
	if err != nil {
		return Page{}, fmt.Errorf("final page: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := r.Scan(&e.Rank, &e.ParticipantID, &e.DisplayName, &e.Score)
		return e, err
	})
	if err != nil {
		return Page{}, fmt.Errorf("final page: %w", err)
	}
	if len(entries) > 0 {
		p.Entries = entries
	}
	return p, nil
}
