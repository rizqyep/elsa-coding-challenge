package main

import (
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/testkit"
)

func room(nfr6, nfr7 time.Duration, joined int) *testkit.Result {
	rec := func(d time.Duration) *testkit.Recorder { r := &testkit.Recorder{}; r.Add(d); return r }
	return &testkit.Result{Participants: 10, Joined: joined, Finished: joined, Errors: map[string]int{}, Closes: map[int]int{},
		Raw: testkit.Recorders{NFR6: rec(nfr6), NFR7: rec(nfr7), NFR8: rec(time.Millisecond), NFR9: rec(time.Millisecond),
			TransitionLag: rec(time.Millisecond), Rejoin: &testkit.Recorder{}}}
}

func find(cs []Check, name string) Check {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return Check{Name: "missing"}
}

func TestAggregate_MergesRooms(t *testing.T) {
	a := AggregateResults([]*testkit.Result{room(100*time.Millisecond, time.Millisecond, 10), room(700*time.Millisecond, time.Millisecond, 10)})
	if a.Participants != 20 || a.Joined != 20 || a.NFR6.Count != 2 || a.NFR6.Max != 700*time.Millisecond {
		t.Errorf("aggregate %+v", a)
	}
}

func TestEvaluate_Targets(t *testing.T) {
	good := AggregateResults([]*testkit.Result{room(300*time.Millisecond, 20*time.Millisecond, 10)})
	for _, c := range Evaluate(good, []string{"nfr6", "nfr7", "nfr8", "nfr9", "transition-lag", "reconciliation", "no-errors", "rejoin"}) {
		if !c.Pass {
			t.Errorf("%s failed on a good run: %s vs %s", c.Name, c.Actual, c.Target)
		}
	}
	slow := AggregateResults([]*testkit.Result{room(600*time.Millisecond, 150*time.Millisecond, 10)})
	cs := Evaluate(slow, []string{"nfr6", "nfr7"})
	if find(cs, "nfr6").Pass || find(cs, "nfr7").Pass {
		t.Errorf("slow run passed: %+v", cs)
	}
}

// Lost participants, protocol violations, and score mismatches fail a run whatever it asserts.
func TestEvaluate_CleanAlwaysChecked(t *testing.T) {
	short := room(time.Millisecond, time.Millisecond, 9)
	if find(Evaluate(AggregateResults([]*testkit.Result{short}), nil), "clean").Pass {
		t.Error("a run that lost a participant passed")
	}
	bad := room(time.Millisecond, time.Millisecond, 10)
	bad.PointMismatches = []string{"u_1 q1: server 150, expected 149"}
	cs := Evaluate(AggregateResults([]*testkit.Result{bad}), []string{"reconciliation"})
	if find(cs, "reconciliation").Pass {
		t.Error("a point mismatch passed reconciliation")
	}
	metric := AggregateResults([]*testkit.Result{room(time.Millisecond, time.Millisecond, 10)})
	metric.ReconciliationMetric = 1
	if find(Evaluate(metric, []string{"reconciliation"}), "reconciliation").Pass {
		t.Error("score_reconciliation_mismatches_total 1 passed")
	}
	errs := room(time.Millisecond, time.Millisecond, 10)
	errs.Errors["server_busy"] = 3
	if find(Evaluate(AggregateResults([]*testkit.Result{errs}), []string{"no-errors"}), "no-errors").Pass {
		t.Error("errors passed no-errors")
	}
}
