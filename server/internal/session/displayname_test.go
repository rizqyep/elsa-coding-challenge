package session_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

// FR-15: trimmed, 1–20 characters (not bytes), letters, digits, spaces, and - _ . '
func TestNormalizeDisplayName(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"Rina", "Rina", true},
		{"  Rina  ", "Rina", true},   // trimmed
		{"Tomás", "Tomás", true},     // non-ASCII letters
		{"Aiko 愛子", "Aiko 愛子", true}, // CJK letters
		{"O'Brien-Smith_2.0", "O'Brien-Smith_2.0", true},
		{strings.Repeat("é", 20), strings.Repeat("é", 20), true}, // 20 characters, 40 bytes
		{strings.Repeat("é", 21), "", false},                     // 21 characters
		{"", "", false},
		{"   ", "", false},      // empty after trimming
		{"Rina\x00", "", false}, // control character
		{"Ri\nna", "", false},   // newline
		{"Rina 🎉", "", false},   // emoji is not a letter or digit
		{"<script>", "", false}, // punctuation outside the allowed set
	}
	for _, tc := range cases {
		got, err := session.NormalizeDisplayName(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("NormalizeDisplayName(%q) = %q, %v; want %q, ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
		if err != nil && !errors.Is(err, session.ErrInvalidDisplayName) {
			t.Errorf("NormalizeDisplayName(%q): error %v is not ErrInvalidDisplayName", tc.in, err)
		}
	}
}
