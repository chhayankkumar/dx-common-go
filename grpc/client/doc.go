// Package client dials gRPC servers with the platform's standard client
// behaviour applied by default — a resilience unary interceptor (retry on
// transient codes + optional circuit breaker), OpenTelemetry tracing, and
// keepalive — so services don't re-hand-roll grpc.NewClient plumbing.
//
// Everything is no-op-safe: tracing does nothing until observability.Init
// configures a provider, and the resilience interceptor only retries idempotent
// transient failures. Both are on by default and can be disabled per-dial
// (WithoutResilience / WithoutTracing). TLS is opt-in via Config; the default
// is an insecure channel (TLS terminated by the service mesh).
package client
