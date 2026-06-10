package elastic

// Query DSL builders. Each constructor returns a Query (a JSON-serializable
// map fragment), mirroring the Java QueryModel's query types without the
// giant switch: composition happens in Go code, serialization is plain JSON.
//
//	q := elastic.Bool().
//	    Must(elastic.Match("title", "solar pump")).
//	    Filter(elastic.Term("status", "ACTIVE")).
//	    MustNot(elastic.Exists("deleted_at")).
//	    Build()

// Query is one Elasticsearch query-DSL fragment.
type Query map[string]any

// MatchAll matches every document.
func MatchAll() Query {
	return Query{"match_all": map[string]any{}}
}

// Match performs full-text matching on one field.
func Match(field string, value any) Query {
	return Query{"match": map[string]any{field: map[string]any{"query": value}}}
}

// MatchFuzzy is Match with a fuzziness setting (e.g. "AUTO", "2").
func MatchFuzzy(field string, value any, fuzziness string) Query {
	return Query{"match": map[string]any{field: map[string]any{"query": value, "fuzziness": fuzziness}}}
}

// MatchPhrase matches the exact phrase.
func MatchPhrase(field string, value any) Query {
	return Query{"match_phrase": map[string]any{field: map[string]any{"query": value}}}
}

// MultiMatch searches value across several fields (supports boosts like "name^3").
func MultiMatch(value any, fields ...string) Query {
	return Query{"multi_match": map[string]any{"query": value, "fields": fields}}
}

// Term matches an exact keyword value.
func Term(field string, value any) Query {
	return Query{"term": map[string]any{field: map[string]any{"value": value}}}
}

// Terms matches any of the exact values.
func Terms[T any](field string, values []T) Query {
	return Query{"terms": map[string]any{field: values}}
}

// Exists matches documents where field has a value.
func Exists(field string) Query {
	return Query{"exists": map[string]any{"field": field}}
}

// Wildcard matches a pattern with * and ? wildcards.
func Wildcard(field, pattern string, caseInsensitive bool) Query {
	body := map[string]any{"value": pattern}
	if caseInsensitive {
		body["case_insensitive"] = true
	}
	return Query{"wildcard": map[string]any{field: body}}
}

// QueryString runs a Lucene query-string search, optionally limited to fields.
func QueryString(queryStr string, fields ...string) Query {
	body := map[string]any{"query": queryStr}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	return Query{"query_string": body}
}

// RangeBuilder builds range queries field-by-field.
type RangeBuilder struct {
	field string
	body  map[string]any
}

// Range starts a range query on field, e.g.
// elastic.Range("created_at").Gte("2026-01-01").Lt("2026-02-01").Build().
func Range(field string) *RangeBuilder {
	return &RangeBuilder{field: field, body: map[string]any{}}
}

func (r *RangeBuilder) Gte(v any) *RangeBuilder { r.body["gte"] = v; return r }
func (r *RangeBuilder) Gt(v any) *RangeBuilder  { r.body["gt"] = v; return r }
func (r *RangeBuilder) Lte(v any) *RangeBuilder { r.body["lte"] = v; return r }
func (r *RangeBuilder) Lt(v any) *RangeBuilder  { r.body["lt"] = v; return r }

// Format sets the date format for date ranges.
func (r *RangeBuilder) Format(f string) *RangeBuilder { r.body["format"] = f; return r }

// Build finalizes the range query.
func (r *RangeBuilder) Build() Query {
	return Query{"range": map[string]any{r.field: r.body}}
}

// BoolBuilder composes bool queries (must/should/must_not/filter), the Go
// equivalent of the Java decorator chain accumulating FilterType buckets.
type BoolBuilder struct {
	must               []Query
	should             []Query
	mustNot            []Query
	filter             []Query
	minimumShouldMatch any
}

// Bool starts a bool query.
func Bool() *BoolBuilder { return &BoolBuilder{} }

func (b *BoolBuilder) Must(qs ...Query) *BoolBuilder    { b.must = append(b.must, qs...); return b }
func (b *BoolBuilder) Should(qs ...Query) *BoolBuilder  { b.should = append(b.should, qs...); return b }
func (b *BoolBuilder) MustNot(qs ...Query) *BoolBuilder { b.mustNot = append(b.mustNot, qs...); return b }
func (b *BoolBuilder) Filter(qs ...Query) *BoolBuilder  { b.filter = append(b.filter, qs...); return b }

// MinimumShouldMatch sets the minimum_should_match parameter (int or string).
func (b *BoolBuilder) MinimumShouldMatch(v any) *BoolBuilder {
	b.minimumShouldMatch = v
	return b
}

// Build finalizes the bool query.
func (b *BoolBuilder) Build() Query {
	body := map[string]any{}
	if len(b.must) > 0 {
		body["must"] = b.must
	}
	if len(b.should) > 0 {
		body["should"] = b.should
	}
	if len(b.mustNot) > 0 {
		body["must_not"] = b.mustNot
	}
	if len(b.filter) > 0 {
		body["filter"] = b.filter
	}
	if b.minimumShouldMatch != nil {
		body["minimum_should_match"] = b.minimumShouldMatch
	}
	return Query{"bool": body}
}

// ── aggregations ────────────────────────────────────────────────────────────

// Agg is one named aggregation fragment.
type Agg map[string]any

// TermsAgg buckets documents by exact field values.
func TermsAgg(field string, size int) Agg {
	body := map[string]any{"field": field}
	if size > 0 {
		body["size"] = size
	}
	return Agg{"terms": body}
}

// MetricAgg builds a single-value metric aggregation: kind is one of
// "avg", "sum", "min", "max", "cardinality", "value_count".
func MetricAgg(kind, field string) Agg {
	return Agg{kind: map[string]any{"field": field}}
}

// DateHistogramAgg buckets documents by calendar interval ("day", "month", …).
func DateHistogramAgg(field, calendarInterval string) Agg {
	return Agg{"date_histogram": map[string]any{"field": field, "calendar_interval": calendarInterval}}
}
