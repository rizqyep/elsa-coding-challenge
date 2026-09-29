package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// ErrBudgetExhausted is returned (wrapping the last error) when the next retry would exceed the budget.
var ErrBudgetExhausted = errors.New("retry budget exhausted")

// Policy is capped exponential backoff with full jitter (TRD §9.3).
type Policy struct {
	Base   time.Duration
	Cap    time.Duration
	Budget time.Duration // total across all attempts
}

// Delay is uniform in [0, min(Cap, Base·2^attempt)); rnd(n) must return a value in [0, n).
func (p Policy) Delay(attempt int, rnd func(n int64) int64) time.Duration {
	ceiling := p.ceiling(attempt)
	if ceiling <= 0 {
		return 0
	}
	return time.Duration(rnd(int64(ceiling)))
}

// ceiling stops doubling at Cap, so it can't overflow.
func (p Policy) ceiling(attempt int) time.Duration {
	c := p.Base
	for i := 0; i < attempt && c < p.Cap; i++ {
		c *= 2
	}
	return min(c, p.Cap)
}

// Clock is the time source; tests replace it.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Retrier runs an operation until it succeeds, fails permanently, or the budget runs out.
type Retrier struct {
	Policy    Policy
	Clock     Clock
	Rand      func(n int64) int64
	Retryable func(error) bool // nil: every error is retryable
}

// New returns a Retrier using the real clock and a random source.
func New(p Policy, retryable func(error) bool) Retrier {
	return Retrier{Policy: p, Clock: realClock{}, Rand: rand.Int64N, Retryable: retryable}
}

// Do retries fn until success, a non-retryable error, context end, or the budget runs out.
func (r Retrier) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	start := r.Clock.Now()
	for attempt := 0; ; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if r.Retryable != nil && !r.Retryable(err) {
			return err
		}
		d := r.Policy.Delay(attempt, r.Rand)
		if r.Clock.Now().Sub(start)+d > r.Policy.Budget {
			return fmt.Errorf("%w after %d attempts: %w", ErrBudgetExhausted, attempt+1, err)
		}
		if err := r.Clock.Sleep(ctx, d); err != nil {
			return err
		}
	}
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
