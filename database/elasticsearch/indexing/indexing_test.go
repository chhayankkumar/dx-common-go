package indexing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		handler(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := client.New(client.Config{Addresses: []string{srv.URL}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("client.New against fake ES: %v", err)
	}
	return c
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

	stats, err := BulkDo(context.Background(), c, "things", []BulkOp{
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

// sliceSource yields pre-batched docs — a trivial Source for testing Sync.
type sliceSource struct {
	batches [][]Doc
	i       int
}

func (s *sliceSource) Next(_ context.Context) ([]Doc, bool, error) {
	if s.i >= len(s.batches) {
		return nil, true, nil
	}
	b := s.batches[s.i]
	s.i++
	return b, s.i >= len(s.batches), nil
}

func TestSyncDrainsSource(t *testing.T) {
	var batches int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_bulk" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		batches++
		_, _ = w.Write([]byte(`{"errors":false,"items":[{"index":{"_id":"x"}},{"index":{"_id":"y"}}]}`))
	})

	src := &sliceSource{batches: [][]Doc{
		{{ID: "1", Body: map[string]any{"n": 1}}, {ID: "2", Body: map[string]any{"n": 2}}},
		{{ID: "3", Body: map[string]any{"n": 3}}, {ID: "4", Body: map[string]any{"n": 4}}},
	}}
	rep, err := Sync(context.Background(), c, src, SyncConfig{Index: "things"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if rep.Batches != 2 || rep.Indexed != 4 || rep.Failed != 0 {
		t.Fatalf("report = %+v, want 2 batches / 4 indexed / 0 failed", rep)
	}
	if batches != 2 {
		t.Fatalf("expected 2 bulk calls, got %d", batches)
	}
}

func TestWorkerRunsAndStops(t *testing.T) {
	runs := make(chan struct{}, 4)
	w := &Worker{
		Name:       "test",
		Interval:   5 * time.Millisecond,
		RunOnStart: true,
		Job: func(context.Context) error {
			runs <- struct{}{}
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	// RunOnStart guarantees at least one run promptly.
	select {
	case <-runs:
	case <-time.After(time.Second):
		t.Fatal("worker did not run within 1s")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start should return ctx.Err() after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorkerValidatesConfig(t *testing.T) {
	if err := (&Worker{Name: "x", Job: func(context.Context) error { return nil }}).Start(context.Background()); err == nil {
		t.Fatal("Worker with zero Interval should fail fast")
	}
	if err := (&Worker{Name: "x", Interval: time.Second}).Start(context.Background()); err == nil {
		t.Fatal("Worker with nil Job should fail fast")
	}
}
