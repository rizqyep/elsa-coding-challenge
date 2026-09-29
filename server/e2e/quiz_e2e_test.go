//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/testkit"
)

// A whole quiz through nginx, gateways, and workers: every answer's points and every archived total
// match an independent recomputation (FR-2, FR-4, FR-7, FR-19, FR-35).
func TestE2E_QuizRunsToFinish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := env.RunRoom(ctx, testkit.RoomSpec{QuestionSet: "demo-quick", Participants: 30, JoinRamp: time.Second,
		Window: 5 * time.Second, Reveal: 2 * time.Second, AnswerWithin: 2 * time.Second, CorrectRatio: 0.7, NoAnswerRatio: 0.1, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	requireClean(t, res)
	if res.Accepted == 0 || res.NFR6.Count == 0 || res.NFR8.Count == 0 {
		t.Errorf("nothing measured: accepted %d, NFR-6 %d, NFR-8 %d", res.Accepted, res.NFR6.Count, res.NFR8.Count)
	}
}
