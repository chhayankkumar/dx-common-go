package middleware

import (
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"
)

// Option configures the Standard composite.
type Option func(*standardConfig)

type standardConfig struct {
	tracing bool
}

// WithTracing enables OTel HTTP-server instrumentation (otelhttp) as the
// outermost middleware, reading from whatever TracerProvider
// observability.Init configured. A no-op wrapper when Init hasn't run (or
// ran with no endpoint configured) — OTel's default global TracerProvider is
// itself a no-op, so this is always safe to enable.
func WithTracing() Option {
	return func(c *standardConfig) { c.tracing = true }
}

// Standard is the options-based successor to StandardStack — additive:
// StandardStack is untouched and keeps working for every existing consumer.
// New cross-cutting capabilities (tracing today) are opt-in via Option
// rather than widening StandardStack's fixed signature.
func Standard(logger *zap.Logger, timeout time.Duration, opts ...Option) func(chi.Router) {
	cfg := &standardConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return func(r chi.Router) {
		if cfg.tracing {
			r.Use(otelhttp.NewMiddleware("http.server"))
		}
		r.Use(RequestID())
		r.Use(chimw.RealIP)
		r.Use(Logger(logger))
		r.Use(CORS(DefaultCORSConfig()))
		r.Use(Compression())
		r.Use(chimw.Recoverer)
		r.Use(chimw.Timeout(timeout))
	}
}
