package resilience

import (
	"testing"
	"time"
)

func TestBackoffNoJitter(t *testing.T) {
	p := NewPolicy(
		WithBaseDelay(100*time.Millisecond),
		WithMultiplier(2),
		WithMaxDelay(1*time.Second),
		WithJitter(false),
	)
	cases := map[int]time.Duration{
		1: 0,                      // first attempt: no wait
		2: 100 * time.Millisecond, // base
		3: 200 * time.Millisecond, // base*2
		4: 400 * time.Millisecond, // base*4
		5: 800 * time.Millisecond, // base*8
		6: 1 * time.Second,        // capped at MaxDelay (would be 1600ms)
		7: 1 * time.Second,        // stays capped
	}
	for attempt, want := range cases {
		if got := p.Backoff(attempt); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", attempt, got, want)
		}
	}
}

func TestBackoffFullJitter(t *testing.T) {
	// Jitter source returning its argument = the maximum; returning 0 = the min.
	max := NewPolicy(WithBaseDelay(100*time.Millisecond), WithMultiplier(2), WithJitter(true),
		WithJitterSource(func(n int64) int64 { return n }))
	if got := max.Backoff(3); got != 200*time.Millisecond {
		t.Fatalf("max jitter Backoff(3) = %v, want 200ms", got)
	}
	min := NewPolicy(WithBaseDelay(100*time.Millisecond), WithJitter(true),
		WithJitterSource(func(int64) int64 { return 0 }))
	if got := min.Backoff(3); got != 0 {
		t.Fatalf("min jitter Backoff(3) = %v, want 0", got)
	}
}

func TestPolicyAttemptsFloor(t *testing.T) {
	if got := NewPolicy(WithMaxAttempts(0)).attempts(); got != 1 {
		t.Fatalf("attempts() with MaxAttempts=0 = %d, want 1", got)
	}
}
