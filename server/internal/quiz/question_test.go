package quiz_test

import (
	"reflect"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

func TestPublicQuestion_HasNoAnswerKeyField(t *testing.T) {
	typ := reflect.TypeOf(quiz.PublicQuestion{})
	for i := range typ.NumField() {
		if typ.Field(i).Name == "CorrectOptionID" {
			t.Fatal("PublicQuestion must not carry the correct option (FR-21)")
		}
	}
}

func TestQuestion_Public(t *testing.T) {
	q := quiz.Question{
		ID:              "syn-03",
		Prompt:          "Choose the synonym of 'rapid'",
		Options:         []quiz.Option{{ID: "syn-03-a", Text: "slow"}, {ID: "syn-03-b", Text: "quick"}},
		CorrectOptionID: "syn-03-b",
	}
	want := quiz.PublicQuestion{ID: q.ID, Prompt: q.Prompt, Options: q.Options}
	if got := q.Public(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
