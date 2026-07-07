package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func resetDefaultRegistry(t *testing.T) {
	t.Helper()
	// Use a fresh registry per test to avoid "duplicate metric" panics.
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	prometheus.DefaultGatherer = prometheus.DefaultRegisterer.(*prometheus.Registry)
}

func TestHandler_ServesMetrics(t *testing.T) {
	h := Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerFor_CustomRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_custom_counter",
		Help: "A test counter.",
	})
	reg.MustRegister(counter)
	counter.Inc()

	h := HandlerFor(reg)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "test_custom_counter") {
		t.Fatal("expected custom counter in output")
	}
}

func TestNewRequestMetrics(t *testing.T) {
	resetDefaultRegistry(t)

	rm := NewRequestMetrics("test")

	rm.RecordRequest(http.MethodGet, 200, 50*time.Millisecond)
	rm.RecordRequest(http.MethodPost, 404, 100*time.Millisecond)
	rm.RecordRequest(http.MethodGet, 500, 200*time.Millisecond)

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	names := make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}

	if !names["test_http_requests_total"] {
		t.Fatal("expected test_http_requests_total metric")
	}
	if !names["test_http_request_duration_seconds"] {
		t.Fatal("expected test_http_request_duration_seconds metric")
	}
}

func TestNewRequestMetrics_NoNamespace(t *testing.T) {
	resetDefaultRegistry(t)

	rm := NewRequestMetrics("")
	rm.RecordRequest(http.MethodGet, 200, 10*time.Millisecond)

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	names := make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}

	if !names["http_requests_total"] {
		t.Fatal("expected http_requests_total metric (no namespace)")
	}
}
