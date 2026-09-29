package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/gen"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var (
	errRedisDown = fmt.Errorf("start: %w", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
	created      = time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	expires      = created.Add(30 * time.Minute)
	lobby        = quiz.Summary{Code: "K7Q2MX", QuestionSetID: "demo-quick", Status: quiz.StatusLobby, QuestionCount: 3,
		WindowMs: 10_000, RevealMs: 3_000, CreatedAt: created, LobbyExpiresAt: &expires}
)

// --- dev tokens (task-15) ---

func TestDevTokens_Issue(t *testing.T) {
	h := newHarness(t)
	r := h.do("POST", "/api/v1/dev/tokens", "", map[string]any{"role": "participant"})
	expectStatus(t, r, http.StatusCreated, "")
	var got gen.DevToken
	r.json(t, &got)
	if !strings.HasPrefix(got.ParticipantId, "u_") || got.Role != gen.Participant {
		t.Errorf("token = %+v; want a generated participant id", got)
	}
	claims, err := h.tokens.Verify(got.Token)
	if err != nil || claims.ParticipantID != got.ParticipantId || claims.Role != auth.RoleParticipant {
		t.Errorf("issued token verifies as %+v, %v", claims, err)
	}

	r = h.do("POST", "/api/v1/dev/tokens", "", map[string]any{"role": "host", "participantId": "host_demo"})
	expectStatus(t, r, http.StatusCreated, "")
	r.json(t, &got)
	if got.ParticipantId != "host_demo" || got.Role != gen.Host {
		t.Errorf("token = %+v; want host_demo as host", got)
	}
}

func TestDevTokens_RejectsBadBodies(t *testing.T) {
	h := newHarness(t)
	for name, body := range map[string]any{
		"unknown role":     map[string]any{"role": "admin"},
		"missing role":     map[string]any{},
		"unknown field":    map[string]any{"role": "host", "isAdmin": true},
		"empty id":         map[string]any{"role": "host", "participantId": ""},
		"id too long":      map[string]any{"role": "host", "participantId": strings.Repeat("a", 65)},
		"malformed json":   `{"role":`,
		"not a json value": `hello`,
	} {
		t.Run(name, func(t *testing.T) {
			expectStatus(t, h.do("POST", "/api/v1/dev/tokens", "", body), http.StatusBadRequest, gen.ErrorCodeInvalidRequest)
		})
	}
}

func TestDevTokens_Disabled(t *testing.T) {
	h := newHarness(t, withDevTokens(false))
	expectStatus(t, h.do("POST", "/api/v1/dev/tokens", "", map[string]any{"role": "host"}), http.StatusNotFound, gen.ErrorCodeNotFound)
	expectStatus(t, h.do("POST", "/api/v1/dev/tokens", "", map[string]any{"role": "admin"}), http.StatusNotFound, gen.ErrorCodeNotFound)
}

// --- authentication and roles ---

func TestAuth(t *testing.T) {
	h := newHarness(t)
	expired, _, _ := (&auth.Tokens{Key: signingKey, TTL: time.Minute, Now: func() time.Time { return time.Now().Add(-time.Hour) }}).Issue("host_1", auth.RoleHost)
	for name, tok := range map[string]string{"missing": "", "garbage": "abc", "expired": expired} {
		t.Run("401 "+name, func(t *testing.T) {
			expectStatus(t, h.do("GET", "/api/v1/quizzes/K7Q2MX", tok, nil), http.StatusUnauthorized, gen.ErrorCodeUnauthorized)
		})
	}
	hostOnly := []struct{ method, path string }{
		{"GET", "/api/v1/question-sets"},
		{"POST", "/api/v1/quizzes"},
		{"POST", "/api/v1/quizzes/K7Q2MX/start"},
	}
	for _, op := range hostOnly {
		t.Run("403 participant "+op.method+" "+op.path, func(t *testing.T) {
			var body any
			if op.path == "/api/v1/quizzes" {
				body = map[string]any{"questionSetId": "demo-quick"}
			}
			expectStatus(t, h.do(op.method, op.path, h.participant(), body), http.StatusForbidden, gen.ErrorCodeForbidden)
		})
	}
}

// Auth runs before validation, so an anonymous caller learns nothing about the request shape.
func TestAuth_BeforeValidation(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.do("POST", "/api/v1/quizzes", "", map[string]any{"bogus": 1}), http.StatusUnauthorized, gen.ErrorCodeUnauthorized)
}

// --- question sets and quizzes ---

