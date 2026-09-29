package scoring

import "sort"

// Scoring constants (FR-19).
const (
	BasePoints = 100
	MaxBonus   = 100
	MaxPoints  = BasePoints + MaxBonus
)

// Points scores an accepted answer (FR-19, TRD §3.4). Mirrored by answer.lua (testdata/points_cases.json).
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

// RankOf is the rank of a score with the given number of strictly higher scores (FR-24).
func RankOf(higher int) int { return higher + 1 }

// CompetitionRanks returns each score's rank in input order, e.g. 1, 2, 2, 4 (FR-24).
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
