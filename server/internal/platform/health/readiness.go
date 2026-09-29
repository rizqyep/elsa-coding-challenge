package health

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
)

// ErrDraining is the readiness error once shutdown has begun.
var ErrDraining = errors.New("shutting down")

// Readiness answers /readyz from a dependency check, and says not ready as soon as shutdown begins (NFR-15).
type Readiness struct {
	check    func(context.Context) error
	draining atomic.Bool
}

// NewReadiness wraps check, which pings the service's dependencies.
func NewReadiness(check func(context.Context) error) *Readiness { return &Readiness{check: check} }

// Check returns nil when the service should receive traffic.
func (r *Readiness) Check(ctx context.Context) error {
	if r.draining.Load() {
		return ErrDraining
	}
	return r.check(ctx)
}

// Handler serves /readyz: 200 when ready, 503 otherwise.
func (r *Readiness) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := r.Check(req.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// Shutdown marks the service not ready, then runs steps in order.
func (r *Readiness) Shutdown(steps ...func()) {
	r.draining.Store(true)
	for _, step := range steps {
		step()
	}
}
