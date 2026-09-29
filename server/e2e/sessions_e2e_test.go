//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/testkit"
)

// quizWith creates a demo-quick quiz with short windows and returns its host token and code.
func quizWith(ctx context.Context, t *testing.T, window time.Duration) (testkit.Token, string) {
	t.Helper()
	host, err := env.DevToken(ctx, "host")
	if err != nil {
		t.Fatal(err)
	}
	code, err := env.CreateQuiz(ctx, host, "demo-quick", window, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return host, code
}

// joinAs connects with tok and joins code, returning the client and its snapshot.
func joinAs(ctx context.Context, t *testing.T, tok testkit.Token, code, name string) (*testkit.Client, testkit.Message) {
	t.Helper()
	c, err := testkit.Dial(ctx, env.WSURL(), tok.Token, env.Origin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	from := len(c.Messages())
	if _, err := c.Send("join", map[string]any{"quizCode": code, "displayName": name}); err != nil {
		t.Fatal(err)
	}
	snap, err := c.Expect(ctx, from, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	return c, snap
}

func participant(ctx context.Context, t *testing.T) testkit.Token {
	t.Helper()
	tok, err := env.DevToken(ctx, "participant")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func noViolations(t *testing.T, cs ...*testkit.Client) {
	t.Helper()
	for _, c := range cs {
		for _, v := range c.Violations() {
			t.Error(v)
		}
	}
}

// A second connection for the same identity replaces the first with 4000, whichever gateway each lands on (FR-12).
func TestE2E_SecondConnectionKicksFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, code := quizWith(ctx, t, 10*time.Second)
	tok := participant(ctx, t)
	first, _ := joinAs(ctx, t, tok, code, "Rina")
	second, snap := joinAs(ctx, t, tok, code, "Rina")
	select {
	case <-first.Done():
	case <-ctx.Done():
		t.Fatal("first connection was not replaced")
	}
	if first.CloseCode() != 4000 {
		t.Errorf("first closed with %d, want 4000", first.CloseCode())
	}
	if you := snap.Data["you"].(map[string]any); you["participantId"] != tok.ParticipantID {
		t.Errorf("second snapshot is for %v", you)
	}
	select {
	case <-second.Done():
		t.Errorf("second connection closed too (%d)", second.CloseCode())
	case <-time.After(500 * time.Millisecond):
	}
	noViolations(t, first, second)
}

// Resending an answer with its original id after a reconnect returns the original result and scores once (FR-18, FR-30).
func TestE2E_ResendAfterReconnect_NoDoubleScore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	host, code := quizWith(ctx, t, 10*time.Second)
	keys, err := env.AnswerKeys(ctx, "demo-quick")
	if err != nil {
		t.Fatal(err)
	}
	tok, other := participant(ctx, t), participant(ctx, t)
	c, _ := joinAs(ctx, t, tok, code, "Rina")
	joinAs(ctx, t, other, code, "Budi") // stays silent, so the question stays open
	if err := env.Start(ctx, host, code); err != nil {
		t.Fatal(err)
	}
	qm, err := c.Expect(ctx, 0, "question")
	if err != nil {
		t.Fatal(err)
	}
	qid := qm.Data["question"].(map[string]any)["questionId"].(string)
	answer := map[string]any{"questionId": qid, "optionId": keys[qid]}
	if err := c.SendID("a-1", "submit_answer", answer); err != nil {
		t.Fatal(err)
	}
	first, err := c.WaitFor(ctx, 0, func(m testkit.Message) bool { return m.ID == "a-1" })
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	c2, _ := joinAs(ctx, t, tok, code, "Rina")
	if err := c2.SendID("a-1", "submit_answer", answer); err != nil {
		t.Fatal(err)
	}
	again, err := c2.WaitFor(ctx, 0, func(m testkit.Message) bool { return m.ID == "a-1" })
	if err != nil {
		t.Fatal(err)
	}
	if first.Data["status"] != "accepted" || again.Data["status"] != "duplicate" ||
		again.Data["points"] != first.Data["points"] || again.Data["totalScore"] != first.Data["points"] {
		t.Fatalf("first %v, resend %v; want the original result back and the total counted once", first.Data, again.Data)
	}
	noViolations(t, c, c2)
}

// Someone who drops out and comes back mid-quiz resumes at the current question; what they missed scores 0 (FR-29).
func TestE2E_ReconnectResumesAtCurrentQuestion_MissedScoresZero(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	host, code := quizWith(ctx, t, 5*time.Second)
	keys, err := env.AnswerKeys(ctx, "demo-quick")
	if err != nil {
		t.Fatal(err)
	}
	away, stay := participant(ctx, t), participant(ctx, t)
	c, _ := joinAs(ctx, t, away, code, "Rina")
	s, _ := joinAs(ctx, t, stay, code, "Budi")
	c.Close() // gone before the quiz starts; presence still counts them for a while
	if err := env.Start(ctx, host, code); err != nil {
		t.Fatal(err)
	}
	// Wait for question 2 on the connection that stayed.
	q2, err := s.WaitFor(ctx, 0, func(m testkit.Message) bool {
		q, _ := m.Data["question"].(map[string]any)
		return m.Type == "question" && q["index"] == 1.0
	})
	if err != nil {
		t.Fatal(err)
	}
	qid := q2.Data["question"].(map[string]any)["questionId"].(string)

	back, snap := joinAs(ctx, t, away, code, "Rina")
	sq, _ := snap.Data["question"].(map[string]any)
	if sq == nil || sq["questionId"] != qid {
		t.Fatalf("rejoin snapshot question %v, want the current %s", sq, qid)
	}
	if you := snap.Data["you"].(map[string]any); you["score"] != 0.0 {
		t.Errorf("score after missing question 1: %v, want 0", you["score"])
	}
	if _, err := back.Send("submit_answer", map[string]any{"questionId": qid, "optionId": keys[qid]}); err != nil {
		t.Fatal(err)
	}
	res, err := back.Expect(ctx, 0, "answer_result")
	if err != nil {
		t.Fatal(err)
	}
	if res.Data["status"] != "accepted" || res.Data["totalScore"] != res.Data["points"] {
		t.Errorf("answer after rejoining: %v", res.Data)
	}
	noViolations(t, back, s)
}

// The host leaving after the start doesn't stop the quiz: the server drives it (FR-31).
func TestE2E_HostDisconnectAfterStart_QuizContinues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	host, code := quizWith(ctx, t, 5*time.Second)
	h, err := testkit.Dial(ctx, env.WSURL(), host.Token, env.Origin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send("watch", map[string]any{"quizCode": code}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Expect(ctx, 0, "snapshot"); err != nil {
		t.Fatal(err)
	}
	p, _ := joinAs(ctx, t, participant(ctx, t), code, "Rina")
	if err := env.Start(ctx, host, code); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Expect(ctx, 0, "question"); err != nil {
		t.Fatal(err)
	}
	h.Close()
	if _, err := p.Expect(ctx, 0, "quiz_finished"); err != nil {
		t.Fatalf("quiz did not finish without its host: %v", err)
	}
	noViolations(t, p)
}
