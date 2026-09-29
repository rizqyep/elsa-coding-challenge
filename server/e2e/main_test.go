//go:build e2e

// Package e2e runs the end-to-end and fault tests against the Compose stack (`make test-e2e`, TRD §10.4, §10.6).
package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/testkit"
)

var env *testkit.Compose

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	var err error
	env, err = testkit.NewCompose(ctx, getenv("E2E_BASE_URL", "http://localhost:8080"),
		getenv("E2E_POSTGRES_DSN", "postgres://quiz:quiz-local-only@localhost:15432/quiz?sslmode=disable"))
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: the stack must be running (make up):", err)
		os.Exit(1)
	}
	code := m.Run()
	env.Close()
	os.Exit(code)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// requireClean fails the test on anything a correct system never does, and on non-zero reconciliation mismatches.
func requireClean(t *testing.T, res *testkit.Result) {
	t.Helper()
	t.Logf("quiz %s: %d/%d joined, %d finished, %d answers (%d accepted, %d duplicate), %d reconnects, errors %v, closes %v, %.1fs",
		res.Code, res.Joined, res.Participants, res.Finished, res.AnswersSent, res.Accepted, res.Duplicates, res.Reconnects, res.Errors, res.Closes, res.Elapsed.Seconds())
	t.Logf("NFR-6 p95 %v · NFR-7 p95 %v · NFR-8 p95 %v · NFR-9 p95 %v · transition lag p95 %v",
		res.NFR6.P95, res.NFR7.P95, res.NFR8.P95, res.NFR9.P95, res.TransitionLag.P95)
	for _, list := range [][]string{res.Violations, res.PointMismatches, res.TotalMismatches, res.StepErrors} {
		for _, s := range list {
			t.Error(s)
		}
	}
	if res.Joined != res.Participants || res.Finished != res.Joined {
		t.Errorf("%d joined and %d finished of %d", res.Joined, res.Finished, res.Participants)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := env.Metrics(ctx, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if n := m["score_reconciliation_mismatches_total"]; n != 0 {
		t.Errorf("score_reconciliation_mismatches_total = %v, want 0 (FR-35)", n)
	}
}
