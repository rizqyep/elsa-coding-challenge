package auth

import (
	"context"
	"net/http"
	"strings"
)

// FromBearer reads the token from the Authorization header (REST).
func FromBearer(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// FromQuery reads the token from the `token` query parameter (WebSocket handshake, D15).
func FromQuery(r *http.Request) (string, bool) {
	token := r.URL.Query().Get("token")
	return token, token != ""
}

type claimsKey struct{}

// WithClaims stores the verified caller in the context.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClaimsFrom returns the verified caller, if any.
func ClaimsFrom(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	return c, ok
}