func TestListQuestionSets(t *testing.T) {
	h := newHarness(t)
	h.quizzes.EXPECT().QuestionSets(gomock.Any()).Return([]quiz.QuestionSetSummary{{ID: "demo-quick", Title: "Quick demo", QuestionCount: 3}}, nil)
	r := h.do("GET", "/api/v1/question-sets", h.host(), nil)
	expectStatus(t, r, http.StatusOK, "")
	if !strings.Contains(r.Body.String(), `"id":"demo-quick"`) {
		t.Errorf("body %s", r.body)
	}
	h.quizzes.EXPECT().QuestionSets(gomock.Any()).Return(nil, nil)
	if r := h.do("GET", "/api/v1/question-sets", h.host(), nil); !strings.Contains(r.Body.String(), `"items":[]`) {
		t.Errorf("empty list encoded as %s; want []", r.body)
	}
}

func TestCreateQuiz(t *testing.T) {
	h := newHarness(t)
	h.quizzes.EXPECT().Create(gomock.Any(), quiz.CreateInput{
		HostID: "host_1", QuestionSetID: "demo-quick", Window: 10 * time.Second, Reveal: 3 * time.Second,
	}).Return(lobby, nil)
	r := h.do("POST", "/api/v1/quizzes", h.host(), map[string]any{"questionSetId": "demo-quick", "questionWindowSeconds": 10, "revealSeconds": 3})
	expectStatus(t, r, http.StatusCreated, "")
	if loc := r.Header().Get("Location"); loc != "/api/v1/quizzes/K7Q2MX" {
		t.Errorf("Location %q", loc)
	}
	var got gen.Quiz
	r.json(t, &got)
	if got.Code != "K7Q2MX" || got.Status != gen.Lobby || got.QuestionWindowSeconds != 10 || got.RevealSeconds != 3 ||
		got.LobbyExpiresAt == nil || !got.LobbyExpiresAt.Equal(expires) || !got.CreatedAt.Equal(created) {
		t.Errorf("quiz = %+v", got)
	}

	h.quizzes.EXPECT().Create(gomock.Any(), quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick"}).Return(lobby, nil)
	expectStatus(t, h.do("POST", "/api/v1/quizzes", h.host(), map[string]any{"questionSetId": "demo-quick"}), http.StatusCreated, "")
}

func TestCreateQuiz_Errors(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		code   gen.ErrorCode
	}{
		"unknown set":      {quiz.ErrQuestionSetNotFound, http.StatusNotFound, gen.ErrorCodeQuestionSetNotFound},
		"no free code":     {quiz.ErrNoFreeCode, http.StatusServiceUnavailable, gen.ErrorCodeUnavailable},
		"redis down":       {errRedisDown, http.StatusServiceUnavailable, gen.ErrorCodeUnavailable},
		"postgres timeout": {fmt.Errorf("insert quiz: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, gen.ErrorCodeUnavailable},
		"invalid settings": {fmt.Errorf("%w: x", quiz.ErrInvalidSettings), http.StatusBadRequest, gen.ErrorCodeInvalidRequest},
		"unexpected (bug)": {errors.New("create_room: unexpected reply [boom]"), http.StatusInternalServerError, gen.ErrorCodeInternal},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.quizzes.EXPECT().Create(gomock.Any(), gomock.Any()).Return(quiz.Summary{}, tc.err)
			r := h.do("POST", "/api/v1/quizzes", h.host(), map[string]any{"questionSetId": "demo-quick"})
			expectStatus(t, r, tc.status, tc.code)
			if tc.status == http.StatusServiceUnavailable && r.Header().Get("Retry-After") == "" {
				t.Error("503 without Retry-After")
			}
			if p := r.problem(t); tc.status == http.StatusInternalServerError && p.Detail != nil {
				t.Errorf("500 leaks detail %q", *p.Detail)
			}
		})
	}
}

