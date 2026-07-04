package elastic

import "context"

// SearchBuilder is the fluent entry point for searches — bool composition,
// sorting, pagination, aggregations, highlighting, suggesters, KNN, and PIT
// in one chain, without hand-assembling a SearchRequest:
//
//	res, err := es.NewSearch("catalogue").
//	    Filter(Term("status", "ACTIVE")).
//	    Filter(Term("provider", providerID)).
//	    Must(Match("description", keyword)).
//	    SortDesc("createdAt").
//	    Page(page, size).
//	    Do(ctx)
//
// Typed decoding: items, total, err := SearchAs[domain.Item](ctx, builder),
// or start from a typed repo with repo.NewSearch().
type SearchBuilder struct {
	client  *Client
	indices []string

	boolQ    *BoolBuilder
	rawQuery Query

	req SearchRequest
}

// NewSearch starts a fluent search over one or more indices (none = all, or
// the PIT's indices once PIT is set).
func (c *Client) NewSearch(indices ...string) *SearchBuilder {
	return &SearchBuilder{client: c, indices: indices}
}

func (b *SearchBuilder) ensureBool() *BoolBuilder {
	if b.boolQ == nil {
		b.boolQ = Bool()
	}
	return b.boolQ
}

// Must adds scoring AND clauses.
func (b *SearchBuilder) Must(qs ...Query) *SearchBuilder { b.ensureBool().Must(qs...); return b }

// Should adds OR clauses.
func (b *SearchBuilder) Should(qs ...Query) *SearchBuilder { b.ensureBool().Should(qs...); return b }

// MustNot adds exclusion clauses.
func (b *SearchBuilder) MustNot(qs ...Query) *SearchBuilder { b.ensureBool().MustNot(qs...); return b }

// Filter adds non-scoring AND clauses — the cacheable filter context; prefer
// it over Must for exact/term/range constraints.
func (b *SearchBuilder) Filter(qs ...Query) *SearchBuilder { b.ensureBool().Filter(qs...); return b }

// Query sets a full query directly, replacing bool composition (use for a
// single query or a pre-built FunctionScore).
func (b *SearchBuilder) Query(q Query) *SearchBuilder { b.rawQuery = q; return b }

// SortAsc / SortDesc append sort keys in call order.
func (b *SearchBuilder) SortAsc(field string) *SearchBuilder {
	b.req.Sort = append(b.req.Sort, map[string]string{field: "asc"})
	return b
}

func (b *SearchBuilder) SortDesc(field string) *SearchBuilder {
	b.req.Sort = append(b.req.Sort, map[string]string{field: "desc"})
	return b
}

// Page applies 1-based page/size pagination (from = (page-1)*size). For
// result sets that can exceed 10 000 hits use SearchAfter/PIT instead.
func (b *SearchBuilder) Page(page, size int) *SearchBuilder {
	if page < 1 {
		page = 1
	}
	b.req.From = (page - 1) * size
	b.req.Size = size
	return b
}

// From / Size set raw offset pagination.
func (b *SearchBuilder) From(from int) *SearchBuilder { b.req.From = from; return b }
func (b *SearchBuilder) Size(size int) *SearchBuilder { b.req.Size = size; return b }

// SearchAfter continues from the previous page's last hit Sort values.
func (b *SearchBuilder) SearchAfter(cursor []any) *SearchBuilder {
	b.req.SearchAfter = cursor
	return b
}

// Source limits the returned _source fields.
func (b *SearchBuilder) Source(includes ...string) *SearchBuilder {
	b.req.SourceIncludes = includes
	return b
}

// ExcludeSource omits _source fields.
func (b *SearchBuilder) ExcludeSource(excludes ...string) *SearchBuilder {
	b.req.SourceExcludes = excludes
	return b
}

// Agg attaches a named aggregation.
func (b *SearchBuilder) Agg(name string, agg Agg) *SearchBuilder {
	if b.req.Aggregations == nil {
		b.req.Aggregations = map[string]Agg{}
	}
	b.req.Aggregations[name] = agg
	return b
}

// AggsOnly requests zero hits — aggregation-only searches.
func (b *SearchBuilder) AggsOnly() *SearchBuilder { b.req.SizeZero = true; b.req.Size = 0; return b }

// Highlight requests highlighted fragments.
func (b *SearchBuilder) Highlight(h Highlight) *SearchBuilder { b.req.Highlight = &h; return b }

// Suggest attaches a named suggester.
func (b *SearchBuilder) Suggest(name string, s Suggester) *SearchBuilder {
	if b.req.Suggest == nil {
		b.req.Suggest = map[string]Suggester{}
	}
	b.req.Suggest[name] = s
	return b
}

// KNN adds an approximate nearest-neighbour clause (hybrid search when
// combined with Must/Filter/Query).
func (b *SearchBuilder) KNN(k KNN) *SearchBuilder { b.req.KNN = append(b.req.KNN, k); return b }

// PIT pins the search to an open point-in-time.
func (b *SearchBuilder) PIT(id, keepAlive string) *SearchBuilder {
	pit := PIT{ID: id, KeepAlive: keepAlive}
	b.req.PIT = &pit
	return b
}

// TrackTotal forces exact hit counting past 10 000.
func (b *SearchBuilder) TrackTotal() *SearchBuilder { b.req.TrackTotalHits = true; return b }

// Request renders the accumulated SearchRequest (exposed for reuse in
// Client.Scroll or tests).
func (b *SearchBuilder) Request() SearchRequest {
	req := b.req
	switch {
	case b.rawQuery != nil:
		req.Query = b.rawQuery
	case b.boolQ != nil:
		req.Query = b.boolQ.Build()
	}
	return req
}

// Do executes the search.
func (b *SearchBuilder) Do(ctx context.Context) (*SearchResult, error) {
	return b.client.SearchMulti(ctx, b.indices, b.Request())
}

// Count executes a count for the accumulated query (pagination/aggs ignored).
func (b *SearchBuilder) Count(ctx context.Context) (int64, error) {
	index := ""
	if len(b.indices) > 0 {
		index = b.indices[0]
	}
	return b.client.Count(ctx, index, b.Request().Query)
}

// SearchAs runs the builder and decodes hits into T.
func SearchAs[T any](ctx context.Context, b *SearchBuilder) ([]T, int64, error) {
	res, err := b.Do(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := HitsAs[T](res)
	if err != nil {
		return nil, 0, err
	}
	return items, res.Total, nil
}
