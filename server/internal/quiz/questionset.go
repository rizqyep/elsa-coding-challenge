package quiz

import (
	"errors"
	"fmt"
)

// MaxQuestions is the most questions a set may have (TRD §3.2).
const MaxQuestions = 50

// ErrMalformedSet is returned for a question set that breaks TRD §3.2's shape rules.
var ErrMalformedSet = errors.New("malformed question set")

// ValidateSet checks a set before it's cached: 1–50 questions, 2–4 options each, unique IDs,
// non-empty text, and a correct option that belongs to its own question (TRD §7.8).
func ValidateSet(s QuestionSet) error {
	if n := len(s.Questions); n < 1 || n > MaxQuestions {
		return fmt.Errorf("%w: %d questions, want 1-%d", ErrMalformedSet, n, MaxQuestions)
	}
	questions, options := map[QuestionID]bool{}, map[OptionID]bool{}
	for _, q := range s.Questions {
		if q.ID == "" || q.Prompt == "" || questions[q.ID] {
			return fmt.Errorf("%w: question %q has an empty or duplicate id, or no prompt", ErrMalformedSet, q.ID)
		}
		questions[q.ID] = true
		if n := len(q.Options); n < 2 || n > 4 {
			return fmt.Errorf("%w: question %q has %d options, want 2-4", ErrMalformedSet, q.ID, n)
		}
		for _, o := range q.Options {
			if o.ID == "" || o.Text == "" || options[o.ID] {
				return fmt.Errorf("%w: question %q has an empty or duplicate option %q", ErrMalformedSet, q.ID, o.ID)
			}
			options[o.ID] = true
		}
		if !q.HasOption(q.CorrectOptionID) {
			return fmt.Errorf("%w: question %q: correct option %q is not one of its options", ErrMalformedSet, q.ID, q.CorrectOptionID)
		}
	}
	return nil
}

// Question finds a question by ID.
func (s *QuestionSet) Question(id QuestionID) (Question, bool) {
	for _, q := range s.Questions {
		if q.ID == id {
			return q, true
		}
	}
	return Question{}, false
}

// HasOption reports whether id is one of this question's options.
func (q Question) HasOption(id OptionID) bool {
	for _, o := range q.Options {
		if o.ID == id {
			return true
		}
	}
	return false
}

// IsCorrect reports whether id is the answer key.
func (q Question) IsCorrect(id OptionID) bool { return id != "" && id == q.CorrectOptionID }
