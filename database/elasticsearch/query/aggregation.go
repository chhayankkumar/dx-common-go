package query

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

// FilterAgg wraps a query as a single-bucket aggregation — useful for counting
// a specific subset.
func FilterAgg(filter Query) Agg {
	return Agg{"filter": filter}
}

// Sub nests child aggregations inside a bucket aggregation, returning the outer
// agg for chaining. Example:
//
//	query.TermsAgg("tags", 20).Sub("avg_score", query.MetricAgg("avg", "score"))
func (a Agg) Sub(name string, child Agg) Agg {
	subs, ok := a["aggs"].(map[string]Agg)
	if !ok {
		subs = map[string]Agg{}
	}
	subs[name] = child
	a["aggs"] = subs
	return a
}
