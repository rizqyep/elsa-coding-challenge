package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/gen"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics"
)

// Config wires the REST API.
type Config struct {
	Quizzes        Quizzes
	Leaderboards   Leaderboards
	Tokens         *auth.Tokens
	DevTokens      bool
	Ready          func(context.Context) error
	Log            *slog.Logger
	Metrics        *prometheus.Registry
	RequestTimeout time.Duration
}

type api struct {
	cfg    Config
	errors *prometheus.CounterVec
}

// New returns the REST API: every request is routed, authenticated, and validated against
// openapi.yaml before a handler runs (D13).
func New(c Config) (http.Handler, error) {
	doc, err := gen.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load openapi spec: %w", err)
	}
	doc.Servers = nil // match any host; the spec's server URL is for documentation
	if err := checkAccess(doc); err != nil {
		return nil, err
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("openapi router: %w", err)
	}
	a := &api{cfg: c, errors: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "errors_total", Help: "Errors returned to callers, by service and code (TRD §9.6).",
	}, []string{"service", "code"})}
	if err := c.Metrics.Register(a.errors); err != nil {
		return nil, fmt.Errorf("register metrics: %w", err)
	}

	strict := gen.NewStrictHandlerWithOptions(a, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  func(w http.ResponseWriter, r *http.Request, err error) { a.fail(w, r, invalid(err)) },
		ResponseErrorHandlerFunc: a.fail,
	})
	routes := gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { a.fail(w, r, invalid(err)) },
	})
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler(c.Metrics))
	mux.Handle("/", a.contract(router, routes))
	return a.requestID(a.recover(mux)), nil
}

type rule int

const (
	public rule = iota
	anyRole
	hostOnly
)

// access lists who may call each operation, keyed by the generated operation name; New refuses
// to start if an operation is missing, so a new endpoint can't ship unprotected by accident.
var access = map[string]rule{
	"CreateDevToken":   public,
	"Healthz":          public,
	"Readyz":           public,
	"ListQuestionSets": hostOnly,
	"CreateQuiz":       hostOnly,
	"StartQuiz":        hostOnly,
	"GetQuiz":          anyRole,
	"GetLeaderboard":   anyRole,
}

func checkAccess(doc *openapi3.T) error {
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			r, ok := access[op.OperationID]
			if !ok {
				return fmt.Errorf("%s %s (%s) has no access rule", method, path, op.OperationID)
			}
			if specPublic := op.Security != nil && len(*op.Security) == 0; specPublic != (r == public) {
				return fmt.Errorf("%s: access rule disagrees with the spec's security", op.OperationID)
			}
		}
	}
	return nil
}
