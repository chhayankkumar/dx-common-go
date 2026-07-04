// Package observability owns the OpenTelemetry SDK lifecycle for a service:
// one Init call wires a TracerProvider + propagators, or does nothing at all
// when no OTLP endpoint is configured. No framework tracing abstraction is
// built on top — instrumentation happens at each driver's own seam
// (middleware.WithTracing for HTTP, postgres.WithTracers for pgx, …), all
// reading spans from the same global TracerProvider this package sets.
package observability

// Config controls OTel SDK initialization. The zero value is a fully valid,
// no-op configuration — Init(ctx, Config{}) is always safe to call
// unconditionally, regardless of whether tracing is configured for the
// current environment.
type Config struct {
	// ServiceName becomes the resource's service.name attribute. Required
	// for a live SDK; ignored in no-op mode.
	ServiceName string
	// Endpoint is the OTLP/gRPC collector address (host:port). Empty means
	// "read OTEL_EXPORTER_OTLP_ENDPOINT instead" (the standard OTel env var);
	// both empty means no-op mode — Init constructs no SDK, starts no
	// goroutines, and leaves the global TracerProvider as OTel's default
	// no-op implementation.
	Endpoint string
}
