//go:build integration

package leaderboard_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// 25 participants; the tie at 300 spans the first page boundary when pages hold 7 entries.
func manyScores() map[string]int {
	s := map[string]int{}
	for i := range 25 {
		s[fmt.Sprintf("u_%02d", i)] = 50 * (i / 3)
	}
	s["u_x"], s["u_y"], s["u_z"] = 300, 300, 300
	return s
}

func TestLivePage_RanksAndOrder(t *testing.T) {
	repo := setup(t, map[string]int{"u_a": 186, "u_b": 186, "u_c": 150, "u_d": 0})
	got, err := repo.LivePage(context.Background(), code, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := leaderboard.Page{Code: code, Status: quiz.StatusLobby, ParticipantCount: 4, Offset: 0, Limit: 100, Entries: []leaderboard.Entry{
		{Rank: 1, ParticipantID: "u_b", DisplayName: "P-u_b", Score: 186},
		{Rank: 1, ParticipantID: "u_a", DisplayName: "P-u_a", Score: 186},
		{Rank: 3, ParticipantID: "u_c", DisplayName: "P-u_c", Score: 150},
		{Rank: 4, ParticipantID: "u_d", DisplayName: "P-u_d", Score: 0},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// Walking the pages must give exactly the single-page result: same order, ranks shared across page boundaries.
func TestLivePage_PagesConcatenateToTheWhole(t *testing.T) {
	repo := setup(t, manyScores())
	ctx := context.Background()
	whole, err := repo.LivePage(ctx, code, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(whole.Entries) != 28 || whole.ParticipantCount != 28 {
		t.Fatalf("whole page has %d entries, count %d; want 28", len(whole.Entries), whole.ParticipantCount)
	}
	var walked []leaderboard.Entry
	for off := 0; off < 28; off += 7 {
		p, err := repo.LivePage(ctx, code, off, 7)
		if err != nil {
			t.Fatal(err)
		}
		if p.Offset != off || p.Limit != 7 || p.ParticipantCount != 28 {
			t.Errorf("page at %d: offset %d limit %d count %d", off, p.Offset, p.Limit, p.ParticipantCount)
		}
		walked = append(walked, p.Entries...)
	}
	if !reflect.DeepEqual(walked, whole.Entries) {
		t.Errorf("walked pages differ from the whole:\n%+v\n%+v", walked, whole.Entries)
	}
	for i, e := range whole.Entries {
		if want := rankAt(whole.Entries, i); e.Rank != want {
			t.Errorf("entry %d (%s, %d): rank %d, want %d", i, e.ParticipantID, e.Score, e.Rank, want)
		}
	}
}

// The live top 10 (leaderboard events) and page 1 of the REST leaderboard must agree.
func TestLivePage_MatchesTheLiveTop(t *testing.T) {
	repo := setup(t, manyScores())
	_, ev := publishAndReceive(t, repo)
	p, err := repo.LivePage(context.Background(), code, 0, leaderboard.TopN)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Top) != len(p.Entries) {
		t.Fatalf("event has %d entries, page %d", len(ev.Top), len(p.Entries))
	}
	for i, e := range p.Entries {
		if id := fmt.Sprint(ev.Top[i][0]); id != string(e.ParticipantID) {
			t.Errorf("position %d: event %s, page %s", i, id, e.ParticipantID)
		}
	}
}

func TestLivePage_Edges(t *testing.T) {
	repo := setup(t, map[string]int{"u_a": 100, "u_b": 50})
	ctx := context.Background()
	p, err := repo.LivePage(ctx, code, 5, 10)
	if err != nil || len(p.Entries) != 0 || p.ParticipantCount != 2 || p.Offset != 5 {
		t.Errorf("past the end: %+v, %v; want no entries and the count", p, err)
	}
	if p.Entries == nil {
		t.Error("entries is nil; the contract needs an empty array")
	}
	env.Redis.HSet(ctx, redisx.RoomKey(string(code)), "status", string(quiz.StatusQuestionOpen))
	if p, _ := repo.LivePage(ctx, code, 0, 10); p.Status != quiz.StatusQuestionOpen {
		t.Errorf("status %q, want question_open", p.Status)
	}
	if _, err := repo.LivePage(ctx, "ZZZZZZ", 0, 10); !errors.Is(err, quiz.ErrUnknownQuiz) {
		t.Errorf("missing room: %v, want ErrUnknownQuiz", err)
	}
}

func TestFinalPage(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	store := leaderboard.NewPostgresStore(env.Postgres)
	archive(t, "finished", map[string]int{"u_a": 186, "u_b": 186, "u_c": 150})

	got, err := store.FinalPage(ctx, code, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := leaderboard.Page{Code: code, Status: quiz.StatusFinished, ParticipantCount: 3, Limit: 100, Entries: []leaderboard.Entry{
		{Rank: 1, ParticipantID: "u_b", DisplayName: "P-u_b", Score: 186},
		{Rank: 1, ParticipantID: "u_a", DisplayName: "P-u_a", Score: 186},
		{Rank: 3, ParticipantID: "u_c", DisplayName: "P-u_c", Score: 150},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if p, _ := store.FinalPage(ctx, code, 1, 1); len(p.Entries) != 1 || p.Entries[0].ParticipantID != "u_a" || p.Entries[0].Rank != 1 {
		t.Errorf("second page = %+v; want u_a sharing rank 1", p.Entries)
	}
	if _, err := store.FinalPage(ctx, "ZZZZZZ", 0, 10); !errors.Is(err, quiz.ErrUnknownQuiz) {
		t.Errorf("unknown quiz: %v", err)
	}
}

func TestFinalPage_RoomGoneWithoutResults(t *testing.T) {
	env.Reset(t)
	archive(t, "running", nil)
	p, err := leaderboard.NewPostgresStore(env.Postgres).FinalPage(context.Background(), code, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != quiz.StatusExpired || len(p.Entries) != 0 || p.Entries == nil || p.ParticipantCount != 0 {
		t.Errorf("got %+v; want expired with an empty list", p)
	}
}

// Ties sort by participant ID byte order in Redis (ZREVRANGE); PostgreSQL must use the same order,
// whatever the database collation, or the final leaderboard reshuffles tied players (FR-13).
func TestFinalPage_TieOrderMatchesLive(t *testing.T) {
	scores := map[string]int{"u_B": 100, "u_a": 100, "u_C": 100, "u_b": 100, "u_10": 50, "u_9": 50, "u_é": 50, "u_e": 50}
	repo := setup(t, scores)
	live, err := repo.LivePage(context.Background(), code, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	// The test image sorts text bytewise (musl), which would hide the bug; use a real locale collation,
	// as a glibc or managed PostgreSQL would have.
	ctx := context.Background()
	if _, err := env.Postgres.Exec(ctx, `ALTER TABLE quiz_results ALTER COLUMN participant_id TYPE text COLLATE "en-US-x-icu"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := env.Postgres.Exec(ctx, `ALTER TABLE quiz_results ALTER COLUMN participant_id TYPE text COLLATE "default"`); err != nil {
			t.Error(err)
		}
	})
	archive(t, "finished", scores)
	pool, err := pgxpool.New(ctx, env.PostgresDSN) // fresh pool: cached plans predate the ALTER
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	final, err := leaderboard.NewPostgresStore(pool).FinalPage(ctx, code, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(final.Entries, live.Entries) {
		t.Errorf("final order differs from live:\nlive  %+v\nfinal %+v", live.Entries, final.Entries)
	}
}

// archive writes the quiz row and its results the way the final job does (ranked, TRD §4.5).
func archive(t *testing.T, status string, scores map[string]int) {
	t.Helper()
	ctx := context.Background()
	if _, err := env.Postgres.Exec(ctx, `INSERT INTO quizzes (code, question_set_id, host_id, status, window_ms, reveal_ms)
		VALUES ($1, 'demo-quick', 'host_1', $2, 10000, 3000)`, string(code), status); err != nil {
		t.Fatal(err)
	}
	for id, s := range scores {
		rank := 1
		for _, other := range scores {
			if other > s {
				rank++
			}
		}
		if _, err := env.Postgres.Exec(ctx, `INSERT INTO quiz_results (quiz_code, participant_id, display_name, total_score, rank)
			VALUES ($1, $2, $3, $4, $5)`, string(code), id, "P-"+id, s, rank); err != nil {
			t.Fatal(err)
		}
	}
}

// rankAt is 1 + the number of entries with a strictly higher score (FR-24).
func rankAt(entries []leaderboard.Entry, i int) int {
	r := 1
	for _, e := range entries {
		if e.Score > entries[i].Score {
			r++
		}
	}
	return r
}
