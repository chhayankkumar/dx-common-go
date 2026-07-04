package repository

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
	"github.com/datakaveri/dx-common-go/database/elasticsearch/indexing"
	"github.com/datakaveri/dx-common-go/database/elasticsearch/query"
)

// Repo is a typed repository over one index — the common case (search/get/
// index/bulk against one index, one document type) without every service
// hand-unmarshalling _source. A service repo embeds *Repo[T] and adds only its
// domain methods; aggregations and other custom handling stay reachable through
// Client().
type Repo[T any] struct {
	client *client.Client
	index  string
}

// New constructs a Repo bound to index.
func New[T any](c *client.Client, index string) *Repo[T] {
	return &Repo[T]{client: c, index: index}
}

// Search runs q against the repo's index and decodes hits into T.
func (r *Repo[T]) Search(ctx context.Context, q query.Query, o SearchOpts) (items []T, total int64, err error) {
	res, err := Search(ctx, r.client, r.index, query.SearchRequest{
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
// Elasticsearch's default 10 000 max_result_window. Pass the previous page's
// returned next as the following call's after; a nil next means there is no
// further page. sort must be set — search_after has no meaning without an
// explicit, deterministic sort order.
func (r *Repo[T]) SearchAfter(ctx context.Context, q query.Query, sort []map[string]string, after []any, size int) (items []T, next []any, err error) {
	res, err := Search(ctx, r.client, r.index, query.SearchRequest{
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

// Get fetches one document by id, decoded into T. Returns a dxerrors NotFound
// when absent.
func (r *Repo[T]) Get(ctx context.Context, id string) (*T, error) {
	var doc T
	if err := GetDoc(ctx, r.client, r.index, id, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// FindByID is Get under the repository-pattern name.
func (r *Repo[T]) FindByID(ctx context.Context, id string) (*T, error) { return r.Get(ctx, id) }

// Exists reports whether a document with id exists.
func (r *Repo[T]) Exists(ctx context.Context, id string) (bool, error) {
	_, err := r.Get(ctx, id)
	if err != nil {
		if client.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Index stores doc under id (empty id lets Elasticsearch assign one).
func (r *Repo[T]) Index(ctx context.Context, id string, doc T) error {
	_, err := IndexDoc(ctx, r.client, r.index, id, doc)
	return err
}

// Update applies a partial-document merge to id.
func (r *Repo[T]) Update(ctx context.Context, id string, partial any) error {
	return UpdateDoc(ctx, r.client, r.index, id, partial)
}

// Delete removes one document.
func (r *Repo[T]) Delete(ctx context.Context, id string) error {
	return DeleteDoc(ctx, r.client, r.index, id)
}

// Count returns the number of documents matching q (nil = all).
func (r *Repo[T]) Count(ctx context.Context, q query.Query) (int64, error) {
	return Count(ctx, r.client, r.index, q)
}

// ReindexTo copies this repo's index into dst (optionally transforming with a
// Painless script) — one leg of the alias/versioned-index rebuild.
func (r *Repo[T]) ReindexTo(ctx context.Context, dst, script string) error {
	return indexing.Reindex(ctx, r.client, r.index, dst, script)
}

// NewSearch starts a fluent search bound to the repo's index; decode with
// SearchAs[T](ctx, b) for typed results.
func (r *Repo[T]) NewSearch() *SearchBuilder {
	return NewSearch(r.client, r.index)
}

// Client exposes the underlying client for operations the typed repo doesn't
// wrap (aggregations, scripts, admin) — the documented escape hatch.
func (r *Repo[T]) Client() *client.Client { return r.client }

// IndexName is the index this repo is bound to.
func (r *Repo[T]) IndexName() string { return r.index }
