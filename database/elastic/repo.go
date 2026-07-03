package elastic

import "context"

// Repo is a typed wrapper over Client for a single index — the common case
// (search/get/index/bulk against one index, one document type) without
// every service hand-unmarshalling _source. Aggregations and other
// custom _source handling stay on the underlying *Client (Repo composes it,
// doesn't replace it).
type Repo[T any] struct {
	client *Client
	index  string
}

// NewRepo constructs a Repo bound to index.
func NewRepo[T any](c *Client, index string) *Repo[T] {
	return &Repo[T]{client: c, index: index}
}

// SearchOpts carries the non-query parts of a Search call.
type SearchOpts struct {
	Size           int
	From           int
	Sort           []map[string]string
	SourceIncludes []string
	SourceExcludes []string
	TrackTotalHits bool
}

// Search runs q against the repo's index and decodes hits into T.
func (r *Repo[T]) Search(ctx context.Context, q Query, o SearchOpts) (items []T, total int64, err error) {
	res, err := r.client.Search(ctx, r.index, SearchRequest{
		Query:          q,
		Size:           o.Size,
		From:           o.From,
		Sort:           o.Sort,
		SourceIncludes: o.SourceIncludes,
		SourceExcludes: o.SourceExcludes,
		TrackTotalHits: o.TrackTotalHits,
	})
	if err != nil {
		return nil, 0, err
	}
	items, err = HitsAs[T](res)
	if err != nil {
		return nil, 0, err
	}
	return items, res.Total, nil
}

// SearchAfter is the deep-pagination form of Search, for result sets past
// Elasticsearch's default 10 000 max_result_window (DATABASE.md §8.4). Pass
// the previous page's returned next as the following call's after; a nil
// next means there is no further page. sort must be set — search_after has
// no meaning without an explicit, deterministic sort order.
func (r *Repo[T]) SearchAfter(ctx context.Context, q Query, sort []map[string]string, after []any, size int) (items []T, next []any, err error) {
	res, err := r.client.Search(ctx, r.index, SearchRequest{
		Query:       q,
		Sort:        sort,
		Size:        size,
		SearchAfter: after,
	})
	if err != nil {
		return nil, nil, err
	}
	items, err = HitsAs[T](res)
	if err != nil {
		return nil, nil, err
	}
	if len(res.Hits) > 0 {
		next = res.Hits[len(res.Hits)-1].Sort
	}
	return items, next, nil
}

// Get fetches one document by id, decoded into T. Returns a dxerrors
// NotFound when absent (via Client.GetDoc's existing 404 mapping).
func (r *Repo[T]) Get(ctx context.Context, id string) (*T, error) {
	var doc T
	if err := r.client.GetDoc(ctx, r.index, id, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Index stores doc under id (empty id lets Elasticsearch assign one).
func (r *Repo[T]) Index(ctx context.Context, id string, doc T) error {
	_, err := r.client.IndexDoc(ctx, r.index, id, doc)
	return err
}

// BulkIndex indexes docs (id → document) via the bulk API with retry on
// transport failure; per-item failures are reported in the returned
// BulkStats rather than as a call error.
func (r *Repo[T]) BulkIndex(ctx context.Context, docs map[string]T) (BulkStats, error) {
	anyDocs := make(map[string]any, len(docs))
	for id, doc := range docs {
		anyDocs[id] = doc
	}
	return r.client.BulkIndexWithRetry(ctx, r.index, anyDocs, 3)
}
