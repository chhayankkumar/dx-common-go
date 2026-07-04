package query

// RangeBuilder builds range queries field-by-field — the primary filter-context
// constructor for numeric and date bounds.
type RangeBuilder struct {
	field string
	body  map[string]any
}

// Range starts a range query on field, e.g.
// query.Range("created_at").Gte("2026-01-01").Lt("2026-02-01").Build().
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
