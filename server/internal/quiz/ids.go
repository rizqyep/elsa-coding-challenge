package quiz

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Identifiers (TRD §3.1).
type (
	// Code is the 6-character quiz code participants type to join (D16).
	Code string
	// ParticipantID is the token subject of a participant or host.
	ParticipantID string
	// QuestionSetID identifies a seeded question set.
	QuestionSetID string
	// QuestionID identifies a question within the seed data.
	QuestionID string
	// OptionID identifies an answer option.
	OptionID string
)

const (
	// CodeAlphabet has 31 symbols: digits and letters without the look-alikes 0, 1, O, I, L.
	CodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	// CodeLength is the number of symbols in a code (31^6 ≈ 887 million codes).
	CodeLength = 6

	// acceptBelow is the largest multiple of len(CodeAlphabet) that fits in a byte (31·8 = 248).
	// Bytes at or above it are rejected, so every symbol is equally likely.
	acceptBelow = 248
)

// ErrInvalidCode is returned for input that isn't a valid quiz code.
var ErrInvalidCode = errors.New("invalid quiz code")

// NewCode generates a random quiz code. A nil source means crypto/rand.
func NewCode(src io.Reader) (Code, error) {
	if src == nil {
		src = rand.Reader
	}
	var out [CodeLength]byte
	var b [1]byte
	for i := 0; i < CodeLength; {
		if _, err := io.ReadFull(src, b[:]); err != nil {
			return "", fmt.Errorf("read random byte: %w", err)
		}
		if b[0] >= acceptBelow {
			continue
		}
		out[i] = CodeAlphabet[int(b[0])%len(CodeAlphabet)]
		i++
	}
	return Code(out[:]), nil
}

// ParseCode normalises user input (trims spaces, upper-cases) and validates it.
func ParseCode(s string) (Code, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != CodeLength {
		return "", fmt.Errorf("%w: must be %d characters", ErrInvalidCode, CodeLength)
	}
	for _, c := range s {
		if !strings.ContainsRune(CodeAlphabet, c) {
			return "", fmt.Errorf("%w: %q is not allowed", ErrInvalidCode, c)
		}
	}
	return Code(s), nil
}

// Question window and reveal limits for per-quiz overrides, in milliseconds (FR-7).
const (
	MinWindowMs = 5_000
	MaxWindowMs = 120_000
	MinRevealMs = 2_000
	MaxRevealMs = 30_000
)

// ValidateTiming checks per-quiz question window and reveal durations.
func ValidateTiming(windowMs, revealMs int64) error {
	if windowMs < MinWindowMs || windowMs > MaxWindowMs {
		return fmt.Errorf("question window must be between %d and %d ms, got %d", MinWindowMs, MaxWindowMs, windowMs)
	}
	if revealMs < MinRevealMs || revealMs > MaxRevealMs {
		return fmt.Errorf("reveal must be between %d and %d ms, got %d", MinRevealMs, MaxRevealMs, revealMs)
	}
	return nil
}

// ValidateRequestID checks a client request ID (1–64 characters; FR-18).
func ValidateRequestID(id string) error {
	if id == "" || len(id) > 64 {
		return fmt.Errorf("request id must be 1–64 characters, got %d", len(id))
	}
	return nil
}
