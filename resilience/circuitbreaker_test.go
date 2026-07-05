package resilience

import (
	"errors"
	"testing"
	"time"
)

func TestBreakerTripsAndRejects(t *testing.T) {
	b := NewCircuitBreaker(WithFailureThreshold(2), WithCooldown(time.Minute))
	boom := errors.New("boom")

	// Two failures trip it.
	_ = b.Execute(func() error { return boom })
	if b.State() != StateClosed {
		t.Fatalf("after 1 failure state = %v, want closed", b.State())
	}
	_ = b.Execute(func() error { return boom })
	if b.State() != StateOpen {
		t.Fatalf("after 2 failures state = %v, want open", b.State())
	}

	// Open rejects without calling fn.
	called := false
	if err := b.Execute(func() error { called = true; return nil }); !errors.Is(err, ErrOpen) {
		t.Fatalf("open Execute = %v, want ErrOpen", err)
	}
	if called {
		t.Fatal("fn must not run while open")
	}
}

func TestBreakerHalfOpenRecovery(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewCircuitBreaker(WithFailureThreshold(1), WithCooldown(30*time.Second),
		withClock(func() time.Time { return now }))

	_ = b.Execute(func() error { return errors.New("boom") }) // trips (threshold 1)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	// Before cooldown: still open.
	now = now.Add(29 * time.Second)
	if err := b.Execute(func() error { return nil }); !errors.Is(err, ErrOpen) {
		t.Fatalf("before cooldown = %v, want ErrOpen", err)
	}

	// After cooldown: half-open admits one probe; success closes it.
	now = now.Add(2 * time.Second)
	if err := b.Execute(func() error { return nil }); err != nil {
		t.Fatalf("probe = %v, want nil", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("after successful probe state = %v, want closed", b.State())
	}
}

func TestBreakerHalfOpenProbeFailureReopens(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewCircuitBreaker(WithFailureThreshold(1), WithCooldown(10*time.Second),
		withClock(func() time.Time { return now }))
	_ = b.Execute(func() error { return errors.New("boom") })
	now = now.Add(11 * time.Second) // enter half-open

	if err := b.Execute(func() error { return errors.New("still down") }); err == nil {
		t.Fatal("probe should surface the underlying error")
	}
	if b.State() != StateOpen {
		t.Fatalf("failed probe state = %v, want open again", b.State())
	}
}

func TestBreakerStateChangeHook(t *testing.T) {
	var transitions []string
	b := NewCircuitBreaker(WithFailureThreshold(1), WithCooldown(time.Nanosecond),
		WithOnStateChange(func(from, to State) {
			transitions = append(transitions, from.String()+"->"+to.String())
		}))
	_ = b.Execute(func() error { return errors.New("boom") }) // closed->open
	_ = b.State()                                             // open->half-open (cooldown ~0)
	_ = b.Execute(func() error { return nil })                // half-open->closed
	if len(transitions) < 2 || transitions[0] != "closed->open" {
		t.Fatalf("unexpected transitions: %v", transitions)
	}
}
