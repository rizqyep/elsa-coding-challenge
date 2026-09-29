package testkit

import (
	"encoding/json"
	"os"
	"testing"
)

// The kit's own points formula agrees with the shared vectors, so it can check the server independently.
func TestExpectedPoints_Vectors(t *testing.T) {
	raw, err := os.ReadFile("../internal/scoring/testdata/points_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []struct {
			Name               string
			Correct            bool
			OpenedAt, Deadline int64
			ReceivedAt         int64
			Points             int
		}
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) < 10 {
		t.Fatalf("read %d cases; the vectors file changed shape", len(file.Cases))
	}
	for _, c := range file.Cases {
		if got := ExpectedPoints(c.Correct, c.OpenedAt, c.Deadline, c.ReceivedAt); got != c.Points {
			t.Errorf("%s: %d, want %d", c.Name, got, c.Points)
		}
	}
}
