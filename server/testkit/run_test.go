package testkit

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// An answer is covered by the first leaderboard version that started arriving after it was accepted;
// its NFR-6 latency is when that version reached the last client (TRD §10.3).
func TestLeaderboardLatencies(t *testing.T) {
	at := func(ms int) time.Time { return time.UnixMilli(int64(ms)) }
	arrivals := map[int64][]time.Time{
		1: {at(110), at(100), at(150)},
		2: {at(300), at(320)},
	}
	got := leaderboardLatencies([]time.Time{at(50), at(120), at(400)}, arrivals)
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}
	if !slices.Equal(got, want) {
		t.Errorf("latencies %v, want %v (an answer after the last update is not counted)", got, want)
	}
}

func TestChooseOption_FollowsTheRatios(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	opts := []string{"a", "b", "c", "d"}
	var right, wrong, none int
	for range 10_000 {
		switch o := chooseOption(rng, opts, "b", 0.7, 0.1); o {
		case "":
			none++
		case "b":
			right++
		default:
			if !slices.Contains(opts, o) {
				t.Fatalf("chose %q, not an option", o)
			}
			wrong++
		}
	}
	near := func(n int, share float64) bool { return float64(n) > share*10_000*0.9 && float64(n) < share*10_000*1.1 }
	if !near(none, 0.1) || !near(right, 0.9*0.7) || !near(wrong, 0.9*0.3) {
		t.Errorf("none %d, right %d, wrong %d of 10000; want about 10%%, 63%%, 27%%", none, right, wrong)
	}
}
