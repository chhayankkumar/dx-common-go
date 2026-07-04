package elastic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── DSL v2 rendering (pure — no server) ─────────────────────────────────────

func renderJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestDSLv2Render(t *testing.T) {
	cases := []struct {
		name string
		q    any
		want []string
	}{
		{"fuzzy", Fuzzy("name", "solr", ""), []string{`"fuzzy"`, `"fuzziness":"AUTO"`}},
		{"regexp", Regexp("tag", "sen.*r"), []string{`"regexp"`, `"value":"sen.*r"`}},
		{"script query", ScriptQuery("doc['a'].value > params.n", map[string]any{"n": 5}), []string{`"script"`, `"params":{"n":5}`}},
		{"has_child", HasChild("answer", MatchAll(), "max"), []string{`"has_child"`, `"type":"answer"`, `"score_mode":"max"`}},
		{"has_parent", HasParent("question", Term("topic", "es")), []string{`"has_parent"`, `"parent_type":"question"`}},
		{"parent_id", ParentID("answer", "q1"), []string{`"parent_id"`, `"id":"q1"`}},
		{"function_score", FunctionScore(Match("t", "x")).
			FieldValueFactor("popularity", 1.2, "log1p", 1).
			Weight(2, Term("featured", true)).
			Decay("gauss", "createdAt", "now", "30d").
			ScoreMode("sum").BoostMode("multiply").Build(),
			[]string{`"function_score"`, `"field_value_factor"`, `"weight":2`, `"gauss"`, `"score_mode":"sum"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderJSON(t, tc.q)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("%s: rendered %s missing %s", tc.name, got, want)
				}
			}
		})
	}
}

func TestKNNBodyDefaults(t *testing.T) {
	body := KNN{Field: "embedding", QueryVector: []float32{0.1, 0.2}, K: 10}.body()
	if body["num_candidates"] != 100 {
		t.Fatalf("num_candidates default should be 10*K, got %v", body["num_candidates"])
	}
}

// ── fluent builder ──────────────────────────────────────────────────────────

func TestSearchBuilderRequest(t *testing.T) {
	c := &Client{} // Request() is pure — no connection needed
	req := c.NewSearch("catalogue").
		Filter(Term("status", "ACTIVE")).
		Filter(Term("provider", "p1")).
		Must(Match("description", "solar")).
		SortDesc("createdAt").
		Page(2, 20).
		TrackTotal().
		Request()

	if req.From != 20 || req.Size != 20 {
		t.Fatalf("Page(2,20) → from=%d size=%d, want 20/20", req.From, req.Size)
	}
	got := renderJSON(t, req.body())
	for _, want := range []string{`"bool"`, `"filter"`, `"must"`, `"track_total_hits":true`, `{"createdAt":"desc"}`} {
		if !strings.Contains(got, want) {
			t.Fatalf("builder body %s missing %s", got, want)
		}
	}
}

func TestSearchBuilderMultiIndexAndPITPaths(t *testing.T) {
	var paths []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"hits":{"total":{"value":0},"hits":[]}}`))
	})

	if _, err := c.NewSearch("a", "b").Do(context.Background()); err != nil {
		t.Fatalf("multi-index search: %v", err)
	}
	if _, err := c.NewSearch("ignored").PIT("pit-id", "1m").Do(context.Background()); err != nil {
		t.Fatalf("PIT search: %v", err)
	}
	if paths[0] != "/a,b/_search" {
		t.Fatalf("multi-index path = %s, want /a,b/_search", paths[0])
	}
	if paths[1] != "/_search" {
		t.Fatalf("PIT search path = %s, want /_search (no index)", paths[1])
	}
}

