package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/mock/gomock"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/gen"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/mocks"
)

var signingKey = []byte("0123456789abcdef0123456789abcdef")

type harness struct {
	t       *testing.T
	quizzes *mocks.MockQuizzes
	boards  *mocks.MockLeaderboards
	tokens  *auth.Tokens
	ready   error
	handler http.Handler
	router  routers.Router
}

type option func(*httpapi.Config)

func withDevTokens(on bool) option { return func(c *httpapi.Config) { c.DevTokens = on } }

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	h := &harness{t: t, quizzes: mocks.NewMockQuizzes(ctrl), boards: mocks.NewMockLeaderboards(ctrl)}
	h.tokens = &auth.Tokens{Key: signingKey, TTL: 15 * time.Minute}
	cfg := httpapi.Config{
		Quizzes: h.quizzes, Leaderboards: h.boards, Tokens: h.tokens, DevTokens: true,
		Ready: func(context.Context) error { return h.ready },
		Log:   slog.New(slog.DiscardHandler), Metrics: prometheus.NewRegistry(), RequestTimeout: 5 * time.Second,
	}
	for _, o := range opts {
		o(&cfg)
	}
	handler, err := httpapi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.handler = handler
	doc, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	doc.Servers = nil
	if h.router, err = legacy.NewRouter(doc); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) token(id string, role auth.Role) string {
	h.t.Helper()
	tok, _, err := h.tokens.Issue(id, role)
	if err != nil {
		h.t.Fatal(err)
	}
	return tok
}

func (h *harness) host() string        { return h.token("host_1", auth.RoleHost) }
func (h *harness) participant() string { return h.token("u_1", auth.RoleParticipant) }

type response struct {
	*httptest.ResponseRecorder
	body []byte
}

func (r response) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r response) problem(t *testing.T) gen.Problem {
	t.Helper()
	var p gen.Problem
	r.json(t, &p)
	return p
}

// do sends a request and checks the response against openapi.yaml: status, headers, and body.
func (h *harness) do(method, path, token string, body any) response {
	h.t.Helper()
	var reqBody io.Reader
	var raw []byte
	switch b := body.(type) {
	case nil:
	case string:
		raw = []byte(b)
	default:
		var err error
		if raw, err = json.Marshal(b); err != nil {
			h.t.Fatal(err)
		}
	}
	if raw != nil {
		reqBody = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, reqBody)
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	res := response{ResponseRecorder: rec, body: rec.Body.Bytes()}

	if rec.Header().Get("X-Request-ID") == "" {
		h.t.Errorf("%s %s: no X-Request-ID", method, path)
	}
	if rec.Code >= 400 && strings.HasPrefix(path, "/api/") && !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json") {
		h.t.Errorf("%s %s: error %d with content type %q", method, path, rec.Code, rec.Header().Get("Content-Type"))
	}
	h.validate(method, path, raw, res)
	return res
}

func (h *harness) validate(method, path string, reqBody []byte, res response) {
	h.t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, bytes.NewReader(reqBody))
	route, params, err := h.router.FindRoute(req)
	if err != nil {
		return // not an API route; nothing to validate against
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
		Status:                 res.Code,
		Header:                 res.Header(),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	in.SetBodyBytes(res.body)
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		h.t.Errorf("%s %s → %d violates openapi.yaml: %v\nbody: %s", method, path, res.Code, err, res.body)
	}
}

func expectStatus(t *testing.T, r response, status int, code gen.ErrorCode) {
	t.Helper()
	if r.Code != status {
		t.Fatalf("status %d, want %d; body %s", r.Code, status, r.body)
	}
	if code != "" {
		if p := r.problem(t); p.Code != code {
			t.Errorf("code %q, want %q", p.Code, code)
		}
	}
}

func newRequest(method, path string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), method, path, nil)
}

func serve(h *harness, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func grep(s, substr string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, substr) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
