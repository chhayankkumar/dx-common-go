package elastic

import (
	"context"
	"net/http"
	"net/url"
)

// EnsureAlias points alias at index, creating the alias if it doesn't exist
// yet. Idempotent — safe to call on every boot. Part of the alias/
// versioned-index model in DATABASE.md §8.4: services address data through
// a stable alias, never a physical index name, so a reindex can repoint it
// without a client-visible change.
func (c *Client) EnsureAlias(ctx context.Context, index, alias string) error {
	_, err := c.do(ctx, http.MethodPut, "/"+url.PathEscape(index)+"/_alias/"+url.PathEscape(alias), nil)
	return err
}

// SwapAlias atomically repoints alias from oldIndex to newIndex in one
// _aliases call — readers never observe a moment where the alias resolves
// to neither index, the way a separate remove-then-add would.
func (c *Client) SwapAlias(ctx context.Context, alias, oldIndex, newIndex string) error {
	body := map[string]any{
		"actions": []map[string]any{
			{"remove": map[string]any{"index": oldIndex, "alias": alias}},
			{"add": map[string]any{"index": newIndex, "alias": alias}},
		},
	}
	_, err := c.do(ctx, http.MethodPost, "/_aliases", body)
	return err
}

// PutMapping updates an index's mapping. Adding new fields is a safe online
// operation; changing an existing field's type is not (ES rejects it) — use
// the reindex-to-a-new-index-then-SwapAlias flow for that, matching the
// expand-only migration convention in DATABASE.md §8.4.
func (c *Client) PutMapping(ctx context.Context, index string, mapping map[string]any) error {
	_, err := c.do(ctx, http.MethodPut, "/"+url.PathEscape(index)+"/_mapping", mapping)
	return err
}

// Reindex copies documents from src to dst, optionally transforming them
// with a Painless script, and blocks until Elasticsearch reports completion
// — the versioned-index rebuild flow: create dst with the new mapping,
// Reindex into it, SwapAlias, then drop src once readers have moved on.
func (c *Client) Reindex(ctx context.Context, src, dst, script string) error {
	body := map[string]any{
		"source": map[string]any{"index": src},
		"dest":   map[string]any{"index": dst},
	}
	if script != "" {
		body["script"] = map[string]any{"source": script, "lang": "painless"}
	}
	_, err := c.do(ctx, http.MethodPost, "/_reindex?wait_for_completion=true", body)
	return err
}
