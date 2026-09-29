package main

import (
	"fmt"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/testkit"
)

// Aggregate merges every room of a run.
type Aggregate struct {
	Rooms                                 int
	Participants, Joined, Finished        int
	AnswersSent, Accepted, Duplicates     int64
	Reconnects                            int64
	Stale                                 int
	NFR6, NFR7, NFR8, NFR9, TransitionLag testkit.Summary
	Rejoin                                testkit.Summary
	Errors                                map[string]int
	Closes                                map[int]int
	Violations, PointMismatches           []string
	TotalMismatches, StepErrors           []string
	ReconciliationMetric                  float64 // score_reconciliation_mismatches_total added during the run
}

// AggregateResults merges rooms; percentiles come from the merged raw latencies, not averages of rooms.
func AggregateResults(rs []*testkit.Result) Aggregate {
	a := Aggregate{Rooms: len(rs), Errors: map[string]int{}, Closes: map[int]int{}}
	var n6, n7, n8, n9, lag, rejoin testkit.Recorder
	for _, r := range rs {
		a.Participants += r.Participants
		a.Joined += r.Joined
		a.Finished += r.Finished
		a.AnswersSent += r.AnswersSent
		a.Accepted += r.Accepted
		a.Duplicates += r.Duplicates
		a.Reconnects += r.Reconnects
		a.Stale += r.Stale
		for k, v := range r.Errors {
			a.Errors[k] += v
		}
		for k, v := range r.Closes {
			a.Closes[k] += v
		}
		a.Violations = append(a.Violations, r.Violations...)
		a.PointMismatches = append(a.PointMismatches, r.PointMismatches...)
		a.TotalMismatches = append(a.TotalMismatches, r.TotalMismatches...)
		a.StepErrors = append(a.StepErrors, r.StepErrors...)
		for dst, src := range map[*testkit.Recorder]*testkit.Recorder{&n6: r.Raw.NFR6, &n7: r.Raw.NFR7, &n8: r.Raw.NFR8,
			&n9: r.Raw.NFR9, &lag: r.Raw.TransitionLag, &rejoin: r.Raw.Rejoin} {
			if src != nil {
				dst.Merge(src)
			}
		}
	}
	a.NFR6, a.NFR7, a.NFR8, a.NFR9 = n6.Summary(), n7.Summary(), n8.Summary(), n9.Summary()
	a.TransitionLag, a.Rejoin = lag.Summary(), rejoin.Summary()
	return a
}

// Check is one pass/fail line of the report.
type Check struct {
	Name, Target, Actual string
	Pass                 bool
}

// Evaluate checks the asserted targets (non-functional §2), plus "clean", which every run must pass.
func Evaluate(a Aggregate, asserts []string) []Check {
	below := func(name, what string, got, limit time.Duration) Check {
		return Check{Name: name, Target: fmt.Sprintf("%s < %v", what, limit), Actual: got.String(), Pass: got < limit}
	}
	faults := len(a.Violations) + len(a.StepErrors)
	checks := []Check{{Name: "clean", Target: "all joined and finished, no protocol violations, no failed steps",
		Actual: fmt.Sprintf("%d/%d joined, %d finished, %d violations, %d step errors", a.Joined, a.Participants, a.Finished, len(a.Violations), len(a.StepErrors)),
		Pass:   a.Joined == a.Participants && a.Finished == a.Joined && faults == 0}}
	for _, name := range asserts {
		switch name {
		case "nfr6":
			checks = append(checks, below(name, "p95 answer → leaderboard at every client", a.NFR6.P95, 500*time.Millisecond))
		case "nfr7":
			c := below(name, "p95 answer → result", a.NFR7.P95, 100*time.Millisecond)
			c.Target += ", p99 < 250ms"
			c.Actual = fmt.Sprintf("p95 %v, p99 %v", a.NFR7.P95, a.NFR7.P99)
			c.Pass = c.Pass && a.NFR7.P99 < 250*time.Millisecond
			checks = append(checks, c)
		case "nfr8":
			checks = append(checks, below(name, "p95 question opened → delivered", a.NFR8.P95, 200*time.Millisecond))
		case "nfr9":
			checks = append(checks, below(name, "p95 join → snapshot", a.NFR9.P95, 300*time.Millisecond))
		case "transition-lag":
			checks = append(checks, below(name, "p95 transition lag", a.TransitionLag.P95, 250*time.Millisecond))
		case "reconciliation":
			n := len(a.PointMismatches) + len(a.TotalMismatches)
			checks = append(checks, Check{Name: name, Target: "0 point or total mismatches, score_reconciliation_mismatches_total 0",
				Actual: fmt.Sprintf("%d mismatches, metric %v", n, a.ReconciliationMetric), Pass: n == 0 && a.ReconciliationMetric == 0})
		case "no-errors":
			total := 0
			for _, v := range a.Errors {
				total += v
			}
			checks = append(checks, Check{Name: name, Target: "0 error messages", Actual: fmt.Sprint(a.Errors), Pass: total == 0})
		case "rejoin":
			checks = append(checks, below(name, "slowest reconnect → snapshot", a.Rejoin.Max, 15*time.Second))
		}
	}
	return checks
}
