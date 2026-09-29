package testkit

// ExpectedPoints is the scoring rule written out again, independently of internal/scoring (FR-19):
// correct answers get 100 plus a speed bonus of floor(100 × time left / window), wrong ones 0.
func ExpectedPoints(correct bool, openedAt, deadline, receivedAt int64) int {
	if !correct {
		return 0
	}
	window := deadline - openedAt
	if window <= 0 {
		return 100
	}
	left := min(max(deadline-receivedAt, 0), window)
	return 100 + int(100*left/window)
}
