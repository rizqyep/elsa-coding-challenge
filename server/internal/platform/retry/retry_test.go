package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
)

// fakeClock advances instantly and records every sleep.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
	// sleepErr, when set, is returned by Sleep (simulates ctx cancellation mid-sleep).
	sleepErr error
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	if c.sleepErr != nil {
		return c.sleepErr
	}
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

var policy = retry.Policy{Base: 100 * time.Millisecond, Cap: 5 * time.Second, Budget: 10 * time.Second}

// maxRand returns the largest allowed value, so Delay returns its upper bound minus 1ns.
func maxRand(n int64) int64 { return n - 1 }
func zeroRand(int64) int64  { return 0 }

func TestDelay_FullJitterBounds(t *testing.T) {
	cases := []struct {
		attempt int
		ceiling time.Duration // min(Cap, Base·2^attempt)
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{5, 3200 * time.Millisecond},
		{6, 5 * time.Second}, // 6.4 s capped
		{10, 5 * time.Second},
	}
	for _, tc := range cases {
		if got := policy.Delay(tc.attempt, maxRand); got != tc.ceiling-1 {
			t.Errorf("attempt %d, max rand: got %v, want %v", tc.attempt, got, tc.ceiling-1)
		}
		if got := policy.Delay(tc.attempt, zeroRand); got != 0 {
			t.Errorf("attempt %d, zero rand: got %v, want 0", tc.attempt, got)
		}
	}
}

func TestDelay_LargeAttemptsDoNotOverflow(t *testing.T) {
	for _, attempt := range []int{62, 63, 64, 1000} {
		got := policy.Delay(attempt, maxRand)
		if got < 0 || got >= policy.Cap {
			t.Errorf("attempt %d: got %v, want within [0, %v)", attempt, got, policy.Cap)
		}
	}
}

func TestDo_SucceedsAfterTransientFailures(t *testing.T) {
	clk := &fakeClock{}
	r := retry.Retrier{Policy: policy, Clock: clk, Rand: maxRand, Retryable: func(error) bool { return true }}
	calls := 0
	err := r.Do(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}
	if calls != 3 || len(clk.sleeps) != 2 {
		t.Errorf("calls=%d sleeps=%d, want 3 and 2", calls, len(clk.sleeps))
	}
}

func TestDo_NonRetryableErrorReturnsImmediately(t *testing.T) {
	clk := &fakeClock{}
	permanent := errors.New("permanent")
	r := retry.Retrier{Policy: policy, Clock: clk, Rand: maxRand, Retryable: func(err error) bool { return !errors.Is(err, permanent) }}
	calls := 0
	err := r.Do(context.Background(), func(context.Context) error { calls++; return permanent })
	if !errors.Is(err, permanent) {
		t.Fatalf("got %v, want %v", err, permanent)
	}
	if calls != 1 || len(clk.sleeps) != 0 {
		t.Errorf("calls=%d sleeps=%d, want 1 and 0", calls, len(clk.sleeps))
	}
}

func TestDo_StopsBeforeExceedingBudget(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	last := errors.New("still failing")
	r := retry.Retrier{Policy: policy, Clock: clk, Rand: maxRand, Retryable: func(error) bool { return true }}
	err := r.Do(context.Background(), func(context.Context) error { return last })

	if !errors.Is(err, retry.ErrBudgetExhausted) || !errors.Is(err, last) {
		t.Fatalf("got %v, want an error wrapping both ErrBudgetExhausted and the last error", err)
	}
	var total time.Duration
	for _, s := range clk.sleeps {
		total += s
	}
	if total > policy.Budget {
		t.Errorf("slept %v in total, budget is %v", total, policy.Budget)
	}
	if len(clk.sleeps) == 0 {
		t.Error("never retried")
	}
}

func TestDo_ContextCancelledDuringSleep(t *testing.T) {
	clk := &fakeClock{sleepErr: context.Canceled}
	r := retry.Retrier{Policy: policy, Clock: clk, Rand: maxRand, Retryable: func(error) bool { return true }}
	err := r.Do(context.Background(), func(context.Context) error { return errors.New("transient") })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestDo_ContextAlreadyCancelledDoesNotCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := retry.Retrier{Policy: policy, Clock: &fakeClock{}, Rand: maxRand, Retryable: func(error) bool { return true }}
	called := false
	err := r.Do(ctx, func(context.Context) error { called = true; return nil })
	if called || !errors.Is(err, context.Canceled) {
		t.Errorf("called=%v err=%v, want not called and context.Canceled", called, err)
	}
}
