package resilience

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastPolicy retries quickly so tests don't sleep.
func fastPolicy(attempts int) Policy {
	return NewPolicy(WithMaxAttempts(attempts), WithBaseDelay(time.Millisecond), WithJitter(false))
}

func TestHTTPRetriesTransientStatus(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewHTTPClient(WithPolicy(fastPolicy(3)))
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("server hits = %d, want 3 (two retries)", got)
	}
}

func TestHTTPDoesNotRetryClientError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewHTTPClient(WithPolicy(fastPolicy(3)))
	resp, _ := client.Get(srv.URL)
	if resp != nil {
		resp.Body.Close()
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1 (404 not retried)", got)
	}
}

func TestHTTPDoesNotRetryNonIdempotentByDefault(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewHTTPClient(WithPolicy(fastPolicy(3)))
	resp, _ := client.Post(srv.URL, "text/plain", strings.NewReader("x"))
	if resp != nil {
		resp.Body.Close()
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1 (POST not retried by default)", got)
	}
}

func TestHTTPRetriesPOSTWithBodyReplay(t *testing.T) {
	var hits int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewHTTPClient(WithPolicy(fastPolicy(3)), WithRetryMethods(http.MethodPost))
	resp, err := client.Post(srv.URL, "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(bodies) != 3 {
		t.Fatalf("status=%d bodies=%v", resp.StatusCode, bodies)
	}
	for i, b := range bodies {
		if b != "payload" {
			t.Fatalf("body replay %d = %q, want payload", i, b)
		}
	}
}

func TestHTTPBreakerOpensAfterFailures(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	breaker := NewCircuitBreaker(WithFailureThreshold(1), WithCooldown(time.Minute))
	client := NewHTTPClient(WithPolicy(fastPolicy(1)), WithBreaker(breaker))

	// First request: one 503 → breaker trips.
	resp, _ := client.Get(srv.URL)
	if resp != nil {
		resp.Body.Close()
	}
	// Second request: breaker open → no server hit, ErrOpen surfaces.
	before := atomic.LoadInt32(&hits)
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected an error while breaker is open")
	}
	if atomic.LoadInt32(&hits) != before {
		t.Fatal("breaker-open request must not reach the server")
	}
}

func TestRetryAfterHeaderParsing(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "5")
	if got := retryAfter(resp, time.Now()); got != 5*time.Second {
		t.Fatalf("delta-seconds Retry-After = %v, want 5s", got)
	}
	now := time.Unix(1000, 0)
	resp.Header.Set("Retry-After", now.Add(3*time.Second).UTC().Format(http.TimeFormat))
	if got := retryAfter(resp, now); got < 2*time.Second || got > 3*time.Second {
		t.Fatalf("http-date Retry-After = %v, want ~3s", got)
	}
}
