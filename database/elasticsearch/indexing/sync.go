package indexing

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
)

// Doc is one document to index: an id and its body. An empty ID lets the caller
// rely on Elasticsearch-assigned ids, but a Syncer works best with stable ids
// so re-runs upsert rather than duplicate.
type Doc struct {
	ID   string
	Body any
}

// Source yields documents in batches for a Syncer to index. Next returns the
// next batch and whether the stream is exhausted (done). The final non-empty
// batch may be returned together with done=true, or as the batch before an
// empty done=true call — Sync handles both. Implement it over any origin: a
// Postgres cursor, a paged upstream API, a file, a Kafka partition.
//
// A Source is stateful and single-use; construct a fresh one per Sync run.
type Source interface {
	Next(ctx context.Context) (batch []Doc, done bool, err error)
}

// SyncConfig tunes a Sync run.
type SyncConfig struct {
	// Index is the destination index (or alias). Required.
	Index string
	// MaxAttempts is the per-batch bulk transport-retry cap (default 3).
	MaxAttempts int
	// OnBatch, if set, is called after each batch with that batch's stats —
	// a progress/metrics hook.
	OnBatch func(BulkStats)
}

// Report aggregates the outcome of a full Sync run.
type Report struct {
	Batches int
	Indexed int
	Failed  int
	Errors  []ItemError
}

// Sync drains src, bulk-indexing each batch into cfg.Index. It stops and
// returns at the first transport-level failure (after BulkDo's own retries are
// exhausted) or context cancellation; per-item mapping failures do not stop the
// run — they accumulate in the Report so a single bad document never blocks the
// stream. This is the generic backbone for "keep an index populated from a
// source of truth": DB→ES backfills, reindex-from-origin, catch-up jobs.
func Sync(ctx context.Context, c *client.Client, src Source, cfg SyncConfig) (Report, error) {
	var rep Report
	for {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		batch, done, err := src.Next(ctx)
		if err != nil {
			return rep, err
		}
		if len(batch) > 0 {
			docs := make(map[string]any, len(batch))
			ops := make([]BulkOp, 0, len(batch))
			for _, d := range batch {
				if d.ID != "" {
					docs[d.ID] = d.Body
				} else {
					ops = append(ops, IndexOp("", d.Body))
				}
			}
			for id, body := range docs {
				ops = append(ops, IndexOp(id, body))
			}
			stats, err := BulkDo(ctx, c, cfg.Index, ops, cfg.MaxAttempts)
			if err != nil {
				return rep, err
			}
			rep.Batches++
			rep.Indexed += stats.Indexed
			rep.Failed += stats.Failed
			rep.Errors = append(rep.Errors, stats.Errors...)
			if cfg.OnBatch != nil {
				cfg.OnBatch(stats)
			}
		}
		if done {
			return rep, nil
		}
	}
}
