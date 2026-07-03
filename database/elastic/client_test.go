package elastic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient spins up a fake Elasticsearch on httptest and returns a
// Client wired to it. handler receives every request except the startup ping.
// Every response carries the X-Elastic-Product header — the official client
// refuses to talk to a server without it ("unknown product"), so any ES mock
// must set it.
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

	c, err := NewClient(Config{Addresses: []string{srv.URL}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient against fake ES: %v", err)
	}
	return c
}

func TestSearchHighlightRoundTrip(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/things/_search" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte(`{
			"hits": {"total": {"value": 1}, "hits": [
				{"_id": "a", "_source": {"name": "solar farm"},
				 "highlight": {"name": ["<mark>solar</mark> farm"]}}
			]}
		}`))
	})

	res, err := c.Search(context.Background(), "things", SearchRequest{
		Query: Match("name", "solar"),
		Highlight: &Highlight{
			Fields:       []string{"name"},
			PreTags:      []string{"<mark>"},
			PostTags:     []string{"</mark>"},
			FragmentSize: 120,
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	hl, ok := gotBody["highlight"].(map[string]any)
	if !ok {
		t.Fatalf("request body missing highlight block: %v", gotBody)
	}
	if _, ok := hl["fields"].(map[string]any)["name"]; !ok {
		t.Fatalf("highlight.fields missing name: %v", hl)
	}
	if hl["fragment_size"] != float64(120) {
		t.Fatalf("fragment_size not rendered: %v", hl)
	}
	if got := res.Hits[0].Highlight["name"][0]; got != "<mark>solar</mark> farm" {
		t.Fatalf("hit highlight not decoded, got %q", got)
	}
}

func TestEnsureIndex(t *testing.T) {
	var created atomic.Bool
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/widgets":
			if created.Load() {
				_, _ = w.Write([]byte(`{"widgets":{}}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"type":"index_not_found_exception","reason":"no such index"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/widgets":
			created.Store(true)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	ctx := context.Background()
	madeIt, err := c.EnsureIndex(ctx, "widgets", nil)
	if err != nil || !madeIt {
		t.Fatalf("first EnsureIndex = (%v, %v), want (true, nil)", madeIt, err)
	}
	madeIt, err = c.EnsureIndex(ctx, "widgets", nil)
	if err != nil || madeIt {
		t.Fatalf("second EnsureIndex = (%v, %v), want (false, nil)", madeIt, err)
	}
}

func TestBulkDoMixedOps(t *testing.T) {
	var ndjson string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		ndjson = string(raw)
		_, _ = w.Write([]byte(`{"errors": true, "items": [
			{"index":  {"_id": "a"}},
			{"update": {"_id": "b", "error": {"type": "document_missing_exception", "reason": "not found"}}},
			{"delete": {"_id": "c"}}
		]}`))
	})

	stats, err := c.BulkDo(context.Background(), "things", []BulkOp{
		IndexOp("a", map[string]any{"name": "one"}),
		UpdateOp("b", map[string]any{"name": "two"}),
		DeleteOp("c"),
	}, 1)
	if err != nil {
		t.Fatalf("BulkDo: %v", err)
	}
	if stats.Indexed != 2 || stats.Failed != 1 {
		t.Fatalf("stats = %+v, want 2 ok / 1 failed", stats)
	}
	if stats.Errors[0].ID != "b" || !strings.Contains(stats.Errors[0].Reason, "document_missing_exception") {
		t.Fatalf("unexpected item error: %+v", stats.Errors[0])
	}

	lines := strings.Split(strings.TrimSpace(ndjson), "\n")
	if len(lines) != 5 { // index meta+doc, update meta+doc, delete meta only
		t.Fatalf("expected 5 NDJSON lines, got %d:\n%s", len(lines), ndjson)
	}
	if !strings.Contains(lines[2], `"update"`) || !strings.Contains(lines[3], `"doc"`) {
		t.Fatalf("update op not rendered as meta + {\"doc\": …}:\n%s", ndjson)
	}
	if !strings.Contains(lines[4], `"delete"`) {
		t.Fatalf("delete op should be the final meta-only line:\n%s", ndjson)
	}
}

func TestHealthCheck(t *testing.T) {
	status := "yellow"
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_cluster/health" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
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
		_, _ = w.Write([]byte(`{"count": 7}`))
	})

	n, err := c.Count(context.Background(), "things", nil)
	if err != nil {
		t.Fatalf("Count should have succeeded after retry: %v", err)
	}
	if n != 7 || calls.Load() < 2 {
		t.Fatalf("count=%d calls=%d — expected a transparent retry then success", n, calls.Load())
	}
}

// roundTripFunc adapts a function into an http.RoundTripper — the canned-
// response mocking path that needs no server at all.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportInjectionMock(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		if strings.HasSuffix(r.URL.Path, "/_count") {
			body = `{"count": 42}`
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

	c, err := NewClient(Config{Addresses: []string{"http://mock:9200"}, Transport: rt})
	if err != nil {
		t.Fatalf("NewClient with injected transport: %v", err)
	}
	n, err := c.Count(context.Background(), "anything", nil)
	if err != nil || n != 42 {
		t.Fatalf("mocked Count = (%d, %v), want (42, nil)", n, err)
	}
}
