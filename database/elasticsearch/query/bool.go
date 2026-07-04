package query

// BoolBuilder composes bool queries (must/should/must_not/filter), the Go
// equivalent of a decorator chain accumulating filter buckets.
type BoolBuilder struct {
	must               []Query
	should             []Query
	mustNot            []Query
	filter             []Query
	minimumShouldMatch any
}

// Bool starts a bool query.
func Bool() *BoolBuilder { return &BoolBuilder{} }

func (b *BoolBuilder) Must(qs ...Query) *BoolBuilder   { b.must = append(b.must, qs...); return b }
func (b *BoolBuilder) Should(qs ...Query) *BoolBuilder { b.should = append(b.should, qs...); return b }
func (b *BoolBuilder) MustNot(qs ...Query) *BoolBuilder {
	b.mustNot = append(b.mustNot, qs...)
	return b
}
func (b *BoolBuilder) Filter(qs ...Query) *BoolBuilder { b.filter = append(b.filter, qs...); return b }

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
