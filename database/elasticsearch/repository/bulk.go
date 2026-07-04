package repository

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/indexing"
)

// BulkIndex indexes docs (id → document) via the bulk API with retry on
// transport failure; per-item failures are reported in the returned
// indexing.BulkStats rather than as a call error.
func (r *Repo[T]) BulkIndex(ctx context.Context, docs map[string]T) (indexing.BulkStats, error) {
	anyDocs := make(map[string]any, len(docs))
	for id, doc := range docs {
		anyDocs[id] = doc
	}
	return indexing.BulkIndexWithRetry(ctx, r.client, r.index, anyDocs, 3)
}

// BulkDelete removes ids via the bulk API (per-item failures in BulkStats).
func (r *Repo[T]) BulkDelete(ctx context.Context, ids []string) (indexing.BulkStats, error) {
	ops := make([]indexing.BulkOp, 0, len(ids))
	for _, id := range ids {
		ops = append(ops, indexing.DeleteOp(id))
	}
	return indexing.BulkDo(ctx, r.client, r.index, ops, 3)
}
