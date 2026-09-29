package testkit

import (
	"sync"
	"testing"
	"time"
)

func TestRecorder_Percentiles(t *testing.T) {
	var r Recorder
	for i := 1; i <= 100; i++ {
		r.Add(time.Duration(i) * time.Millisecond)
	}
	s := r.Summary()
	if s.Count != 100 || s.P50 != 50*time.Millisecond || s.P95 != 95*time.Millisecond || s.P99 != 99*time.Millisecond || s.Max != 100*time.Millisecond {
		t.Errorf("summary %+v; want nearest-rank p50 50ms, p95 95ms, p99 99ms, max 100ms", s)
	}
}

func TestRecorder_EmptyAndConcurrent(t *testing.T) {
	var r Recorder
	if s := r.Summary(); s.Count != 0 || s.P95 != 0 {
		t.Errorf("empty summary %+v", s)
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { r.Add(time.Millisecond) })
	}
	wg.Wait()
	if s := r.Summary(); s.Count != 50 {
		t.Errorf("count %d after 50 concurrent adds", s.Count)
	}
}