func TestSuggestFlow(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"suggest"`) {
			t.Fatalf("request missing suggest block: %s", raw)
		}
		_, _ = w.Write([]byte(`{
			"hits": {"total": {"value": 0}, "hits": []},
			"suggest": {"auto": [{"text": "sol", "options": [{"text": "solar", "score": 0.9}]}]}
		}`))
	})

	res, err := c.NewSearch("catalogue").
		Suggest("auto", CompletionSuggester("sol", "name_suggest", 5, true)).
		Do(context.Background())
	if err != nil {
		t.Fatalf("suggest search: %v", err)
	}
	if got := res.Suggest["auto"][0].Text; got != "solar" {
		t.Fatalf("suggest option = %q, want solar", got)
	}
}

// ── scroll + PIT lifecycle ──────────────────────────────────────────────────

func TestScrollFlow(t *testing.T) {
	var cleared bool
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/big/_search" && r.URL.Query().Get("scroll") == "1m":
			_, _ = w.Write([]byte(`{"_scroll_id":"s1","hits":{"total":{"value":3},"hits":[{"_id":"1","_source":{}}]}}`))
		case r.URL.Path == "/_search/scroll" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"_scroll_id":"s2","hits":{"total":{"value":3},"hits":[]}}`))
		case r.URL.Path == "/_search/scroll" && r.Method == http.MethodDelete:
			cleared = true
			_, _ = w.Write([]byte(`{"succeeded":true}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	ctx := context.Background()
	page, err := c.Scroll(ctx, "big", SearchRequest{Query: MatchAll()}, "1m")
	if err != nil || page.ScrollID != "s1" || len(page.Hits) != 1 {
		t.Fatalf("scroll open = %+v, %v", page, err)
	}
	next, err := c.ScrollNext(ctx, page.ScrollID, "1m")
	if err != nil || len(next.Hits) != 0 {
		t.Fatalf("scroll next = %+v, %v — empty page should signal exhaustion", next, err)
	}
	if err := c.ClearScroll(ctx, next.ScrollID); err != nil || !cleared {
		t.Fatalf("clear scroll: %v (cleared=%v)", err, cleared)
	}
}

func TestPITLifecycle(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_pit") && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"pit-1"}`))
		case r.URL.Path == "/_pit" && r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"succeeded":true}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	id, err := c.OpenPIT(ctx, "catalogue", "1m")
	if err != nil || id != "pit-1" {
		t.Fatalf("OpenPIT = %q, %v", id, err)
	}
	if err := c.ClosePIT(ctx, id); err != nil {
		t.Fatalf("ClosePIT: %v", err)
	}
}

// ── mapping framework ───────────────────────────────────────────────────────

