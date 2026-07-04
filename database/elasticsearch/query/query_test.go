package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestBoolQuerySerialization(t *testing.T) {
	q := Bool().
		Must(Match("title", "solar pump")).
		Filter(Term("status", "ACTIVE"), Terms("category", []string{"A", "B"})).
		MustNot(Exists("deleted_at")).
		MinimumShouldMatch(1).
		Build()

	got := mustJSON(t, q)
	want := `{"bool":{"filter":[{"term":{"status":{"value":"ACTIVE"}}},{"terms":{"category":["A","B"]}}],"minimum_should_match":1,"must":[{"match":{"title":{"query":"solar pump"}}}],"must_not":[{"exists":{"field":"deleted_at"}}]}}`
	if got != want {
		t.Fatalf("bool query mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestRangeQuerySerialization(t *testing.T) {
	q := Range("created_at").Gte("2026-01-01").Lt("2026-02-01").Build()
	got := mustJSON(t, q)
	want := `{"range":{"created_at":{"gte":"2026-01-01","lt":"2026-02-01"}}}`
	if got != want {
		t.Fatalf("range mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSearchRequestBody(t *testing.T) {
	req := SearchRequest{
		Query:          MatchAll(),
		Size:           10,
		From:           20,
		Sort:           []map[string]string{{"created_at": "desc"}},
		SourceIncludes: []string{"id", "name"},
		Aggregations:   map[string]Agg{"by_status": TermsAgg("status", 5)},
	}
	got := mustJSON(t, req.Body())
	want := `{"_source":{"includes":["id","name"]},"aggs":{"by_status":{"terms":{"field":"status","size":5}}},"from":20,"query":{"match_all":{}},"size":10,"sort":[{"created_at":"desc"}]}`
	if got != want {
		t.Fatalf("search body mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestGeoQuerySerialization(t *testing.T) {
	cases := map[string]struct {
		q    Query
		want string
	}{
		"bbox": {
			GeoBoundingBox("location", 13.1, 77.5, 12.9, 77.7),
			`{"geo_bounding_box":{"location":{"bottom_right":{"lat":12.9,"lon":77.7},"top_left":{"lat":13.1,"lon":77.5}}}}`,
		},
		"distance": {
			GeoDistance("location", 12.97, 77.59, "5km"),
			`{"geo_distance":{"distance":"5km","location":{"lat":12.97,"lon":77.59}}}`,
		},
		"shape": {
			GeoShape("geometry", "polygon", [][][]float64{{{77.5, 13.0}, {77.6, 13.0}, {77.6, 13.1}, {77.5, 13.0}}}, "intersects"),
			`{"geo_shape":{"geometry":{"relation":"intersects","shape":{"coordinates":[[[77.5,13],[77.6,13],[77.6,13.1],[77.5,13]]],"type":"polygon"}}}}`,
		},
	}
	for name, tc := range cases {
		if got := mustJSON(t, tc.q); got != tc.want {
			t.Fatalf("%s geo mismatch:\n got: %s\nwant: %s", name, got, tc.want)
		}
	}
}

func TestScriptScoreSerialization(t *testing.T) {
	q := ScriptScore(MatchAll(), "cosineSimilarity(params.qv, '_word_vector') + 1.0", map[string]any{"qv": []float64{0.1, 0.2}})
	want := `{"script_score":{"query":{"match_all":{}},"script":{"params":{"qv":[0.1,0.2]},"source":"cosineSimilarity(params.qv, '_word_vector') + 1.0"}}}`
	if got := mustJSON(t, q); got != want {
		t.Fatalf("script_score mismatch:\n got: %s\nwant: %s", got, want)
	}
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
			got := mustJSON(t, tc.q)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("%s: rendered %s missing %s", tc.name, got, want)
				}
			}
		})
	}
}

func TestKNNBodyDefaults(t *testing.T) {
	body := KNN{Field: "embedding", QueryVector: []float32{0.1, 0.2}, K: 10}.Body()
	if body["num_candidates"] != 100 {
		t.Fatalf("num_candidates default should be 10*K, got %v", body["num_candidates"])
	}
}
