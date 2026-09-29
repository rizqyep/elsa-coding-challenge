package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/gen"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/logging"
)

const maxBodyBytes = 64 << 10

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// requestID accepts a well-formed inbound X-Request-ID or creates one, and logs each request (NFR-28).
func (a *api) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID.MatchString(id) {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-ID", id)
		ctx := logging.WithRequest(r.Context(), id)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(ctx))
		a.cfg.Log.LogAttrs(ctx, slog.LevelInfo, "request", slog.String("method", r.Method), slog.String("path", r.URL.Path),
			slog.Int("status", rec.status), slog.Duration("duration", time.Since(start)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// recover turns a panic into a 500 for this request only (TRD §9.1).
func (a *api) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				a.cfg.Log.ErrorContext(r.Context(), "panic", "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				a.fail(w, r, errPanic)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

var errPanic = errors.New("panic")

// contract routes the request against openapi.yaml, authenticates it, then validates it (D13).
func (a *api) contract(router routers.Router, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, params, err := router.FindRoute(r)
		switch {
		case err != nil && pathExists(router, r):
			a.fail(w, r, &problem{status: http.StatusMethodNotAllowed, code: gen.ErrorCodeInvalidRequest, detail: "method not allowed"})
			return
		case err != nil:
			a.fail(w, r, errNotFound)
			return
		}
		op := route.Operation.OperationID
		if op == "CreateDevToken" && !a.cfg.DevTokens {
			a.fail(w, r, errNotFound)
			return
		}
		ctx := r.Context()
		if rule := access[op]; rule != public {
			claims, err := a.authenticate(r)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			if rule == hostOnly && claims.Role != auth.RoleHost {
				a.fail(w, r, &problem{status: http.StatusForbidden, code: gen.ErrorCodeForbidden, detail: "requires the host role"})
				return
			}
			ctx = auth.WithClaims(logging.WithParticipant(ctx, claims.ParticipantID), claims)
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if err := openapi3filter.ValidateRequest(ctx, &openapi3filter.RequestValidationInput{
			Request: r, PathParams: params, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		}); err != nil {
			a.fail(w, r, invalid(err))
			return
		}
		ctx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *api) authenticate(r *http.Request) (auth.Claims, error) {
	tok, ok := auth.FromBearer(r)
	if !ok {
		return auth.Claims{}, &problem{status: http.StatusUnauthorized, code: gen.ErrorCodeUnauthorized, detail: "missing bearer token"}
	}
	claims, err := a.cfg.Tokens.Verify(tok)
	if err != nil {
		return auth.Claims{}, &problem{status: http.StatusUnauthorized, code: gen.ErrorCodeUnauthorized, detail: "invalid or expired token"}
	}
	return claims, nil
}

// pathExists reports whether the path is served with some other method (for 405 vs 404).
func pathExists(router routers.Router, r *http.Request) bool {
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if m == r.Method {
			continue
		}
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, _, err := router.FindRoute(probe); err == nil {
			return true
		}
	}
	return false
}
