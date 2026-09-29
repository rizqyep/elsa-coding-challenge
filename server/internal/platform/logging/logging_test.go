package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/logging"
)

func TestLogger_AddsContextFields(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, slog.LevelInfo, "ws")
	ctx := logging.WithQuiz(context.Background(), "K7Q2MX")
	ctx = logging.WithParticipant(ctx, "u_8f3a2c")

	log.InfoContext(ctx, "answer accepted", "points", 186)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{"service": "ws", "quiz_id": "K7Q2MX", "participant_id": "u_8f3a2c", "msg": "answer accepted", "points": 186.0} {
		if rec[k] != want {
			t.Errorf("%s = %v, want %v", k, rec[k], want)
		}
	}
}

func TestWith_DoesNotLeakIntoParentContext(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, slog.LevelInfo, "api")
	parent := logging.WithRequest(context.Background(), "r-1")
	_ = logging.WithQuiz(parent, "K7Q2MX")

	log.InfoContext(parent, "x")
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec["quiz_id"]; ok {
		t.Error("child context field leaked into the parent's records")
	}
}