// Validation errors say where and why, without the validator's internals.
func TestValidationDetails(t *testing.T) {
	h := newHarness(t)
	cases := map[string]struct {
		method, path string
		body         any
		want         string
	}{
		"path parameter": {"GET", "/api/v1/quizzes/k7q2mx", nil, `parameter "code": 'k7q2mx' does not match pattern`},
		"query range":    {"GET", "/api/v1/quizzes/K7Q2MX/leaderboard?limit=1001", nil, `parameter "limit": maximum: got 1,001, want 1,000`},
		"body field":     {"POST", "/api/v1/quizzes", map[string]any{"questionSetId": "demo-quick", "questionWindowSeconds": 4}, `/questionWindowSeconds: minimum: got 4, want 5`},
		"unknown field":  {"POST", "/api/v1/quizzes", map[string]any{"questionSetId": "demo-quick", "hostId": "x"}, `additional properties 'hostId' not allowed`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := h.do(tc.method, tc.path, h.host(), tc.body).problem(t)
			if p.Detail == nil || !strings.Contains(*p.Detail, tc.want) || strings.Contains(*p.Detail, "schema.json") {
				t.Errorf("detail %v, want it to contain %q and no schema URL", deref(p.Detail), tc.want)
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// The contract is validated before the service runs: invalid input never reaches it.
func TestCreateQuiz_ValidatesAgainstTheContract(t *testing.T) {
	h := newHarness(t) // no service expectations: any call fails the test
	for name, body := range map[string]any{
		"window too short": map[string]any{"questionSetId": "demo-quick", "questionWindowSeconds": 4},
		"window too long":  map[string]any{"questionSetId": "demo-quick", "questionWindowSeconds": 121},
		"reveal too short": map[string]any{"questionSetId": "demo-quick", "revealSeconds": 1},
		"missing set":      map[string]any{"questionWindowSeconds": 10},
		"extra field":      map[string]any{"questionSetId": "demo-quick", "hostId": "someone_else"},
		"window as string": map[string]any{"questionSetId": "demo-quick", "questionWindowSeconds": "10"},
		"no body":          nil,
		"huge body":        `{"questionSetId":"` + strings.Repeat("a", 70_000) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			expectStatus(t, h.do("POST", "/api/v1/quizzes", h.host(), body), http.StatusBadRequest, gen.ErrorCodeInvalidRequest)
		})
	}
}

func TestGetQuiz(t *testing.T) {
	h := newHarness(t)
	h.quizzes.EXPECT().Get(gomock.Any(), quiz.Code("K7Q2MX")).Return(lobby, nil)
	r := h.do("GET", "/api/v1/quizzes/K7Q2MX", h.participant(), nil)
	expectStatus(t, r, http.StatusOK, "")

	finished := created.Add(5 * time.Minute)
	done := lobby
	done.Status, done.LobbyExpiresAt, done.FinishedAt, done.ParticipantCount = quiz.StatusFinished, nil, &finished, 12
	h.quizzes.EXPECT().Get(gomock.Any(), gomock.Any()).Return(done, nil)
	var got gen.Quiz
	h.do("GET", "/api/v1/quizzes/K7Q2MX", h.participant(), nil).json(t, &got)
	if got.Status != gen.Finished || got.FinishedAt == nil || got.LobbyExpiresAt != nil || got.ParticipantCount != 12 {
		t.Errorf("finished quiz = %+v", got)
	}

	h.quizzes.EXPECT().Get(gomock.Any(), gomock.Any()).Return(quiz.Summary{}, quiz.ErrUnknownQuiz)
	expectStatus(t, h.do("GET", "/api/v1/quizzes/ZZZZZZ", h.participant(), nil), http.StatusNotFound, gen.ErrorCodeUnknownQuiz)
}

func TestGetQuiz_InvalidCodes(t *testing.T) {
	h := newHarness(t)
	for _, c := range []string{"k7q2mx", "K7Q2M", "K7Q2MXX", "K7Q2M0", "K7Q2MI"} {
		expectStatus(t, h.do("GET", "/api/v1/quizzes/"+c, h.participant(), nil), http.StatusBadRequest, gen.ErrorCodeInvalidRequest)
	}
}

func TestStartQuiz(t *testing.T) {
	h := newHarness(t)
	h.quizzes.EXPECT().Start(gomock.Any(), quiz.Code("K7Q2MX"), quiz.ParticipantID("host_1")).Return(nil)
	r := h.do("POST", "/api/v1/quizzes/K7Q2MX/start", h.host(), nil)
	expectStatus(t, r, http.StatusAccepted, "")
	var got gen.StartAccepted
	r.json(t, &got)
	if got.Code != "K7Q2MX" || !bool(got.StartRequested) {
		t.Errorf("got %+v", got)
	}
	cases := map[error]struct {
		status int
		code   gen.ErrorCode
	}{
		quiz.ErrNotHost:        {http.StatusForbidden, gen.ErrorCodeNotHost},
		quiz.ErrNotInLobby:     {http.StatusConflict, gen.ErrorCodeNotInLobby},
		quiz.ErrNoParticipants: {http.StatusConflict, gen.ErrorCodeNoParticipants},
		quiz.ErrUnknownQuiz:    {http.StatusNotFound, gen.ErrorCodeUnknownQuiz},
		errRedisDown:           {http.StatusServiceUnavailable, gen.ErrorCodeUnavailable},
	}
	for err, want := range cases {
		h.quizzes.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).Return(err)
		expectStatus(t, h.do("POST", "/api/v1/quizzes/K7Q2MX/start", h.host(), nil), want.status, want.code)
	}
}

func TestGetLeaderboard(t *testing.T) {
	h := newHarness(t)
	page := leaderboard.Page{Code: "K7Q2MX", Status: quiz.StatusQuestionOpen, ParticipantCount: 3, Offset: 0, Limit: 100, Entries: []leaderboard.Entry{
		{Rank: 1, ParticipantID: "u_2", DisplayName: "Tomás", Score: 186},
		{Rank: 1, ParticipantID: "u_1", DisplayName: "Rina", Score: 186},
		{Rank: 3, ParticipantID: "u_3", DisplayName: "Aiko", Score: 0},
	}}
	h.boards.EXPECT().Page(gomock.Any(), quiz.Code("K7Q2MX"), 0, 100).Return(page, nil)
	r := h.do("GET", "/api/v1/quizzes/K7Q2MX/leaderboard", h.participant(), nil)
	expectStatus(t, r, http.StatusOK, "")
	var got gen.LeaderboardPage
	r.json(t, &got)
	if len(got.Entries) != 3 || got.Entries[1].Rank != 1 || got.Entries[1].DisplayName != "Rina" || got.Status != gen.QuestionOpen {
		t.Errorf("page = %+v", got)
	}

	h.boards.EXPECT().Page(gomock.Any(), gomock.Any(), 20, 10).Return(leaderboard.Page{Code: "K7Q2MX", Status: quiz.StatusFinished, ParticipantCount: 3, Offset: 20, Limit: 10, Entries: []leaderboard.Entry{}}, nil)
	if r := h.do("GET", "/api/v1/quizzes/K7Q2MX/leaderboard?offset=20&limit=10", h.participant(), nil); !strings.Contains(r.Body.String(), `"entries":[]`) {
		t.Errorf("empty page encoded as %s", r.body)
	}

	h.boards.EXPECT().Page(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaderboard.Page{}, quiz.ErrUnknownQuiz)
	expectStatus(t, h.do("GET", "/api/v1/quizzes/ZZZZZZ/leaderboard", h.participant(), nil), http.StatusNotFound, gen.ErrorCodeUnknownQuiz)

	for _, q := range []string{"limit=0", "limit=1001", "offset=-1", "limit=ten"} {
		expectStatus(t, h.do("GET", "/api/v1/quizzes/K7Q2MX/leaderboard?"+q, h.participant(), nil), http.StatusBadRequest, gen.ErrorCodeInvalidRequest)
	}
}

// --- cross-cutting ---

func TestPanicIsContained(t *testing.T) {
	h := newHarness(t)
	h.quizzes.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, quiz.Code) (quiz.Summary, error) { panic("boom") })
	expectStatus(t, h.do("GET", "/api/v1/quizzes/K7Q2MX", h.participant(), nil), http.StatusInternalServerError, gen.ErrorCodeInternal)
	h.quizzes.EXPECT().Get(gomock.Any(), gomock.Any()).Return(lobby, nil)
	expectStatus(t, h.do("GET", "/api/v1/quizzes/K7Q2MX", h.participant(), nil), http.StatusOK, "")
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.do("GET", "/healthz", "", nil), http.StatusOK, "")
	expectStatus(t, h.do("GET", "/readyz", "", nil), http.StatusOK, "")
	h.ready = errRedisDown
	if r := h.do("GET", "/readyz", "", nil); r.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz with Redis down: %d", r.Code)
	}
	expectStatus(t, h.do("GET", "/healthz", "", nil), http.StatusOK, "")
}

