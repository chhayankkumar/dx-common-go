package indexing

import (
	"context"
	"net/http"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
)

// Reindex copies documents from src to dst, optionally transforming them with
// a Painless script, and blocks until Elasticsearch reports completion — the
// core of the versioned-index rebuild flow: create dst with the new mapping,
// Reindex into it, swap the alias, then drop src once readers have moved on.
// The higher-level orchestration lives in mapping.MigrateIndex.
func Reindex(ctx context.Context, c *client.Client, src, dst, script string) error {
	body := map[string]any{
		"source": map[string]any{"index": src},
		"dest":   map[string]any{"index": dst},
	}
	if script != "" {
		body["script"] = map[string]any{"source": script, "lang": "painless"}
	}
	_, err := c.Do(ctx, http.MethodPost, "/_reindex?wait_for_completion=true", body)
	return err
}
