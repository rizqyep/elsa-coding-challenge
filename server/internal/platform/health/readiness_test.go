package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadiness_ReportsTheCheckUntilDraining(t *testing.T) {
	down := errors.New("redis down")
	var checkErr error
	r := NewReadiness(func(context.Context) error { return checkErr })
	status := func() int {
		rec := httptest.NewRecorder()
		r.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil))
		return rec.Code
	}
	if err := r.Check(context.Background()); err != nil || status() != http.StatusOK {
		t.Fatalf("healthy: Check %v, status %d", err, status())
	}
	checkErr = down
	if err := r.Check(context.Background()); !errors.Is(err, down) || status() != http.StatusServiceUnavailable {
		t.Fatalf("dependency down: Check %v, status %d", err, status())
	}
	checkErr = nil
	r.Shutdown()
	if err := r.Check(context.Background()); !errors.Is(err, ErrDraining) || status() != http.StatusServiceUnavailable {
		t.Fatalf("draining: Check %v, status %d; want not ready even with healthy dependencies", err, status())
	}
}

// Shutdown marks the service not ready before any other step runs (NFR-15).
func TestReadiness_ShutdownGoesNotReadyFirst(t *testing.T) {
	r := NewReadiness(func(context.Context) error { return nil })
	var order []string
	r.Shutdown(
		func() { order = append(order, "drain:"+errString(r.Check(context.Background()))) },
		func() { order = append(order, "stop") },
	)
	if len(order) != 2 || order[0] != "drain:"+ErrDraining.Error() || order[1] != "stop" {
		t.Fatalf("steps saw %v; want not-ready during the first step, then steps in order", order)
	}
}

func errString(err error) string {
	if err == nil {
		return "ready"
	}
	return err.Error()
}
