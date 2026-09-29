package auth_test

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
)

var (
	key   = []byte("0123456789abcdef0123456789abcdef")
	other = []byte("fedcba9876543210fedcba9876543210")
	t0    = time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
)

func tokens(now time.Time) *auth.Tokens {
	return &auth.Tokens{Key: key, TTL: 15 * time.Minute, Now: func() time.Time { return now }}
}

func TestIssueVerify_RoundTrip(t *testing.T) {
	for _, role := range []auth.Role{auth.RoleParticipant, auth.RoleHost} {
		tok, issued, err := tokens(t0).Issue("u_1", role)
		if err != nil {
			t.Fatal(err)
		}
		if want := t0.Add(15 * time.Minute); !issued.ExpiresAt.Equal(want) {
			t.Errorf("expiresAt = %v, want %v", issued.ExpiresAt, want)
		}
		got, err := tokens(t0.Add(time.Minute)).Verify(tok)
		if err != nil {
			t.Fatalf("%s: verify: %v", role, err)
		}
		if got != issued {
			t.Errorf("%s: verified %+v, issued %+v", role, got, issued)
		}
	}
}

func TestVerify_Rejects(t *testing.T) {
	valid, _, _ := tokens(t0).Issue("u_1", auth.RoleHost)
	otherKey, _, _ := (&auth.Tokens{Key: other, TTL: time.Minute, Now: func() time.Time { return t0 }}).Issue("u_1", auth.RoleHost)
	parts := strings.Split(valid, ".")

	cases := map[string]struct {
		token string
		at    time.Time
	}{
		"expired":                  {valid, t0.Add(15 * time.Minute)},
		"expired long ago":         {valid, t0.Add(48 * time.Hour)},
		"signed with another key":  {otherKey, t0},
		"tampered payload":         {parts[0] + "." + claims(`{"sub":"u_1","role":"host","exp":9999999999}`) + "." + parts[2], t0},
		"alg none":                 {header(`{"alg":"none","typ":"JWT"}`) + "." + parts[1] + ".", t0},
		"different HMAC algorithm": {sign(t, jwt.SigningMethodHS512, jwt.MapClaims{"sub": "u_1", "role": "host", "exp": t0.Add(time.Hour).Unix()}), t0},
		"missing exp":              {sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u_1", "role": "host"}), t0},
		"missing sub":              {sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"role": "host", "exp": t0.Add(time.Hour).Unix()}), t0},
		"unknown role":             {sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u_1", "role": "admin", "exp": t0.Add(time.Hour).Unix()}), t0},
		"missing role":             {sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u_1", "exp": t0.Add(time.Hour).Unix()}), t0},
		"empty":                    {"", t0},
		"garbage":                  {"not-a-token", t0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tokens(tc.at).Verify(tc.token); !errors.Is(err, auth.ErrInvalidToken) {
				t.Errorf("got %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestIssue_RejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		id   string
		role auth.Role
	}{
		"empty id":     {"", auth.RoleHost},
		"unknown role": {"u_1", "admin"},
		"id too long":  {strings.Repeat("a", 65), auth.RoleHost},
	}
	for name, tc := range cases {
		if _, _, err := tokens(t0).Issue(tc.id, tc.role); err == nil {
			t.Errorf("%s: issued a token", name)
		}
	}
}

func TestNewParticipantID(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id, err := auth.NewParticipantID()
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^u_[0-9a-f]{16}$`).MatchString(id) {
			t.Fatalf("id %q has the wrong shape", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func sign(t *testing.T, m jwt.SigningMethod, c jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(m, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func header(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func claims(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
