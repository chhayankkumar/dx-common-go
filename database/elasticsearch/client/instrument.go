package client

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"
)

// Metrics are registered once per process on the default Prometheus registry
// (which dx-common-go/metrics.Handler serves), regardless of how many
// Clients are constructed. Labels are bounded — method and status only, never
// the request path — to keep cardinality flat.
var (
	metricsOnce sync.Once
	reqTotal    *prometheus.CounterVec
	reqDuration *prometheus.HistogramVec
)

func registerMetrics() {
	metricsOnce.Do(func() {
		reqTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "dx_elastic_requests_total",
			Help: "Elasticsearch requests by HTTP method and response status (status=error for transport failures).",
		}, []string{"method", "status"})
		reqDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dx_elastic_request_duration_seconds",
			Help:    "Elasticsearch request latency by HTTP method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"})
	})
}

// observedTransport wraps a RoundTripper with metrics and logging. It is the
// module's instrumentation seam: OpenTelemetry tracing will wrap the same
// transport rather than changing any call site.
type observedTransport struct {
	next    http.RoundTripper
	logger  *zap.Logger
	metrics bool
}

func newObservedTransport(next http.RoundTripper, logger *zap.Logger, metrics bool) http.RoundTripper {
	if metrics {
		registerMetrics()
	}
	return &observedTransport{next: next, logger: logger, metrics: metrics}
}

// newTracedTransport wraps next with OpenTelemetry HTTP-client instrumentation.
// It is the second half of the module's instrumentation seam (metrics/logging
// being the first): the returned transport starts a client span per request
// and injects trace-context headers, reading from OTel's global TracerProvider
// and propagator — a no-op until observability.Init configures them.
func newTracedTransport(next http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(next)
}

func (o *observedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	res, err := o.next.RoundTrip(req)
	elapsed := time.Since(start)

	status := "error"
	if err == nil {
		status = strconv.Itoa(res.StatusCode)
	}
	if o.metrics {
		reqTotal.WithLabelValues(req.Method, status).Inc()
		reqDuration.WithLabelValues(req.Method).Observe(elapsed.Seconds())
	}
	if o.logger != nil {
		if err != nil {
			o.logger.Warn("elasticsearch request failed",
				zap.String("method", req.Method),
				zap.String("path", req.URL.Path),
				zap.Duration("duration", elapsed),
				zap.Error(err))
		} else {
			o.logger.Debug("elasticsearch request",
				zap.String("method", req.Method),
				zap.String("path", req.URL.Path),
				zap.String("status", status),
				zap.Duration("duration", elapsed))
		}
	}
	return res, err
}
