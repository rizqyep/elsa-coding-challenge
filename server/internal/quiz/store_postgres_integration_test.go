//go:build integration

package quiz_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

func newStore(t *testing.T) *quiz.PostgresStore {
	t.Helper()
	env.Reset(t)
	return quiz.NewPostgresStore(env.Postgres)
}

var row = quiz.NewQuiz{Code: "K7Q2MX", QuestionSetID: "demo-quick", HostID: "host_1", WindowMs: 10_000, RevealMs: 3_000}

func liveAt(ms int64) func(context.Context) (int64, error) {
	return func(context.Context) (int64, error) { return ms, nil }
}

func archived(t *testing.T, code quiz.Code) (status string, created time.Time, found bool) {
	t.Helper()
	err := env.Postgres.QueryRow(context.Background(), `SELECT status, created_at FROM quizzes WHERE code = $1`, string(code)).Scan(&status, &created)
	return status, created, err == nil
}

func TestStore_QuestionSets(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	// schema: questionCount has minimum 1, so a set without questions must not be offered.
	if _, err := env.Postgres.Exec(ctx, `INSERT INTO question_sets (id, title) VALUES ('empty-set', 'Empty') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	got, err := s.QuestionSets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []quiz.QuestionSetSummary{
		{ID: "business-english", Title: "Business English", QuestionCount: 10},
		{ID: "demo-quick", Title: "Quick demo", QuestionCount: 3},
		{ID: "synonyms-everyday", Title: "Everyday synonyms", QuestionCount: 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestStore_QuestionIDs(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	got, err := s.QuestionIDs(ctx, "demo-quick")
	if err != nil {
		t.Fatal(err)
	}
	if want := []quiz.QuestionID{"dq-01", "dq-02", "dq-03"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if _, err := env.Postgres.Exec(ctx, `INSERT INTO question_sets (id, title) VALUES ('empty-set', 'Empty') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []quiz.QuestionSetID{"no-such-set", "empty-set"} {
		if _, err := s.QuestionIDs(ctx, id); !errors.Is(err, quiz.ErrQuestionSetNotFound) {
			t.Errorf("%s: got %v, want ErrQuestionSetNotFound", id, err)
		}
	}
}

// PostgreSQL stores the creation time Redis reported, so live and archived reads agree.
func TestStore_CreateQuiz_CommitsWithTheLiveCreationTime(t *testing.T) {
	s := newStore(t)
	const ms = int64(1_790_000_000_123)
	if err := s.CreateQuiz(context.Background(), row, liveAt(ms)); err != nil {
		t.Fatal(err)
	}
	status, created, found := archived(t, row.Code)
	if !found || status != quiz.ArchiveLobby || created.UnixMilli() != ms {
		t.Errorf("row: found=%v status=%q created=%d, want lobby at %d", found, status, created.UnixMilli(), ms)
	}
}

// TRD §5.2: if Redis fails, the transaction rolls back and the code is free again.
func TestStore_CreateQuiz_LiveFailureRollsBack(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	boom := errors.New("redis down")
	err := s.CreateQuiz(ctx, row, func(context.Context) (int64, error) { return 0, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the live error", err)
	}
	if _, _, found := archived(t, row.Code); found {
		t.Fatal("row committed despite the live failure")
	}
	if err := s.CreateQuiz(ctx, row, liveAt(1)); err != nil {
		t.Errorf("code not reusable after rollback: %v", err)
	}
}

func TestStore_CreateQuiz_TakenCode(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.CreateQuiz(ctx, row, liveAt(1)); err != nil {
		t.Fatal(err)
	}
	called := false
	err := s.CreateQuiz(ctx, row, func(context.Context) (int64, error) { called = true; return 1, nil })
	if !errors.Is(err, quiz.ErrCodeTaken) {
		t.Errorf("got %v, want ErrCodeTaken", err)
	}
	if called {
		t.Error("created the Redis room for a taken code")
	}
}

func TestStore_MarkRunning(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.CreateQuiz(ctx, row, liveAt(1)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.MarkRunning(ctx, row.Code); err != nil {
			t.Fatal(err)
		}
	}
	if status, _, _ := archived(t, row.Code); status != quiz.ArchiveRunning {
		t.Errorf("status %q, want running", status)
	}
	if _, err := env.Postgres.Exec(ctx, `UPDATE quizzes SET status = 'finished' WHERE code = $1`, string(row.Code)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRunning(ctx, row.Code); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := archived(t, row.Code); status != quiz.ArchiveFinished {
		t.Errorf("finished quiz moved back to %q", status)
	}
	if err := s.MarkRunning(ctx, "ZZZZZZ"); err != nil {
		t.Errorf("unknown code: %v", err)
	}
}

func TestStore_Quiz(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	const ms = int64(1_790_000_000_000)
	if err := s.CreateQuiz(ctx, row, liveAt(ms)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Quiz(ctx, row.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != quiz.ArchiveLobby || got.QuestionCount != 3 || got.ParticipantCount != 0 || got.FinishedAt != nil ||
		got.WindowMs != 10_000 || got.RevealMs != 3_000 || got.QuestionSetID != "demo-quick" || got.CreatedAt.UnixMilli() != ms {
		t.Errorf("lobby quiz = %+v", got)
	}

	finished := time.UnixMilli(ms + 60_000).UTC()
	if _, err := env.Postgres.Exec(ctx, `UPDATE quizzes SET status = 'finished', finished_at = $2 WHERE code = $1`, string(row.Code), finished); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Postgres.Exec(ctx, `INSERT INTO quiz_results (quiz_code, participant_id, display_name, total_score, rank)
		VALUES ($1, 'u_1', 'Rina', 300, 1), ($1, 'u_2', 'Tomas', 100, 2)`, string(row.Code)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Quiz(ctx, row.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != quiz.ArchiveFinished || got.ParticipantCount != 2 || got.FinishedAt == nil || !got.FinishedAt.Equal(finished) {
		t.Errorf("finished quiz = %+v", got)
	}
	if _, err := s.Quiz(ctx, "ZZZZZZ"); !errors.Is(err, quiz.ErrUnknownQuiz) {
		t.Errorf("unknown code: %v, want ErrUnknownQuiz", err)
	}
}

func TestStore_QuestionSet(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	got, err := s.QuestionSet(ctx, "demo-quick")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "demo-quick" || got.Title != "Quick demo" || len(got.Questions) != 3 {
		t.Fatalf("set = %+v", got)
	}
	q := got.Questions[0]
	want := []quiz.Option{{ID: "dq-01-a", Text: "slow"}, {ID: "dq-01-b", Text: "quick"}, {ID: "dq-01-c", Text: "quiet"}, {ID: "dq-01-d", Text: "heavy"}}
	if q.ID != "dq-01" || q.Prompt != "Choose the synonym of 'rapid'" || q.CorrectOptionID != "dq-01-b" || !reflect.DeepEqual(q.Options, want) {
		t.Errorf("first question = %+v", q)
	}
	if got.Questions[1].ID != "dq-02" || got.Questions[2].ID != "dq-03" {
		t.Error("questions out of position order")
	}
	if _, err := s.QuestionSet(ctx, "no-such-set"); !errors.Is(err, quiz.ErrQuestionSetNotFound) {
		t.Errorf("unknown set: %v", err)
	}
	// Every seeded set loads and passes the cache's validation.
	sets, err := s.QuestionSets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sum := range sets {
		full, err := s.QuestionSet(ctx, sum.ID)
		if err != nil || len(full.Questions) != sum.QuestionCount {
			t.Errorf("%s: %d questions, %v; want %d", sum.ID, len(full.Questions), err, sum.QuestionCount)
		}
		if err := quiz.ValidateSet(full); err != nil {
			t.Errorf("%s: %v", sum.ID, err)
		}
	}
}

// A set that breaks the shape rules in the database is refused by the cache, not served (TRD §7.8).
func TestCache_RefusesMalformedSetFromPostgres(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	cleanup := []string{
		`DELETE FROM options WHERE question_id LIKE 'broken-%'`,
		`DELETE FROM questions WHERE set_id = 'broken'`,
		`DELETE FROM question_sets WHERE id = 'broken'`,
	}
	t.Cleanup(func() { // question sets survive Reset; don't leak this one into other tests
		for _, stmt := range cleanup {
			if _, err := env.Postgres.Exec(ctx, stmt); err != nil {
				t.Error(err)
			}
		}
	})
	for _, stmt := range []string{
		`INSERT INTO question_sets (id, title) VALUES ('broken', 'Broken')`,
		`INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('broken-1', 'broken', 0, 'p', 'broken-1-z')`,
		`INSERT INTO options (id, question_id, position, text) VALUES ('broken-1-a', 'broken-1', 0, 'a'), ('broken-1-b', 'broken-1', 1, 'b')`,
	} {
		if _, err := env.Postgres.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	var logs bytes.Buffer
	cache := quiz.NewCache(s, quiz.CacheOptions{MaxSets: 4, Retry: retry.New(retry.Policy{Base: time.Millisecond, Cap: time.Millisecond, Budget: time.Second}, nil),
		AttemptTimeout: time.Second, Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if _, err := cache.Acquire(ctx, "broken"); !errors.Is(err, quiz.ErrMalformedSet) {
		t.Fatalf("got %v, want ErrMalformedSet", err)
	}
	if _, ok := cache.Get("broken"); ok || !strings.Contains(logs.String(), "broken") {
		t.Errorf("cached=%v, logs %q", ok, logs.String())
	}
	if set, err := cache.Acquire(ctx, "demo-quick"); err != nil || len(set.Questions) != 3 {
		t.Errorf("good set: %v, %v", set, err)
	}
}