func TestUnknownRoutes(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.do("GET", "/api/v1/nope", h.host(), nil), http.StatusNotFound, gen.ErrorCodeNotFound)
	expectStatus(t, h.do("DELETE", "/api/v1/quizzes/K7Q2MX", h.host(), nil), http.StatusMethodNotAllowed, gen.ErrorCodeInvalidRequest)
}

func TestRequestIDs(t *testing.T) {
	h := newHarness(t)
	for in, echoed := range map[string]bool{"req-123_abc.DEF": true, strings.Repeat("a", 65): false, "has space": false, "": false} {
		req := newRequest("GET", "/healthz")
		if in != "" {
			req.Header.Set("X-Request-ID", in)
		}
		rec := serve(h, req)
		got := rec.Header().Get("X-Request-ID")
		if got == "" || (got == in) != echoed {
			t.Errorf("inbound %q → %q (echo expected: %v)", in, got, echoed)
		}
	}
}

func TestMetricsCountErrors(t *testing.T) {
	h := newHarness(t)
	h.do("GET", "/api/v1/quizzes/K7Q2MX", "", nil)
	rec := serve(h, newRequest("GET", "/metrics"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `errors_total{code="unauthorized",service="api"} 1`) {
		t.Errorf("metrics %d:\n%s", rec.Code, grep(rec.Body.String(), "errors_total"))
	}
}
