package realtime_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/realtime"
)

// Draining closes every socket with 1012, each at its own point in the window, so clients don't reconnect together (TRD §7.9).
func TestDrain_ClosesEveryConnectionWith1012AcrossTheWindow(t *testing.T) {
	var n atomic.Int64
	e := newEnv(t, func(o *realtime.Options) {
		// Offsets step through the window: 0, ¼, ½, ¾ of it, then again.
		o.RandIntN = func(limit int) int { return int(n.Add(1)%4) * limit / 4 }
	})
	const conns = 8
	cs := make([]*websocket.Conn, conns)
	for i := range cs {
		cs[i] = e.mustDial()
	}
	eventually(t, "all connections registered", func() bool { return e.gw.Active() == conns })

	const window = 400 * time.Millisecond
	start := time.Now()
	done := make(chan int, 1)
	go func() { done <- e.gw.Drain(context.Background(), window) }()

	closedAt := make([]time.Duration, conns)
	var wg sync.WaitGroup
	for i, c := range cs {
		wg.Go(func() {
			if code := closeCode(t, c, 3*time.Second); code != websocket.CloseServiceRestart {
				t.Errorf("connection %d closed with %d, want 1012", i, code)
			}
			closedAt[i] = time.Since(start)
		})
	}
	wg.Wait()
	select {
	case left := <-done:
		if left != 0 {
			t.Errorf("Drain returned with %d connections open", left)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Drain did not return")
	}
	first, last := closedAt[0], closedAt[0]
	for _, d := range closedAt {
		first, last = min(first, d), max(last, d)
	}
	if last-first < window/2 {
		t.Errorf("closes spread over %v, want them across the %v window", last-first, window)
	}
	if last > window+time.Second {
		t.Errorf("last close at %v, want within the window", last)
	}
}

// While draining, new connections are refused before the upgrade, so nginx sends them to another gateway.
func TestDrain_RefusesNewConnections(t *testing.T) {
	e := newEnv(t, nil)
	e.mustDial()
	eventually(t, "connection registered", func() bool { return e.gw.Active() == 1 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.gw.Drain(ctx, time.Minute)
	eventually(t, "draining", e.gw.Draining)

	_, rej, err := e.dial(e.token(auth.RoleParticipant), testOrigin)
	if err == nil || rej == nil || rej.StatusCode != http.StatusServiceUnavailable || rej.Header.Get("Retry-After") == "" {
		t.Fatalf("dial while draining: %+v, %v; want 503 with Retry-After", rej, err)
	}
}

// Drain gives up when its context ends and reports what is still open.
func TestDrain_StopsAtTheContextDeadline(t *testing.T) {
	e := newEnv(t, nil) // RandIntN returns the last offset, so every close is scheduled at the end of the window
	e.mustDial()
	eventually(t, "connection registered", func() bool { return e.gw.Active() == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if left := e.gw.Drain(ctx, time.Minute); left != 1 {
		t.Errorf("Drain returned %d open, want 1 at its deadline", left)
	}
}
