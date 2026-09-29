//go:build integration

package leaderboard_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

func TestFinal_TopOwnResultAndQuizShape(t *testing.T) {
	env.Reset(t)
	scores := map[string]int{"u_a": 186, "u_b": 186, "u_c": 150}
	for i := range 12 {
		scores[fmt.Sprintf("u_z%02d", i)] = 10
	}
	archive(t, "finished", scores)
	store := leaderboard.NewPostgresStore(env.Postgres)

	got, err := store.Final(context.Background(), code, "u_z05")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != quiz.StatusFinished || got.HostID != "host_1" || got.QuestionCount != 3 || got.ParticipantCount != 15 {
		t.Errorf("shape = %+v; want finished, host_1, 3 questions (demo-quick), 15 participants", got)
	}
	if len(got.Top) != leaderboard.TopN {
		t.Fatalf("top has %d entries, want %d", len(got.Top), leaderboard.TopN)
	}
	page, _ := store.FinalPage(context.Background(), code, 0, leaderboard.TopN)
	if !reflect.DeepEqual(got.Top, page.Entries) {
		t.Errorf("top differs from the first final page:\n%+v\n%+v", got.Top, page.Entries)
	}
	want := leaderboard.Entry{Rank: 4, ParticipantID: "u_z05", DisplayName: "P-u_z05", Score: 10}
	if got.You == nil || *got.You != want {
		t.Errorf("own result = %+v, want %+v (outside the top, still found)", got.You, want)
	}
}

func TestFinal_NonParticipantHasNoOwnResult(t *testing.T) {
	env.Reset(t)
	archive(t, "finished", map[string]int{"u_a": 100})
	got, err := leaderboard.NewPostgresStore(env.Postgres).Final(context.Background(), code, "u_never")
	if err != nil {
		t.Fatal(err)
	}
	if got.You != nil {
		t.Errorf("own result %+v for someone who never took part", got.You)
	}
	if got, _ := leaderboard.NewPostgresStore(env.Postgres).Final(context.Background(), code, ""); got.You != nil {
		t.Errorf("own result %+v for the host", got.You)
	}
}

func TestFinal_ExpiredAndUnknown(t *testing.T) {
	env.Reset(t)
	archive(t, "expired", nil)
	store := leaderboard.NewPostgresStore(env.Postgres)
	got, err := store.Final(context.Background(), code, "u_a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != quiz.StatusExpired || got.Top == nil || len(got.Top) != 0 {
		t.Errorf("got %+v; want expired with an empty top", got)
	}
	if _, err := store.Final(context.Background(), "ZZZZZZ", "u_a"); !errors.Is(err, quiz.ErrUnknownQuiz) {
		t.Errorf("unknown quiz: %v, want ErrUnknownQuiz", err)
	}
}
