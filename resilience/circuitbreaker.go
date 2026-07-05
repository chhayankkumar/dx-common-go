package resilience

import (
	"errors"
	"sync"
	"time"
)

// ErrOpen is returned by a CircuitBreaker when it is open (rejecting calls).
var ErrOpen = errors.New("resilience: circuit breaker is open")

// State is a CircuitBreaker's current state.
type State int

const (
	// StateClosed passes calls through; failures are counted.
	StateClosed State = iota
	// StateOpen rejects calls immediately until the cooldown elapses.
	StateOpen
	// StateHalfOpen allows a single probe call to test recovery.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker trips open after FailureThreshold consecutive failures, stays
// open for Cooldown, then admits one probe (half-open). A probe success closes
// it and resets the count; a probe failure re-opens it for another cooldown.
// Safe for concurrent use.
type CircuitBreaker struct {
	failureThreshold int
	cooldown         time.Duration
	isFailure        func(error) bool
	now              func() time.Time
	onStateChange    func(from, to State)

	mu            sync.Mutex
	state         State
	failures      int
	openedAt      time.Time
	probeInFlight bool
}

// BreakerOption customizes a CircuitBreaker.
type BreakerOption func(*CircuitBreaker)

// WithFailureThreshold sets how many consecutive failures trip the breaker.
func WithFailureThreshold(n int) BreakerOption {
	return func(b *CircuitBreaker) {
		if n > 0 {
			b.failureThreshold = n
		}
	}
}

// WithCooldown sets how long the breaker stays open before admitting a probe.
func WithCooldown(d time.Duration) BreakerOption {
	return func(b *CircuitBreaker) {
		if d > 0 {
			b.cooldown = d
		}
	}
}

// WithFailureClassifier decides which errors count as failures (default: any
// non-nil error, except ErrOpen which never counts).
func WithFailureClassifier(f func(error) bool) BreakerOption {
	return func(b *CircuitBreaker) {
		if f != nil {
			b.isFailure = f
		}
	}
}

// WithOnStateChange registers a state-transition hook (metrics/logging seam).
func WithOnStateChange(f func(from, to State)) BreakerOption {
	return func(b *CircuitBreaker) { b.onStateChange = f }
}

// withClock injects the clock (tests only).
func withClock(f func() time.Time) BreakerOption {
	return func(b *CircuitBreaker) { b.now = f }
}

// NewCircuitBreaker builds a breaker (defaults: 5 failures, 30s cooldown).
func NewCircuitBreaker(opts ...BreakerOption) *CircuitBreaker {
	b := &CircuitBreaker{
		failureThreshold: 5,
		cooldown:         30 * time.Second,
		isFailure:        func(err error) bool { return err != nil },
		now:              time.Now,
		state:            StateClosed,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// State returns the breaker's current state (advancing open→half-open if the
// cooldown has elapsed).
func (b *CircuitBreaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	return b.state
}

// Execute runs fn if the breaker admits it, records the outcome, and returns
// fn's error — or ErrOpen without calling fn when the breaker is open.
func (b *CircuitBreaker) Execute(fn func() error) error {
	if err := b.beforeCall(); err != nil {
		return err
	}
	err := fn()
	b.afterCall(err)
	return err
}

// beforeCall admits or rejects a call, transitioning open→half-open on cooldown.
func (b *CircuitBreaker) beforeCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	switch b.state {
	case StateOpen:
		return ErrOpen
	case StateHalfOpen:
		if b.probeInFlight {
			return ErrOpen // only one probe at a time
		}
		b.probeInFlight = true
	}
	return nil
}

// afterCall records the outcome and updates state.
func (b *CircuitBreaker) afterCall(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	failure := b.isFailure(err) && !errors.Is(err, ErrOpen)

	if b.state == StateHalfOpen {
		b.probeInFlight = false
		if failure {
			b.trip()
		} else {
			b.reset()
		}
		return
	}
	// closed
	if failure {
		b.failures++
		if b.failures >= b.failureThreshold {
			b.trip()
		}
		return
	}
	b.failures = 0
}

// refresh advances open→half-open once the cooldown has passed.
func (b *CircuitBreaker) refresh() {
	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.cooldown {
		b.setState(StateHalfOpen)
		b.probeInFlight = false
	}
}

func (b *CircuitBreaker) trip() {
	b.openedAt = b.now()
	b.setState(StateOpen)
}

func (b *CircuitBreaker) reset() {
	b.failures = 0
	b.setState(StateClosed)
}

func (b *CircuitBreaker) setState(s State) {
	if b.state == s {
		return
	}
	from := b.state
	b.state = s
	if b.onStateChange != nil {
		b.onStateChange(from, s)
	}
}
