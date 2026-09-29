package history_test

import (
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
)

func TestParseJob(t *testing.T) {
	cases := []struct {
		id      string
		want    history.Job
		wantErr bool
	}{
		{"q|K7Q2MX|dq-01", history.Job{ID: "q|K7Q2MX|dq-01", Kind: history.FlushAnswers, Code: "K7Q2MX", QuestionID: "dq-01"}, false},
		{"final|K7Q2MX", history.Job{ID: "final|K7Q2MX", Kind: history.Finalize, Code: "K7Q2MX"}, false},
		{"q|K7Q2MX", history.Job{}, true},
		{"final|K7Q2MX|extra", history.Job{}, true},
		{"other|K7Q2MX", history.Job{}, true},
		{"", history.Job{}, true},
	}
	for _, tc := range cases {
		got, err := history.ParseJob(tc.id)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParseJob(%q) = %+v, %v; want %+v, error=%v", tc.id, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestParseStoredAnswer(t *testing.T) {
	got, err := history.ParseStoredAnswer("u_1", "dq-01-b|1|186|1790000002100")
	want := history.StoredAnswer{ParticipantID: "u_1", OptionID: "dq-01-b", Correct: true, Points: 186, ReceivedAt: 1790000002100}
	if err != nil || got != want {
		t.Errorf("got %+v, %v; want %+v", got, err, want)
	}
	for _, bad := range []string{"", "a|1|2", "a|1|x|4", "a|yes|2|4"} {
		if _, err := history.ParseStoredAnswer("u_1", bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}
