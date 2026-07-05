package resilience

import (
	"context"
	"errors"
	"time"
)

// Retryable reports whether an error is worth retrying. The default
// (defaultRetryable) retries any non-nil error except context cancellation /
// deadline; callers narrow it with WithRetryable (e.g. only transient DB codes).
type Retryable func(error) bool

func defaultRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// retryConfig holds Retry's tunable hooks.
type retryConfig struct {
	retryable Retryable
	onRetry   func(attempt int, err error, delay time.Duration)
	sleep     func(ctx context.Context, d time.Duration) error // injectable for tests
}

// RetryOption customizes a Retry call.
type RetryOption func(*retryConfig)

// WithRetryable sets the error-classification predicate.
func WithRetryable(f Retryable) RetryOption {
	return func(c *retryConfig) {
		if f != nil {
			c.retryable = f
		}
	}
}

// WithOnRetry registers a hook invoked before each backoff wait — the seam for
// metrics/logging (attempt is the one that just failed, 1-based).
func WithOnRetry(f func(attempt int, err error, delay time.Duration)) RetryOption {
	return func(c *retryConfig) { c.onRetry = f }
}

// withSleep injects the inter-attempt wait (tests only).
func withSleep(f func(ctx context.Context, d time.Duration) error) RetryOption {
	return func(c *retryConfig) { c.sleep = f }
}

// Retry runs fn up to policy.MaxAttempts times, waiting policy.Backoff between
// attempts, until fn returns nil, returns a non-retryable error, the attempt
// cap is reached, or ctx is done. It returns the last error fn produced (or the
// context error if cancelled mid-wait).
func Retry(ctx context.Context, policy Policy, fn func(context.Context) error, opts ...RetryOption) error {
	cfg := retryConfig{retryable: defaultRetryable, sleep: sleepCtx}
	for _, o := range opts {
		o(&cfg)
	}

	attempts := policy.attempts()
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if attempt == attempts || !cfg.retryable(lastErr) {
			break
		}
		delay := policy.Backoff(attempt + 1)
		if cfg.onRetry != nil {
			cfg.onRetry(attempt, lastErr, delay)
		}
		if err := cfg.sleep(ctx, delay); err != nil {
			return err
		}
	}
	return lastErr
}

// sleepCtx waits d or returns early with ctx.Err() on cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
