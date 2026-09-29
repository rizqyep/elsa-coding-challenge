package scoring

import "sort"

// Scoring constants (FR-19).
const (
	BasePoints = 100 // for any correct answer
	MaxBonus   = 100 // for answering the instant the question opens
	MaxPoints  = BasePoints + MaxBonus
)

// Points scores one accepted answer (FR-19). Times are epoch milliseconds from the server clock.
// The bonus is floor(100 × remaining / window), remaining = deadline − receivedAt clamped to [0, window].
// deadline is the question's original deadline, so an early close never changes points.
// Integer arithmetic only: answer.lua computes the same value (checked by shared test vectors).
func Points(correct bool, openedAt, deadline, receivedAt int64) int {
	if !correct {
		return 0
	}
	window := deadline - openedAt
	if window <= 0 {
		return BasePoints
	}
	remaining := min(max(deadline-receivedAt, 0), window)
	return BasePoints + int(MaxBonus*remaining/window)
}

// RankOf returns the rank for a participant with the given number of strictly higher scores
// (FR-24: equal scores share a rank).
func RankOf(higher int) int { return higher + 1 }

// CompetitionRanks returns each score's rank, in input order: equal scores share a rank and
// the next rank skips (1, 2, 2, 4).
func CompetitionRanks(scores []int) []int {
	sorted := append([]int(nil), scores...)
	sort.Sort(sort.Reverse(sort.IntSlice(sorted)))
	rankOf := make(map[int]int, len(sorted))
	for i, s := range sorted {
		if _, seen := rankOf[s]; !seen {
			rankOf[s] = RankOf(i)
		}
	}
	ranks := make([]int, len(scores))
	for i, s := range scores {
		ranks[i] = rankOf[s]
	}
	return ranks
}
