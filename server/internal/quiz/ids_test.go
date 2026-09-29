package quiz_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

func TestCodeAlphabet_ExcludesLookAlikes(t *testing.T) {
	if len(quiz.CodeAlphabet) != 31 {
		t.Fatalf("alphabet has %d symbols, want 31", len(quiz.CodeAlphabet))
	}
	for _, c := range "01OIL" {
		if strings.ContainsRune(quiz.CodeAlphabet, c) {
			t.Errorf("alphabet contains look-alike %q", c)
		}
	}
}

func TestNewCode_LengthAndAlphabet(t *testing.T) {
	for range 1000 {
		code, err := quiz.NewCode(nil) // nil: crypto/rand
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != quiz.CodeLength {
			t.Fatalf("code %q has length %d", code, len(code))
		}
		for _, c := range string(code) {
			if !strings.ContainsRune(quiz.CodeAlphabet, c) {
				t.Fatalf("code %q contains %q, outside the alphabet", code, c)
			}
		}
	}
}

// Rejection sampling, no modulo bias: bytes 0–247 (= 31·8) are accepted and 248–255 rejected.
// Six rounds of all 256 byte values give 6·248 = 1,488 accepted bytes = exactly 248 codes,
// so every symbol must appear exactly 6·8 = 48 times. (NewCode reads its source sequentially.)
func TestNewCode_RejectionSamplingHasNoModuloBias(t *testing.T) {
	round := make([]byte, 256)
	for i := range round {
		round[i] = byte(i)
	}
	src := bytes.NewReader(bytes.Repeat(round, 6))
	counts := map[rune]int{}
	codes := 0
	for {
		code, err := quiz.NewCode(src)
		if err != nil {
			break // source exhausted
		}
		codes++
		for _, c := range string(code) {
			counts[c]++
		}
	}
	if codes != 248 {
		t.Fatalf("got %d codes, want 248", codes)
	}
	for _, c := range quiz.CodeAlphabet {
		if counts[c] != 48 {
			t.Errorf("symbol %q appeared %d times, want 48", c, counts[c])
		}
	}
}

func TestNewCode_SkipsRejectedBytes(t *testing.T) {
	// 248–255 are rejected; 0 maps to the first symbol, 30 to the last.
	src := bytes.NewReader([]byte{255, 248, 0, 250, 30, 1, 2, 3, 4})
	code, err := quiz.NewCode(src)
	if err != nil {
		t.Fatal(err)
	}
	a := quiz.CodeAlphabet
	if want := quiz.Code(string([]byte{a[0], a[30], a[1], a[2], a[3], a[4]})); code != want {
		t.Errorf("got %q, want %q", code, want)
	}
}

func TestNewCode_SourceErrorIsReturned(t *testing.T) {
	if _, err := quiz.NewCode(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Error("expected an error from an exhausted source")
	}
}

func TestParseCode(t *testing.T) {
	cases := []struct {
		in      string
		want    quiz.Code
		wantErr bool
	}{
		{"K7Q2MX", "K7Q2MX", false},
		{"k7q2mx", "K7Q2MX", false},     // case-insensitive
		{"  K7Q2MX  ", "K7Q2MX", false}, // surrounding spaces ignored
		{"K7Q2M0", "", true},            // look-alike 0
		{"K7Q2MI", "", true},            // look-alike I
		{"K7Q2M", "", true},             // too short
		{"K7Q2MXA", "", true},           // too long
		{"", "", true},
		{"K7Q-MX", "", true},
	}
	for _, tc := range cases {
		got, err := quiz.ParseCode(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParseCode(%q) = %q, %v; want %q, error=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
		if err != nil && !errors.Is(err, quiz.ErrInvalidCode) {
			t.Errorf("ParseCode(%q) error %v is not ErrInvalidCode", tc.in, err)
		}
	}
}

func TestValidateTiming(t *testing.T) {
	const s = 1000 // ms
	cases := []struct {
		window, reveal int64
		ok             bool
	}{
		{15 * s, 5 * s, true},
		{5 * s, 2 * s, true},      // lower bounds
		{120 * s, 30 * s, true},   // upper bounds
		{5*s - 1, 5 * s, false},   // window below 5 s (FR-7)
		{120*s + 1, 5 * s, false}, // window above 120 s
		{15 * s, 2*s - 1, false},  // reveal below 2 s
		{15 * s, 30*s + 1, false}, // reveal above 30 s
	}
	for _, tc := range cases {
		err := quiz.ValidateTiming(tc.window, tc.reveal)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateTiming(%d, %d) = %v, want ok=%v", tc.window, tc.reveal, err, tc.ok)
		}
	}
}

func TestValidateRequestID(t *testing.T) {
	for _, tc := range []struct {
		id string
		ok bool
	}{
		{"r-1", true},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
		{"", false},
	} {
		if err := quiz.ValidateRequestID(tc.id); (err == nil) != tc.ok {
			t.Errorf("ValidateRequestID(%d chars) = %v, want ok=%v", len(tc.id), err, tc.ok)
		}
	}
}
