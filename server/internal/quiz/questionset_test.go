package quiz_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// set builds a well-formed set with n questions of 4 options; option b is correct.
func set(id string, n int) quiz.QuestionSet {
	s := quiz.QuestionSet{ID: quiz.QuestionSetID(id), Title: "T-" + id}
	for i := range n {
		qid := quiz.QuestionID(fmt.Sprintf("%s-%02d", id, i))
		q := quiz.Question{ID: qid, Prompt: "prompt", CorrectOptionID: quiz.OptionID(string(qid) + "-b")}
		for _, o := range []string{"a", "b", "c", "d"} {
			q.Options = append(q.Options, quiz.Option{ID: quiz.OptionID(string(qid) + "-" + o), Text: o})
		}
		s.Questions = append(s.Questions, q)
	}
	return s
}

func TestValidateSet(t *testing.T) {
	if err := quiz.ValidateSet(set("ok", 3)); err != nil {
		t.Fatalf("valid set refused: %v", err)
	}
	if err := quiz.ValidateSet(set("max", 50)); err != nil {
		t.Fatalf("50 questions refused: %v", err)
	}
	two := set("two", 1)
	two.Questions[0].Options = two.Questions[0].Options[:2]
	if err := quiz.ValidateSet(two); err != nil {
		t.Fatalf("2 options refused: %v", err)
	}

	cases := map[string]func(*quiz.QuestionSet){
		"no questions": func(s *quiz.QuestionSet) { s.Questions = nil },
		"51 questions": func(s *quiz.QuestionSet) { *s = set("big", 51) },
		"one option":   func(s *quiz.QuestionSet) { s.Questions[0].Options = s.Questions[0].Options[:1] },
		"five options": func(s *quiz.QuestionSet) {
			s.Questions[0].Options = append(s.Questions[0].Options, quiz.Option{ID: "x", Text: "x"})
		},
		"correct option elsewhere": func(s *quiz.QuestionSet) { s.Questions[0].CorrectOptionID = s.Questions[1].Options[1].ID },
		"no correct option":        func(s *quiz.QuestionSet) { s.Questions[1].CorrectOptionID = "" },
		"duplicate option id":      func(s *quiz.QuestionSet) { s.Questions[0].Options[2].ID = s.Questions[0].Options[1].ID },
		"option id reused across":  func(s *quiz.QuestionSet) { s.Questions[1].Options[0].ID = s.Questions[0].Options[0].ID },
		"duplicate question id":    func(s *quiz.QuestionSet) { s.Questions[1].ID = s.Questions[0].ID },
		"empty prompt":             func(s *quiz.QuestionSet) { s.Questions[2].Prompt = "" },
		"empty option text":        func(s *quiz.QuestionSet) { s.Questions[2].Options[3].Text = "" },
		"empty question id":        func(s *quiz.QuestionSet) { s.Questions[0].ID = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := set("bad", 3)
			mutate(&s)
			if err := quiz.ValidateSet(s); !errors.Is(err, quiz.ErrMalformedSet) {
				t.Errorf("got %v, want ErrMalformedSet", err)
			}
		})
	}
}

// The answer path looks questions and options up by ID (TRD §3.4).
func TestQuestionSet_Lookups(t *testing.T) {
	s := set("s", 3)
	q, ok := s.Question("s-01")
	if !ok || q.ID != "s-01" {
		t.Fatalf("Question(s-01) = %+v, %v", q, ok)
	}
	if _, ok := s.Question("s-99"); ok {
		t.Error("found a question that isn't in the set")
	}
	if !q.HasOption("s-01-c") || q.HasOption("s-00-c") || q.HasOption("") {
		t.Error("HasOption must accept only this question's options")
	}
	if !q.IsCorrect("s-01-b") || q.IsCorrect("s-01-a") {
		t.Error("IsCorrect must match only the answer key")
	}
}
