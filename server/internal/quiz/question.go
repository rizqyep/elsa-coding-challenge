package quiz

// QuestionSet is an ordered list of questions (TRD §3.2).
type QuestionSet struct {
	ID        QuestionSetID
	Title     string
	Questions []Question
}

// Question includes its answer key; convert with Public before it leaves the server (FR-21).
type Question struct {
	ID              QuestionID
	Prompt          string
	Options         []Option
	CorrectOptionID OptionID
}

// Option is one answer choice.
type Option struct {
	ID   OptionID
	Text string
}

// PublicQuestion is a question without its answer key.
type PublicQuestion struct {
	ID      QuestionID
	Prompt  string
	Options []Option
}

// Public drops the answer key.
func (q Question) Public() PublicQuestion {
	return PublicQuestion{ID: q.ID, Prompt: q.Prompt, Options: q.Options}
}
