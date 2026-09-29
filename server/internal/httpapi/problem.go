package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/gen"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// problem is an error with its HTTP mapping (TRD §9.6).
type problem struct {
	status int
	code   gen.ErrorCode
	detail string
}

func (p *problem) Error() string { return fmt.Sprintf("%s: %s", p.code, p.detail) }

var errNotFound = &problem{status: http.StatusNotFound, code: gen.ErrorCodeNotFound}

func invalid(err error) *problem {
	return &problem{status: http.StatusBadRequest, code: gen.ErrorCodeInvalidRequest, detail: describe(err)}
}

// classify maps an error to its response: business outcomes to 4xx, infrastructure to 503, anything else to 500.
func classify(err error) *problem {
	var p *problem
	switch {
	case errors.As(err, &p):
		return p
	case errors.Is(err, quiz.ErrUnknownQuiz), errors.Is(err, quiz.ErrQuizExpired):
		return &problem{status: http.StatusNotFound, code: gen.ErrorCodeUnknownQuiz}
	case errors.Is(err, quiz.ErrQuestionSetNotFound):
		return &problem{status: http.StatusNotFound, code: gen.ErrorCodeQuestionSetNotFound}
	case errors.Is(err, quiz.ErrNotHost):
		return &problem{status: http.StatusForbidden, code: gen.ErrorCodeNotHost}
	case errors.Is(err, quiz.ErrNotInLobby):
		return &problem{status: http.StatusConflict, code: gen.ErrorCodeNotInLobby}
	case errors.Is(err, quiz.ErrNoParticipants):
		return &problem{status: http.StatusConflict, code: gen.ErrorCodeNoParticipants}
	case errors.Is(err, quiz.ErrInvalidSettings):
		return &problem{status: http.StatusBadRequest, code: gen.ErrorCodeInvalidRequest, detail: err.Error()}
	case errors.Is(err, quiz.ErrNoFreeCode), errors.Is(err, context.Canceled), retry.Transient(err):
		return &problem{status: http.StatusServiceUnavailable, code: gen.ErrorCodeUnavailable}
	}
	return &problem{status: http.StatusInternalServerError, code: gen.ErrorCodeInternal}
}

// fail writes the error as problem+json, counts it, and logs it once, here (TRD §9.6).
func (a *api) fail(w http.ResponseWriter, r *http.Request, err error) {
	p := classify(err)
	a.errors.WithLabelValues("api", string(p.code)).Inc()
	level := slog.LevelDebug // business outcomes are not errors
	switch p.status {
	case http.StatusInternalServerError:
		level = slog.LevelError
	case http.StatusServiceUnavailable:
		level = slog.LevelWarn
	}
	a.cfg.Log.Log(r.Context(), level, "request failed", "status", p.status, "code", string(p.code), "error", err)

	body := gen.Problem{Type: "about:blank", Title: http.StatusText(p.status), Status: p.status, Code: p.code}
	if p.detail != "" && p.status < http.StatusInternalServerError {
		body.Detail = &p.detail
	}
	if p.status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.status)
	_ = json.NewEncoder(w).Encode(body)
}

// describe turns a validation error into a short, safe message: where, and why.
func describe(err error) string {
	var re *openapi3filter.RequestError
	if errors.As(err, &re) && re.Parameter != nil {
		return fmt.Sprintf("parameter %q: %s", re.Parameter.Name, reason(re.Err, re.Reason))
	}
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		if ptr := se.JSONPointer(); len(ptr) > 0 {
			return "/" + strings.Join(ptr, "/") + ": " + tidy(se.Reason)
		}
		return tidy(se.Reason)
	}
	return tidy(err.Error())
}

func reason(err error, fallback string) string {
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		return tidy(se.Reason)
	}
	if fallback != "" {
		return fallback
	}
	if err != nil {
		return tidy(err.Error())
	}
	return "invalid"
}

// tidy drops the validator's schema URL, keeping "at <path>: <reason>".
func tidy(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if rest, ok := strings.CutPrefix(line, "- at '"); ok {
			if path, why, ok := strings.Cut(rest, "': "); ok {
				if path == "" {
					return why
				}
				return path + ": " + why
			}
		}
	}
	return firstLine(s)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
