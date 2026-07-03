package elastic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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

// EnsureIndex creates index with the given settings/mappings body if it does
// not already exist, reporting whether it was created. An existing index is
// left untouched (its live mapping wins — this is provisioning, not
// migration; evolve an existing index with PutMapping or Reindex+SwapAlias).
func (c *Client) EnsureIndex(ctx context.Context, index string, body map[string]any) (bool, error) {
	exists, err := c.IndexExists(ctx, index)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if err := c.CreateIndex(ctx, index, body); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteIndex removes an index permanently — the final step of the
// versioned-index rebuild after SwapAlias has moved readers off it.
func (c *Client) DeleteIndex(ctx context.Context, index string) error {
	_, err := c.do(ctx, http.MethodDelete, "/"+url.PathEscape(index), nil)
	return err
}

// AliasIndices returns the physical indices an alias currently points at
// (empty when the alias doesn't exist) — used by rebuild flows to discover
// the "old" index before a SwapAlias.
func (c *Client) AliasIndices(ctx context.Context, alias string) ([]string, error) {
	payload, err := c.do(ctx, http.MethodGet, "/_alias/"+url.PathEscape(alias), nil)
	if err != nil {
		if dxIsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("elastic: decode alias response: %w", err)
	}
	indices := make([]string, 0, len(resp))
	for index := range resp {
		indices = append(indices, index)
	}
	sort.Strings(indices)
	return indices, nil
}

// PutIndexTemplate installs (or replaces) a composable index template: any
// index later created with a name matching the template's index_patterns
// inherits its settings/mappings. body is the full _index_template payload,
// e.g. {"index_patterns": ["logs-*"], "template": {"mappings": {...}}}.
func (c *Client) PutIndexTemplate(ctx context.Context, name string, body map[string]any) error {
	_, err := c.do(ctx, http.MethodPut, "/_index_template/"+url.PathEscape(name), body)
	return err
}

// DeleteIndexTemplate removes a composable index template.
func (c *Client) DeleteIndexTemplate(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/_index_template/"+url.PathEscape(name), nil)
	return err
}
