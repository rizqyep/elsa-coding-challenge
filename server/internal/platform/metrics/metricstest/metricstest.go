// Package metricstest reads /metrics the way Prometheus does, for integration tests and the test kit.
package metricstest

// AI-assisted: AI-036 (docs/ai-collaboration/log.md).

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// highCardinality are labels no metric may use: one series per quiz or person would grow without bound.
var highCardinality = []string{"quiz", "quiz_code", "participant", "participant_id", "question", "question_id", "conn", "connection"}

// Scrape sums each series across the servers' /metrics, keyed as written, e.g. `transitions_total{to="finished"}`.
func Scrape(t *testing.T, baseURLs ...string) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, u := range baseURLs {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u+"/metrics", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		err = Parse(resp.Body, out)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s/metrics: %d", u, resp.StatusCode)
		}
	}
	return out
}

// Parse adds each series in a Prometheus text exposition to into.
func Parse(r io.Reader, into map[string]float64) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexByte(line, ' ')
		v, err := strconv.ParseFloat(line[i+1:], 64)
		if err != nil {
			return fmt.Errorf("metrics line %q: %w", line, err)
		}
		into[line[:i]] += v
	}
	return sc.Err()
}

// AssertBoundedLabels fails if any series carries a per-quiz or per-person label (NFR-29).
func AssertBoundedLabels(t *testing.T, series map[string]float64) {
	t.Helper()
	for s := range series {
		for _, l := range highCardinality {
			if strings.Contains(s, "{"+l+"=") || strings.Contains(s, ","+l+"=") {
				t.Errorf("series %s uses the unbounded label %q", s, l)
			}
		}
	}
}
