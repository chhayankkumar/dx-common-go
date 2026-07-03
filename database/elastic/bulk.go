package elastic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
func (c *Client) BulkDo(ctx context.Context, index string, ops []BulkOp, maxAttempts int) (BulkStats, error) {
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

	var lastErr error
	delay := 100 * time.Millisecond
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		raw, err := c.doNDJSON(ctx, "/_bulk", payload)
		if err == nil {
			return parseBulkResponse(raw)
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return BulkStats{}, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
	return BulkStats{}, fmt.Errorf("elastic: bulk failed after %d attempts: %w", maxAttempts, lastErr)
}

// BulkIndexWithRetry indexes docs (id → document) via the bulk API — the
// index-only convenience form of BulkDo, with the same partial-success and
// retry semantics.
func (c *Client) BulkIndexWithRetry(ctx context.Context, index string, docs map[string]any, maxAttempts int) (BulkStats, error) {
	ops := make([]BulkOp, 0, len(docs))
	for id, doc := range docs {
		ops = append(ops, IndexOp(id, doc))
	}
	return c.BulkDo(ctx, index, ops, maxAttempts)
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
