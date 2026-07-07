package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

// noSleep makes Retry's inter-attempt waits instant + deterministic.
func noSleep() RetryOption {
	return withSleep(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
}

func TestRetrySucceedsAfterFailures(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), NewPolicy(WithMaxAttempts(3)), func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	}, noSleep())
	if err != nil {
		t.Fatalf("Retry = %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryExhaustsAndReturnsLastError(t *testing.T) {
	calls := 0
	want := errors.New("still failing")
	err := Retry(context.Background(), NewPolicy(WithMaxAttempts(3)), func(context.Context) error {
		calls++
		return want
	}, noSleep())
	if !errors.Is(err, want) {
		t.Fatalf("Retry = %v, want %v", err, want)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (attempt cap)", calls)
	}
}

func TestRetryStopsOnNonRetryable(t *testing.T) {
	calls := 0
	fatal := errors.New("bad request")
	err := Retry(context.Background(), NewPolicy(WithMaxAttempts(5)), func(context.Context) error {
		calls++
		return fatal
	}, WithRetryable(func(err error) bool { return false }), noSleep())
	if !errors.Is(err, fatal) {
		t.Fatalf("Retry = %v, want %v", err, fatal)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (non-retryable stops immediately)", calls)
	}
}

func TestRetryHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Retry(ctx, NewPolicy(WithMaxAttempts(5), WithBaseDelay(time.Hour)), func(context.Context) error {
		calls++
		cancel() // cancel during the first attempt; the backoff wait must abort
		return errors.New("transient")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Retry = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (cancelled before retry)", calls)
	}
}
