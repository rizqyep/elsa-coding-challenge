package auth_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
)

func TestFromBearer(t *testing.T) {
	cases := map[string]struct {
		header string
		want   string
		ok     bool
	}{
		"bearer":            {"Bearer abc.def.ghi", "abc.def.ghi", true},
		"scheme any case":   {"bearer abc", "abc", true},
		"missing":           {"", "", false},
		"basic auth":        {"Basic dXNlcjpwYXNz", "", false},
		"scheme only":       {"Bearer", "", false},
		"scheme and spaces": {"Bearer   ", "", false},
	}
	for name, tc := range cases {
		r := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
		if tc.header != "" {
			r.Header.Set("Authorization", tc.header)
		}
		got, ok := auth.FromBearer(r)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", name, got, ok, tc.want, tc.ok)
		}
	}
}

// D15: the WebSocket handshake carries the token in the query string.
func TestFromQuery(t *testing.T) {
	cases := map[string]struct {
		url  string
		want string
		ok   bool
	}{
		"present":     {"/ws?token=abc.def.ghi", "abc.def.ghi", true},
		"missing":     {"/ws", "", false},
		"empty":       {"/ws?token=", "", false},
		"other param": {"/ws?tok=abc", "", false},
	}
	for name, tc := range cases {
		got, ok := auth.FromQuery(httptest.NewRequestWithContext(context.Background(), "GET", tc.url, nil))
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestClaimsContext(t *testing.T) {
	if _, ok := auth.ClaimsFrom(context.Background()); ok {
		t.Error("claims found in an empty context")
	}
	c := auth.Claims{ParticipantID: "u_1", Role: auth.RoleHost}
	got, ok := auth.ClaimsFrom(auth.WithClaims(context.Background(), c))
	if !ok || got != c {
		t.Errorf("got (%+v, %v), want (%+v, true)", got, ok, c)
	}
}
