package testkit

import (
	"slices"
	"sync"
	"time"
)

// Recorder collects latencies; safe for concurrent use.
type Recorder struct {
	mu sync.Mutex
	d  []time.Duration
}

// Add records one latency.
func (r *Recorder) Add(d time.Duration) {
	r.mu.Lock()
	r.d = append(r.d, d)
	r.mu.Unlock()
}

// Summary is a set of nearest-rank percentiles.
type Summary struct {
	Count              int
	P50, P95, P99, Max time.Duration
}

// Summary returns the percentiles of what was recorded so far.
func (r *Recorder) Summary() Summary {
	r.mu.Lock()
	d := slices.Clone(r.d)
	r.mu.Unlock()
	if len(d) == 0 {
		return Summary{}
	}
	slices.Sort(d)
	at := func(p int) time.Duration { return d[max((p*len(d)+99)/100-1, 0)] }
	return Summary{Count: len(d), P50: at(50), P95: at(95), P99: at(99), Max: d[len(d)-1]}
}
