//go:build integration

package migrations_test

import (
	"context"
	"os"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

// Seed-validation checks from task-04, against a freshly migrated database.
func TestSeed_QuestionSetsAreWellFormed(t *testing.T) {
	ctx := context.Background()
	checks := []struct {
		name  string
		query string
	}{
		{"questions with fewer than 2 or more than 4 options",
			`select count(*) from (select q.id from questions q left join options o on o.question_id = q.id group by q.id having count(o.id) not between 2 and 4) x`},
		{"correct option outside its question",
			`select count(*) from questions q where not exists (select 1 from options o where o.id = q.correct_option_id and o.question_id = q.id)`},
		{"gaps in question positions",
			`select count(*) from (select set_id from questions group by set_id having min(position) <> 0 or max(position) <> count(*) - 1) x`},
		{"gaps in option positions",
			`select count(*) from (select question_id from options group by question_id having min(position) <> 0 or max(position) <> count(*) - 1) x`},
	}
	for _, c := range checks {
		var n int
		if err := env.Postgres.QueryRow(ctx, c.query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if n != 0 {
			t.Errorf("%s: %d", c.name, n)
		}
	}
}

func TestSeed_ExpectedSets(t *testing.T) {
	rows, err := env.Postgres.Query(context.Background(),
		`select s.id, count(q.id) from question_sets s join questions q on q.set_id = s.id group by s.id`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			t.Fatal(err)
		}
		got[id] = n
	}
	want := map[string]int{"demo-quick": 3, "synonyms-everyday": 10, "business-english": 10}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%s: %d questions, want %d", id, got[id], n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got sets %v, want %v", got, want)
	}
}
