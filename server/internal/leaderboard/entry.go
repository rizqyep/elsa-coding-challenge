package leaderboard

import (
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
)

// TopN is how many entries live updates carry (FR-26).
const TopN = quiz.TopN

// Entry is one ranked participant.
type Entry struct {
	Rank          int
	ParticipantID quiz.ParticipantID
	DisplayName   string
	Score         int
}

// Rank sets competition ranks on entries ordered from the top of the leaderboard (FR-24).
func Rank(entries []Entry) {
	scores := make([]int, len(entries))
	for i, e := range entries {
		scores[i] = e.Score
	}
	for i, r := range scoring.CompetitionRanks(scores) {
		entries[i].Rank = r
	}
}
