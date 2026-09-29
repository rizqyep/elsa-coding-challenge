package session

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxDisplayNameLength is measured in characters, not bytes (FR-15).
const MaxDisplayNameLength = 20

// ErrInvalidDisplayName is returned for names that break FR-15.
var ErrInvalidDisplayName = errors.New("invalid display name")

// NormalizeDisplayName trims the name and checks it: 1–20 characters of letters, digits,
// spaces, and - _ . '
func NormalizeDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n == 0 || n > MaxDisplayNameLength {
		return "", fmt.Errorf("%w: must be 1–%d characters, got %d", ErrInvalidDisplayName, MaxDisplayNameLength, n)
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && !strings.ContainsRune("-_.'", r) {
			return "", fmt.Errorf("%w: %q is not allowed", ErrInvalidDisplayName, r)
		}
	}
	return s, nil
}
