package mapping

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
)

// EnsureAlias points alias at index, creating the alias if it doesn't exist
// yet. Idempotent — safe to call on every boot. Part of the alias/
// versioned-index model: services address data through a stable alias, never a
// physical index name, so a reindex can repoint it without a client-visible
// change.
func EnsureAlias(ctx context.Context, c *client.Client, index, alias string) error {
	_, err := c.Do(ctx, http.MethodPut, "/"+url.PathEscape(index)+"/_alias/"+url.PathEscape(alias), nil)
	return err
}

// SwapAlias atomically repoints alias from oldIndex to newIndex in one
// _aliases call — readers never observe a moment where the alias resolves
// to neither index, the way a separate remove-then-add would.
func SwapAlias(ctx context.Context, c *client.Client, alias, oldIndex, newIndex string) error {
	body := map[string]any{
		"actions": []map[string]any{
			{"remove": map[string]any{"index": oldIndex, "alias": alias}},
			{"add": map[string]any{"index": newIndex, "alias": alias}},
		},
	}
	_, err := c.Do(ctx, http.MethodPost, "/_aliases", body)
	return err
}

// PutMapping updates an index's mapping. Adding new fields is a safe online
// operation; changing an existing field's type is not (ES rejects it) — use
// the reindex-to-a-new-index-then-SwapAlias flow for that, matching the
// expand-only migration convention.
func PutMapping(ctx context.Context, c *client.Client, index string, mapping map[string]any) error {
	_, err := c.Do(ctx, http.MethodPut, "/"+url.PathEscape(index)+"/_mapping", mapping)
	return err
}

// AliasIndices returns the physical indices an alias currently points at
// (empty when the alias doesn't exist) — used by rebuild flows to discover
// the "old" index before a SwapAlias.
func AliasIndices(ctx context.Context, c *client.Client, alias string) ([]string, error) {
	payload, err := c.Do(ctx, http.MethodGet, "/_alias/"+url.PathEscape(alias), nil)
	if err != nil {
		if client.IsNotFound(err) {
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
