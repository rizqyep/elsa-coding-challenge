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

// Final is an archived quiz as a late join or watch sees it (FR-13).
type Final struct {
	Status           quiz.Status
	HostID           quiz.ParticipantID
	QuestionCount    int
	ParticipantCount int
	Top              []Entry
	You              *Entry // the caller's own result; nil if they didn't take part
}

// Final reads an archived quiz's top and, if participantID took part, their own result.
func (s *PostgresStore) Final(ctx context.Context, code quiz.Code, participantID quiz.ParticipantID) (Final, error) {
	page, err := s.FinalPage(ctx, code, 0, TopN)
	if err != nil {
		return Final{}, err
	}
	f := Final{Status: page.Status, ParticipantCount: page.ParticipantCount, Top: page.Entries}
	err = s.pool.QueryRow(ctx, `
		SELECT q.host_id, (SELECT count(*) FROM questions WHERE set_id = q.question_set_id)
		FROM quizzes q WHERE q.code = $1`, string(code)).Scan(&f.HostID, &f.QuestionCount)
	if err != nil {
		return Final{}, fmt.Errorf("final: %w", err)
	}
	if participantID == "" {
		return f, nil
	}
	var you Entry
	err = s.pool.QueryRow(ctx, `
		SELECT rank, participant_id, display_name, total_score FROM quiz_results
		WHERE quiz_code = $1 AND participant_id = $2`, string(code), string(participantID)).
		Scan(&you.Rank, &you.ParticipantID, &you.DisplayName, &you.Score)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Final{}, fmt.Errorf("final: %w", err)
	default:
		f.You = &you
	}
	return f, nil
}
