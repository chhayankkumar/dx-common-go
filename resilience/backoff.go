// Package resilience is the shared outbound-call reliability layer of the
// framework: a retry policy with exponential backoff + jitter, a circuit
// breaker, and ready-made wrappers for HTTP (*http.Client) and gRPC
// (UnaryClientInterceptor). It replaces the retry/backoff loops hand-rolled
// across the codebase with one policy to reason about, and gives the outbound
// clients (auth/fga, auth/appid, keycloak, notify/email) the retry+breaker they
// currently lack. It contains no business logic — only generic infrastructure.
package resilience

import (
	"math"
	"math/rand"
	"time"
)

// Policy describes a retry schedule: the attempt cap, the base delay, its
// exponential growth, an upper per-attempt cap, and whether to apply jitter.
// Build one with DefaultPolicy and options rather than by hand.
type Policy struct {
	// MaxAttempts is the total number of attempts including the first
	// (so 3 means one try + two retries). Values < 1 are treated as 1.
	MaxAttempts int
	// BaseDelay is the wait before the second attempt.
	BaseDelay time.Duration
	// MaxDelay caps any single inter-attempt delay.
	MaxDelay time.Duration
	// Multiplier grows the delay each attempt (>= 1).
	Multiplier float64
	// Jitter applies full jitter (delay becomes a uniform random value in
	// [0, computed]) to avoid synchronized retries across replicas.
	Jitter bool

	// rng is an injectable source for deterministic tests; nil uses a shared
	// default. Not exported — set via WithJitterSource for tests only.
	rng func(n int64) int64
}

// DefaultPolicy is a sensible starting point: 3 attempts, 100ms base, ×2 growth
// capped at 10s, with full jitter.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
		Jitter:      true,
	}
}

// PolicyOption customizes a Policy.
type PolicyOption func(*Policy)

// NewPolicy builds a Policy from DefaultPolicy plus options.
func NewPolicy(opts ...PolicyOption) Policy {
	p := DefaultPolicy()
	for _, o := range opts {
		o(&p)
	}
	return p
}

// WithMaxAttempts sets the total attempt cap (including the first attempt).
func WithMaxAttempts(n int) PolicyOption { return func(p *Policy) { p.MaxAttempts = n } }

// WithBaseDelay sets the wait before the second attempt.
func WithBaseDelay(d time.Duration) PolicyOption { return func(p *Policy) { p.BaseDelay = d } }

// WithMaxDelay caps any single inter-attempt delay.
func WithMaxDelay(d time.Duration) PolicyOption { return func(p *Policy) { p.MaxDelay = d } }

// WithMultiplier sets the exponential growth factor.
func WithMultiplier(m float64) PolicyOption { return func(p *Policy) { p.Multiplier = m } }

// WithJitter toggles full jitter.
func WithJitter(on bool) PolicyOption { return func(p *Policy) { p.Jitter = on } }

// WithJitterSource injects a deterministic jitter source (tests only): given n,
// it must return a value in [0, n].
func WithJitterSource(f func(n int64) int64) PolicyOption {
	return func(p *Policy) { p.rng = f }
}

// attempts returns the effective attempt cap (>= 1).
func (p Policy) attempts() int {
	if p.MaxAttempts < 1 {
		return 1
	}
	return p.MaxAttempts
}

// Backoff returns the delay to wait before the given 1-based attempt number.
// Attempt 1 has no wait; attempt 2 waits ~BaseDelay; each subsequent attempt
// multiplies by Multiplier, capped at MaxDelay, then (optionally) jittered.
func (p Policy) Backoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	mult := p.Multiplier
	if mult < 1 {
		mult = 1
	}
	d := float64(p.BaseDelay) * math.Pow(mult, float64(attempt-2))
	if max := float64(p.MaxDelay); p.MaxDelay > 0 && d > max {
		d = max
	}
	delay := time.Duration(d)
	if delay < 0 {
		delay = p.MaxDelay
	}
	if p.Jitter && delay > 0 {
		delay = time.Duration(p.jitter(int64(delay)))
	}
	return delay
}

// jitter returns a value in [0, n] using the policy's source (or a default).
func (p Policy) jitter(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if p.rng != nil {
		return p.rng(n)
	}
	return rand.Int63n(n + 1) // #nosec G404 — jitter, not security-sensitive
}
