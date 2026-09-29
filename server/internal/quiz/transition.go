package quiz

// Stale means the room no longer exists.
const Stale Outcome = "stale"

// TopN is how many entries room events carry (FR-26).
const TopN = 10

// TransitionResult is the outcome of the transition script.
type TransitionResult struct {
	Outcome      Outcome
	Status       Status
	StateVersion int64
}
