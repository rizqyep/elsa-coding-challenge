package history

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// PostgresStore implements Store on PostgreSQL (TRD §5).
type PostgresStore struct{ pool *pgxpool.Pool }

// NewPostgresStore returns a store using pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// SaveAnswers inserts a question's answers in one statement; re-running it inserts nothing (TRD §5.3).
func (s *PostgresStore) SaveAnswers(ctx context.Context, code quiz.Code, questionID quiz.QuestionID, answers []StoredAnswer) error {
	n := len(answers)
	ids, options, correct, points, received := make([]string, n), make([]string, n), make([]bool, n), make([]int32, n), make([]int64, n)
	for i, a := range answers {
		ids[i], options[i], correct[i], points[i], received[i] = string(a.ParticipantID), string(a.OptionID), a.Correct, int32(a.Points), a.ReceivedAt //nolint:gosec // points ≤ 200
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO answers (quiz_code, question_id, participant_id, option_id, correct, points, received_at)
		SELECT $1, $2, p, o, c, pts, to_timestamp(r / 1000.0)
		FROM unnest($3::text[], $4::text[], $5::bool[], $6::int[], $7::bigint[]) AS t(p, o, c, pts, r)
		ON CONFLICT DO NOTHING`,
		string(code), string(questionID), ids, options, correct, points, received)
	if err != nil {
		return fmt.Errorf("insert answers: %w", err)
	}
	return nil
}

// Finalize writes the final results, reconciles totals with the stored answers, and closes the
// quiz, in one transaction (TRD §4.5, FR-35). Re-running it is harmless.
func (s *PostgresStore) Finalize(ctx context.Context, in FinalizeInput) ([]Mismatch, error) {
	if in.Status != quiz.StatusFinished && in.Status != quiz.StatusExpired {
		return nil, fmt.Errorf("finalize: status %q is not terminal", in.Status)
	}
	var mismatches []Mismatch
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		n := len(in.Results)
		ids, names, scores, ranks := make([]string, n), make([]string, n), make([]int32, n), make([]int32, n)
		live := make(map[string]int, n)
		for i, r := range in.Results {
			ids[i], names[i], scores[i], ranks[i] = string(r.ParticipantID), r.DisplayName, int32(r.Score), int32(r.Rank) //nolint:gosec // bounded by question count
			live[ids[i]] = r.Score
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO quiz_results (quiz_code, participant_id, display_name, total_score, rank)
			SELECT $1, p, d, s, r FROM unnest($2::text[], $3::text[], $4::int[], $5::int[]) AS t(p, d, s, r)
			ON CONFLICT DO NOTHING`, string(in.Code), ids, names, scores, ranks); err != nil {
			return fmt.Errorf("insert results: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT participant_id, COALESCE(SUM(points), 0) FROM answers WHERE quiz_code = $1 GROUP BY participant_id`, string(in.Code))
		if err != nil {
			return fmt.Errorf("reconcile: %w", err)
		}
		recomputed := map[string]int{}
		for rows.Next() {
			var id string
			var total int
			if err := rows.Scan(&id, &total); err != nil {
				return err
			}
			recomputed[id] = total
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for id, score := range live {
			if recomputed[id] != score {
				mismatches = append(mismatches, Mismatch{ParticipantID: quiz.ParticipantID(id), Live: score, Recomputed: recomputed[id]})
			}
		}
		for id, total := range recomputed {
			if _, ok := live[id]; !ok {
				mismatches = append(mismatches, Mismatch{ParticipantID: quiz.ParticipantID(id), Live: 0, Recomputed: total})
			}
		}
		_, err = tx.Exec(ctx, `UPDATE quizzes SET status = $2, finished_at = COALESCE(finished_at, now()) WHERE code = $1`, string(in.Code), string(in.Status))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("finalize: %w", err)
	}
	return mismatches, nil
}
