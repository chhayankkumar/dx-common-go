package client

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Config holds Elasticsearch connection settings. It is loadable from a config
// file (mapstructure tags) except for the two runtime-only seams (Logger,
// Transport), which a service wires in code.
type Config struct {
	// Addresses is the list of node URLs, e.g. ["http://elasticsearch:9200"].
	Addresses []string `mapstructure:"addresses"`
	Username  string   `mapstructure:"username"`
	Password  string   `mapstructure:"password"`
	APIKey    string   `mapstructure:"api_key"`
	// Timeout bounds each request. Default 10s.
	Timeout time.Duration `mapstructure:"timeout"`

	// CACertPath points at a PEM bundle to trust for TLS connections
	// (self-signed / private-CA clusters). Ignored when Transport is set.
	CACertPath string `mapstructure:"ca_cert_path"`
	// InsecureSkipVerify disables TLS certificate verification — dev/test
	// only, never production. Ignored when Transport is set.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`

	// MaxRetries caps transport-level retries on 429/502/503/504 responses
	// (exponential backoff). Default 3. Set DisableRetry to turn retries off
	// entirely — e.g. for non-idempotent scripted updates where a replayed
	// request must not run twice.
	MaxRetries   int  `mapstructure:"max_retries"`
	DisableRetry bool `mapstructure:"disable_retry"`

	// MaxIdleConnsPerHost sizes the HTTP keep-alive pool per node. Go's
	// default (2) throttles concurrent ES traffic badly; services with real
	// search volume should set 32–100. Ignored when Transport is set.
	MaxIdleConnsPerHost int `mapstructure:"max_idle_conns_per_host"`

	// EnableMetrics publishes dx_elastic_requests_total{method,status} and
	// dx_elastic_request_duration_seconds{method} to the default Prometheus
	// registry (served by dx-common-go/metrics.Handler).
	EnableMetrics bool `mapstructure:"enable_metrics"`

	// EnableTracing wraps the effective transport with OpenTelemetry
	// instrumentation (otelhttp): one client span per request, and outbound
	// trace-context propagation to the cluster. It reads the TracerProvider
	// and propagator that observability.Init configured, and is a no-op when
	// no provider is set, so it is safe to leave on. Like EnableMetrics, it
	// applies even when Transport is set (only the TLS fields are ignored
	// then) — the wrap is outermost, over any explicit transport.
	EnableTracing bool `mapstructure:"enable_tracing"`

	// Logger, when set, logs each request at Debug and failures at Warn.
	// Runtime-only — not loadable from config files.
	Logger *zap.Logger `mapstructure:"-"`
	// Transport overrides the HTTP transport — the seam for tests (canned
	// responses without a server) and for instrumentation such as
	// OpenTelemetry. When set, the TLS fields above are ignored: an explicit
	// transport owns its own TLS configuration.
	Transport http.RoundTripper `mapstructure:"-"`
}
