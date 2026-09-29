package quiz

// Stale means another worker already applied this transition, or the room is gone.
const Stale Outcome = "stale"

// TopN is how many entries room events carry (FR-26).
const TopN = 10

// TransitionResult is the outcome of the transition script.
type TransitionResult struct {
	Outcome      Outcome
	Status       Status
	StateVersion int64
}
