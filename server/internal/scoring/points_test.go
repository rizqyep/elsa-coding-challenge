package scoring_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
)

type pointsCase struct {
	Name       string `json:"name"`
	Correct    bool   `json:"correct"`
	OpenedAt   int64  `json:"openedAt"`
	Deadline   int64  `json:"deadline"`
	ReceivedAt int64  `json:"receivedAt"`
	Points     int    `json:"points"`
}

func loadPointsCases(t *testing.T) []pointsCase {
	t.Helper()
	b, err := os.ReadFile("testdata/points_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []pointsCase `json:"cases"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("no cases")
	}
	return doc.Cases
}

func TestPoints_SharedVectors(t *testing.T) {
	for _, c := range loadPointsCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			if got := scoring.Points(c.Correct, c.OpenedAt, c.Deadline, c.ReceivedAt); got != c.Points {
				t.Errorf("Points = %d, want %d", got, c.Points)
			}
		})
	}
}

func TestPoints_StaysWithinRange(t *testing.T) {
	const opened, window = int64(1_000_000), int64(15_000)
	for received := opened - 100; received <= opened+window+100; received += 7 {
		p := scoring.Points(true, opened, opened+window, received)
		if p < scoring.BasePoints || p > scoring.MaxPoints {
			t.Fatalf("received at +%d ms: %d points, outside [%d, %d]", received-opened, p, scoring.BasePoints, scoring.MaxPoints)
		}
		if w := scoring.Points(false, opened, opened+window, received); w != 0 {
			t.Fatalf("wrong answer scored %d", w)
		}
	}
}

func TestPoints_NeverIncreasesWithLaterAnswers(t *testing.T) {
	const opened, window = int64(0), int64(15_000)
	prev := scoring.Points(true, opened, opened+window, opened)
	for received := opened + 1; received <= opened+window; received++ {
		p := scoring.Points(true, opened, opened+window, received)
		if p > prev {
			t.Fatalf("answering later at +%d ms scored more (%d > %d)", received, p, prev)
		}
		prev = p
	}
}

func TestPoints_ZeroLengthWindowGivesNoBonus(t *testing.T) {
	// Guard against division by zero; can't happen with validated timings.
	if got := scoring.Points(true, 5, 5, 5); got != scoring.BasePoints {
		t.Errorf("got %d, want %d", got, scoring.BasePoints)
	}
}

// FR-24: equal scores share a rank; the next rank skips (1, 2, 2, 4).
func TestCompetitionRanks(t *testing.T) {
	cases := []struct {
		name   string
		scores []int
		want   []int
	}{
		{"distinct", []int{574, 551, 300}, []int{1, 2, 3}},
		{"tie in the middle", []int{574, 551, 551, 300}, []int{1, 2, 2, 4}},
		{"tie at the top", []int{600, 600, 100}, []int{1, 1, 3}},
		{"everyone on zero", []int{0, 0, 0}, []int{1, 1, 1}},
		{"single participant", []int{120}, []int{1}},
		{"unsorted input keeps positions", []int{300, 574, 551, 551}, []int{4, 1, 2, 2}},
		{"empty", []int{}, []int{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scoring.CompetitionRanks(tc.scores)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestRankOf(t *testing.T) {
	// rank = 1 + number of participants with a strictly higher score (what the gateway
	// computes from ZCOUNT at question close).
	if got := scoring.RankOf(0); got != 1 {
		t.Errorf("RankOf(0) = %d, want 1", got)
	}
	if got := scoring.RankOf(3); got != 4 {
		t.Errorf("RankOf(3) = %d, want 4", got)
	}
}
