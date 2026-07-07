package observability

import (
	"context"
	"fmt"
	"os"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"

	"go.opentelemetry.io/otel/sdk/resource"
)

var initOnce sync.Once

// Init wires a TracerProvider and text-map propagator onto OTel's global
// state, or does nothing when no endpoint is configured (cfg.Endpoint and
// OTEL_EXPORTER_OTLP_ENDPOINT both empty) — services call this
// unconditionally at startup, with zero risk in environments that don't run
// a collector. The returned shutdown flushes and closes the exporter; call
// it during graceful shutdown (deferred right after Init).
//
// Safe to call more than once per process — only the first call takes
// effect, guarding against double-init silently clobbering the global
// TracerProvider a second time. Later calls return a no-op shutdown.
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }

	var initErr error
	initialized := false
	initOnce.Do(func() {
		initialized = true
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		}
		if endpoint == "" {
			shutdown = noop
			return
		}

		exporter, exportErr := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
		if exportErr != nil {
			initErr = fmt.Errorf("observability.Init: create OTLP exporter: %w", exportErr)
			shutdown = noop
			return
		}

		res, resErr := resource.New(ctx,
			resource.WithAttributes(semconv.ServiceName(cfg.ServiceName)),
		)
		if resErr != nil {
			initErr = fmt.Errorf("observability.Init: build resource: %w", resErr)
			shutdown = noop
			return
		}

		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, propagation.Baggage{},
		))
		shutdown = tp.Shutdown
	})

	if !initialized {
		// A later call in the same process: report success with a no-op
		// shutdown rather than silently re-running (and re-clobbering) SDK
		// setup — the first call already owns the shutdown lifecycle.
		return noop, nil
	}
	if shutdown == nil {
		shutdown = noop
	}
	return shutdown, initErr
}
