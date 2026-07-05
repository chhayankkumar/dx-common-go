// Package indexing is the write-path engine of the Elasticsearch framework:
// the bulk API (mixed index/update/delete with partial-success reporting and
// transport retry), the reindex primitive, and a generic Source→bulk Syncer
// with a supervised worker loop. It builds on the client transport and the
// query DSL and imports no higher package, so the repository layer can compose
// it without a cycle.
package indexing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
	"github.com/datakaveri/dx-common-go/resilience"
)

// BulkStats summarizes a bulk indexing call: how many documents succeeded,
// how many failed, and why.
type BulkStats struct {
	Indexed int
	Failed  int
	Errors  []ItemError
}

// ItemError is one failed item from a bulk response.
type ItemError struct {
	ID     string
	Reason string
}

// BulkOp is one operation in a mixed bulk request. Build with IndexOp,
// UpdateOp, or DeleteOp.
type BulkOp struct {
	action string // "index" | "update" | "delete"
	id     string
	doc    any // document for index, partial document for update, nil for delete
}

// IndexOp stores doc under id (full overwrite).
func IndexOp(id string, doc any) BulkOp { return BulkOp{action: "index", id: id, doc: doc} }

// UpdateOp applies a partial-document merge to id.
func UpdateOp(id string, partial any) BulkOp { return BulkOp{action: "update", id: id, doc: partial} }

// DeleteOp removes id.
func DeleteOp(id string) BulkOp { return BulkOp{action: "delete", id: id} }

// BulkDo executes a mixed batch of index/update/delete operations via the
// bulk API. Per-item failures (e.g. one document failing mapping validation)
// are reported in the returned BulkStats rather than treated as a call error
// — a bulk request routinely partially succeeds, and one bad document
// shouldn't lose the other 999. The whole batch is retried, with backoff,
// only on a transport-level failure (the request itself didn't get a
// response), up to maxAttempts (default 3 if <= 0).
func BulkDo(ctx context.Context, c *client.Client, index string, ops []BulkOp, maxAttempts int) (BulkStats, error) {
	if len(ops) == 0 {
		return BulkStats{}, nil
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	var body strings.Builder
	for _, op := range ops {
		meta, _ := json.Marshal(map[string]any{op.action: map[string]any{"_index": index, "_id": op.id}})
		body.Write(meta)
		body.WriteByte('\n')
		switch op.action {
		case "index":
			docBody, err := json.Marshal(op.doc)
			if err != nil {
				return BulkStats{}, fmt.Errorf("elastic: marshal bulk doc %s: %w", op.id, err)
			}
			body.Write(docBody)
			body.WriteByte('\n')
		case "update":
			docBody, err := json.Marshal(map[string]any{"doc": op.doc})
			if err != nil {
				return BulkStats{}, fmt.Errorf("elastic: marshal bulk update %s: %w", op.id, err)
			}
			body.Write(docBody)
			body.WriteByte('\n')
		case "delete":
			// delete has no source line
		default:
			return BulkStats{}, fmt.Errorf("elastic: unknown bulk action %q for %s", op.action, op.id)
		}
	}
	payload := body.String()

	// Retry the whole batch on transport-level failure (no response), with
	// backoff, via the shared resilience engine. Per-item failures are not
	// errors here — they come back in BulkStats — so a successful DoNDJSON ends
	// the retry and parseBulkResponse runs once.
	var raw json.RawMessage
	err := resilience.Retry(ctx,
		resilience.NewPolicy(
			resilience.WithMaxAttempts(maxAttempts),
			resilience.WithBaseDelay(100*time.Millisecond),
		),
		func(ctx context.Context) error {
			r, e := c.DoNDJSON(ctx, "/_bulk", payload)
			if e != nil {
				return e
			}
			raw = r
			return nil
		})
	if err != nil {
		return BulkStats{}, fmt.Errorf("elastic: bulk failed after %d attempts: %w", maxAttempts, err)
	}
	return parseBulkResponse(raw)
}

// BulkIndexWithRetry indexes docs (id → document) via the bulk API — the
// index-only convenience form of BulkDo, with the same partial-success and
// retry semantics.
func BulkIndexWithRetry(ctx context.Context, c *client.Client, index string, docs map[string]any, maxAttempts int) (BulkStats, error) {
	ops := make([]BulkOp, 0, len(docs))
	for id, doc := range docs {
		ops = append(ops, IndexOp(id, doc))
	}
	return BulkDo(ctx, c, index, ops, maxAttempts)
}

// BulkIndex indexes docs (id → document) in one bulk request and returns an
// error when any item fails — the strict, no-retry form for small batches
// where any failure should abort. Prefer BulkIndexWithRetry for real loads.
func BulkIndex(ctx context.Context, c *client.Client, index string, docs map[string]any) error {
	if len(docs) == 0 {
		return nil
	}
	var sb strings.Builder
	for id, doc := range docs {
		meta, _ := json.Marshal(map[string]any{"index": map[string]any{"_index": index, "_id": id}})
		docBody, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("elastic: marshal bulk doc %s: %w", id, err)
		}
		sb.Write(meta)
		sb.WriteByte('\n')
		sb.Write(docBody)
		sb.WriteByte('\n')
	}
	payload, err := c.DoNDJSON(ctx, "/_bulk", sb.String())
	if err != nil {
		return err
	}
	var resp struct {
		Errors bool `json:"errors"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return fmt.Errorf("elastic: decode bulk response: %w", err)
	}
	if resp.Errors {
		return fmt.Errorf("elastic: bulk request reported item failures")
	}
	return nil
}

func parseBulkResponse(raw json.RawMessage) (BulkStats, error) {
	var resp struct {
		Items []map[string]struct {
			ID    string `json:"_id"`
			Error *struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return BulkStats{}, fmt.Errorf("elastic: decode bulk response: %w", err)
	}

	var stats BulkStats
	for _, item := range resp.Items {
		for _, result := range item { // exactly one key per item ("index", "create", …)
			if result.Error != nil {
				stats.Failed++
				stats.Errors = append(stats.Errors, ItemError{ID: result.ID, Reason: result.Error.Type + ": " + result.Error.Reason})
			} else {
				stats.Indexed++
			}
		}
	}
	return stats, nil
}
