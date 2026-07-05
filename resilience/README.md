# `resilience` — outbound-call reliability

Retry with exponential backoff + jitter, a circuit breaker, and ready-made HTTP
and gRPC wrappers. It replaces the retry loops hand-rolled across the codebase
with one policy to reason about, and gives the outbound clients
(`auth/fga`, `auth/appid`, keycloak JWKS, `notify/email`) the retry + breaker
they otherwise lack. No business logic — generic infrastructure only.

## Retry policy

```go
p := resilience.NewPolicy(
    resilience.WithMaxAttempts(4),
    resilience.WithBaseDelay(100*time.Millisecond),
    resilience.WithMaxDelay(5*time.Second),
    resilience.WithMultiplier(2),   // 100ms, 200ms, 400ms … capped at 5s
    resilience.WithJitter(true),    // full jitter to de-synchronize replicas
)

err := resilience.Retry(ctx, p, func(ctx context.Context) error {
    return doThing(ctx)
}, resilience.WithRetryable(func(err error) bool {
    return isTransient(err) // default: retry all non-nil except ctx cancel
}))
```

`Retry` waits `Backoff` between attempts, aborts early on a non-retryable error
or context cancellation, and returns the last error. `WithOnRetry` is the
metrics/logging seam.

## Circuit breaker

```go
b := resilience.NewCircuitBreaker(
    resilience.WithFailureThreshold(5),  // consecutive failures to trip
    resilience.WithCooldown(30*time.Second),
    resilience.WithOnStateChange(func(from, to resilience.State) { … }),
)

err := b.Execute(func() error { return call() })
// b.Execute returns resilience.ErrOpen without calling fn while open.
```

`closed → open` after the threshold; after the cooldown one probe is admitted
(`half-open`); a probe success closes it, a failure re-opens it. Concurrency-safe.

## HTTP client

```go
client := resilience.NewHTTPClient(
    resilience.WithPolicy(p),
    resilience.WithBreaker(b),                    // optional shared breaker
    resilience.WithClientTimeout(10*time.Second),
    // resilience.WithRetryMethods("GET","POST"), // opt POST in (default: idempotent only)
)
resp, err := client.Get(url)
```

Retries idempotent methods on transport errors and `429/500/502/503/504`,
honors `Retry-After`, and replays request bodies safely (buffering when the
caller didn't set `GetBody`). Non-idempotent methods and unbufferable bodies are
attempted once. Compose with `otelhttp` via `WithBaseTransport`.

## gRPC

```go
conn, _ := grpc.NewClient(target, grpc.WithChainUnaryInterceptor(
    resilience.UnaryClientInterceptor(
        resilience.WithGRPCPolicy(p),
        resilience.WithGRPCBreaker(b),
        // resilience.WithRetryableCodes(codes.Unavailable, codes.Aborted),
    ),
))
```

Retries transient codes (`Unavailable`, `ResourceExhausted`, `Aborted` by
default); a permanent code (e.g. `InvalidArgument`) is returned immediately.

## Design notes

- Functional options throughout, matching `postgres.NewPool` / the elasticsearch
  framework — consistent adoption across the fleet.
- Injectable clock / jitter / sleep seams (`withClock`, `WithJitterSource`,
  internal `withSleep`) make every path deterministically unit-testable — the
  suite runs with no real sleeps and covers backoff bounds, breaker state
  transitions, idempotency gating, body replay, and `Retry-After`.
- Only idempotent operations retry by default; opt in explicitly for the rest.
