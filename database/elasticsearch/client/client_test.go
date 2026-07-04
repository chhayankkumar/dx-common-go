package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient spins up a fake Elasticsearch on httptest and returns a Client
// wired to it. handler receives every request except the startup ping. Every
// response carries the X-Elastic-Product header — the official client refuses
// to talk to a server without it ("unknown product"), so any ES mock must set it.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		if r.URL.Path == "/" { // startup ping
			_, _ = w.Write([]byte(`{}`))
			return
		}
		handler(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New(Config{Addresses: []string{srv.URL}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New against fake ES: %v", err)
	}
	return c
}

func TestHealthCheck(t *testing.T) {
	status := "yellow"
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_cluster/health" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	})

	if err := c.HealthCheck(context.Background()); err != nil {
		t.Fatalf("yellow cluster should be healthy (single-node dev is always yellow): %v", err)
	}
	status = "red"
	if err := c.HealthCheck(context.Background()); err == nil {
		t.Fatal("red cluster should be unhealthy")
	}
}

func TestRetryOn503(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"type":"unavailable","reason":"shard init"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"green"}`))
	})

	status, err := c.ClusterHealth(context.Background())
	if err != nil {
		t.Fatalf("ClusterHealth should have succeeded after retry: %v", err)
	}
	if status != "green" || calls.Load() < 2 {
		t.Fatalf("status=%q calls=%d — expected a transparent retry then success", status, calls.Load())
	}
}

// roundTripFunc adapts a function into an http.RoundTripper — the canned-
// response mocking path that needs no server at all.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportInjectionMock(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		if strings.HasSuffix(r.URL.Path, "/_cluster/health") {
			body = `{"status":"green"}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":      []string{"application/json"},
				"X-Elastic-Product": []string{"Elasticsearch"}, // required by the official client's product check
			},
			Body:    io.NopCloser(strings.NewReader(body)),
			Request: r,
		}, nil
	})

	c, err := New(Config{Addresses: []string{"http://mock:9200"}, Transport: rt})
	if err != nil {
		t.Fatalf("New with injected transport: %v", err)
	}
	status, err := c.ClusterHealth(context.Background())
	if err != nil || status != "green" {
		t.Fatalf("mocked ClusterHealth = (%q, %v), want (green, nil)", status, err)
	}
}