func TestMappingBuilder(t *testing.T) {
	body := NewMapping().
		Dynamic("strict").
		TextWithKeyword("name").
		Keyword("status").
		Date("createdAt").
		DenseVector("embedding", 384, "").
		Join("relation", map[string][]string{"question": {"answer"}}).
		NestedField("attachments", NewMapping().Keyword("fileKey").Long("size")).
		RuntimeField("age_days", "long", "emit((System.currentTimeMillis()-doc['createdAt'].value.toInstant().toEpochMilli())/86400000)").
		DynamicTemplate("strings_as_keyword", map[string]any{"match_mapping_type": "string", "mapping": map[string]any{"type": "keyword"}}).
		CustomAnalyzer("en_text", "standard", "lowercase", "en_syn").
		Synonyms("en_syn", "tv => television").
		Normalizer("ci_sort", "lowercase").
		Shards(1, 1).
		Setting("refresh_interval", "30s").
		Build()

	if err := ValidateMapping(body); err != nil {
		t.Fatalf("ValidateMapping: %v", err)
	}
	got := renderJSON(t, body)
	for _, want := range []string{
		`"dynamic":"strict"`, `"dense_vector"`, `"dims":384`, `"similarity":"cosine"`,
		`"join"`, `"nested"`, `"runtime"`, `"dynamic_templates"`,
		`"analyzer":{"en_text"`, `"synonym_graph"`, `"normalizer"`,
		`"number_of_shards":1`, `"refresh_interval":"30s"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("mapping body missing %s:\n%s", want, got)
		}
	}
}

func TestAutoMap(t *testing.T) {
	type Attachment struct {
		FileKey string `json:"fileKey"`
		Size    int64  `json:"size"`
	}
	type Item struct {
		Name        string       `json:"name"`
		Status      string       `json:"status" es:"keyword"`
		Score       float64      `json:"score"`
		Active      bool         `json:"active"`
		CreatedAt   time.Time    `json:"createdAt"`
		Tags        []string     `json:"tags"`
		Attachments []Attachment `json:"attachments"`
		Secret      string       `json:"-"`
		Skipped     string       `json:"skipped" es:"-"`
		internal    string       //nolint:unused — proves unexported fields are skipped
	}

	body := AutoMap[Item]().Build()
	if err := ValidateMapping(body); err != nil {
		t.Fatalf("ValidateMapping: %v", err)
	}
	props := body["mappings"].(map[string]any)["properties"].(map[string]any)

	assertType := func(field, wantType string) {
		t.Helper()
		p, ok := props[field].(map[string]any)
		if !ok {
			t.Fatalf("field %s missing from automap: %v", field, props)
		}
		if p["type"] != wantType {
			t.Fatalf("field %s type = %v, want %s", field, p["type"], wantType)
		}
	}
	assertType("name", "text")
	assertType("status", "keyword") // es tag override
	assertType("score", "double")
	assertType("active", "boolean")
	assertType("createdAt", "date")
	assertType("tags", "text") // scalar slice maps like the scalar
	assertType("attachments", "nested")
	for _, absent := range []string{"Secret", "skipped", "internal"} {
		if _, ok := props[absent]; ok {
			t.Fatalf("field %s should have been skipped", absent)
		}
	}
}

// ── lifecycle orchestration ─────────────────────────────────────────────────

func TestMigrateIndexBlueGreen(t *testing.T) {
	var calls []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/_alias/cat":
			_, _ = w.Write([]byte(`{"cat-v1":{"aliases":{"cat":{}}}}`))
		case r.URL.Path == "/cat-v2" && r.Method == http.MethodGet: // EnsureIndex exists-check
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"type":"index_not_found_exception","reason":"nope"}}`))
		case r.URL.Path == "/cat-v2" && r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.URL.Path == "/_reindex":
			_, _ = w.Write([]byte(`{"total":10}`))
		case r.URL.Path == "/_aliases":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"remove"`) || !strings.Contains(string(raw), `"add"`) {
				t.Fatalf("alias swap should remove+add atomically: %s", raw)
			}
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.URL.Path == "/cat-v1" && r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	err := c.MigrateIndex(context.Background(), "cat", "cat-v2",
		NewMapping().Keyword("id").Build(), MigrateOptions{DeleteOld: true})
	if err != nil {
		t.Fatalf("MigrateIndex: %v", err)
	}
	joined := strings.Join(calls, " → ")
	for _, want := range []string{"GET /_alias/cat", "PUT /cat-v2", "POST /_reindex", "POST /_aliases", "DELETE /cat-v1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("migration sequence missing %q: %s", want, joined)
		}
	}
}

// ── repo v2 ─────────────────────────────────────────────────────────────────

func TestRepoV2(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/things/_doc/yes":
			_, _ = w.Write([]byte(`{"_source":{"name":"x"}}`))
		case r.URL.Path == "/things/_doc/no":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"type":"not_found","reason":"missing"}}`))
		case r.URL.Path == "/_bulk":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"delete"`) {
				t.Fatalf("BulkDelete should emit delete metas: %s", raw)
			}
			_, _ = w.Write([]byte(`{"errors":false,"items":[{"delete":{"_id":"a"}},{"delete":{"_id":"b"}}]}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	type thing struct {
		Name string `json:"name"`
	}
	repo := NewRepo[thing](c, "things")
	ctx := context.Background()

	if ok, err := repo.Exists(ctx, "yes"); err != nil || !ok {
		t.Fatalf("Exists(yes) = %v, %v", ok, err)
	}
	if ok, err := repo.Exists(ctx, "no"); err != nil || ok {
		t.Fatalf("Exists(no) = %v, %v — NotFound must map to false,nil", ok, err)
	}
	stats, err := repo.BulkDelete(ctx, []string{"a", "b"})
	if err != nil || stats.Indexed != 2 {
		t.Fatalf("BulkDelete = %+v, %v", stats, err)
	}
}
