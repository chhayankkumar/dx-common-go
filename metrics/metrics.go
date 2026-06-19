package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns an http.Handler that serves metrics in Prometheus
// exposition format. Includes default process and Go runtime collectors.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HandlerFor returns an http.Handler serving from a custom registry.
func HandlerFor(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// RequestMetrics tracks HTTP request count and duration.
type RequestMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewRequestMetrics creates and registers HTTP request metrics under the
// given namespace (e.g. "acl", "authz"). Pass "" for no namespace prefix.
func NewRequestMetrics(namespace string) *RequestMetrics {
	rm := &RequestMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total HTTP requests.",
		}, []string{"code", "method"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"code", "method"}),
	}
	prometheus.MustRegister(rm.requests, rm.duration)
	return rm
}

// RecordRequest records a completed HTTP request.
func (rm *RequestMetrics) RecordRequest(method string, statusCode int, duration time.Duration) {
	code := strconv.Itoa(statusCode)
	rm.requests.WithLabelValues(code, method).Inc()
	rm.duration.WithLabelValues(code, method).Observe(duration.Seconds())
}
