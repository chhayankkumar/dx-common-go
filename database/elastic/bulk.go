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

// BulkIndexWithRetry indexes docs (id → document) via the bulk API.
// Per-item failures (e.g. one document failing mapping validation) are
// reported in the returned BulkStats rather than treated as a call error —
// a bulk request routinely partially succeeds, and one bad document
// shouldn't lose the other 999. The whole batch is retried, with backoff,
// only on a transport-level failure (the request itself didn't get a
// response), up to maxAttempts (default 3 if <= 0).
func (c *Client) BulkIndexWithRetry(ctx context.Context, index string, docs map[string]any, maxAttempts int) (BulkStats, error) {
	if len(docs) == 0 {
		return BulkStats{}, nil
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	var body strings.Builder
	for id, doc := range docs {
		meta, _ := json.Marshal(map[string]any{"index": map[string]any{"_index": index, "_id": id}})
		docBody, err := json.Marshal(doc)
		if err != nil {
			return BulkStats{}, fmt.Errorf("elastic: marshal bulk doc %s: %w", id, err)
		}
		body.Write(meta)
		body.WriteByte('\n')
		body.Write(docBody)
		body.WriteByte('\n')
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
	return BulkStats{}, fmt.Errorf("elastic: bulk index failed after %d attempts: %w", maxAttempts, lastErr)
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
