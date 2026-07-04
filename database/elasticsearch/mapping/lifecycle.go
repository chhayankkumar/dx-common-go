package mapping

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
	"github.com/datakaveri/dx-common-go/database/elasticsearch/indexing"
)

// CreateIndex creates an index with the given settings/mappings body (may be nil).
func CreateIndex(ctx context.Context, c *client.Client, index string, body map[string]any) error {
	if body == nil {
		// A nil map[string]any, once boxed into Do's `body any` parameter,
		// is a non-nil interface holding a nil map — Do's `body != nil` check
		// does not catch it, so json.Marshal would send the literal 4 bytes
		// "null" instead of an empty request body, which Elasticsearch
		// rejects. Normalize to "{}" (a valid "no special settings/mappings"
		// index-creation payload) before it reaches Do.
		body = map[string]any{}
	}
	_, err := c.Do(ctx, http.MethodPut, "/"+url.PathEscape(index), body)
	return err
}

// IndexExists reports whether the index exists.
func IndexExists(ctx context.Context, c *client.Client, index string) (bool, error) {
	_, err := c.Do(ctx, http.MethodGet, "/"+url.PathEscape(index), nil)
	if err != nil {
		if client.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// EnsureIndex creates index with the given settings/mappings body if it does
// not already exist, reporting whether it was created. An existing index is
// left untouched (its live mapping wins — this is provisioning, not migration;
// evolve an existing index with PutMapping or MigrateIndex).
func EnsureIndex(ctx context.Context, c *client.Client, index string, body map[string]any) (bool, error) {
	exists, err := IndexExists(ctx, c, index)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if err := CreateIndex(ctx, c, index, body); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteIndex removes an index permanently — the final step of the
// versioned-index rebuild after SwapAlias has moved readers off it.
func DeleteIndex(ctx context.Context, c *client.Client, index string) error {
	_, err := c.Do(ctx, http.MethodDelete, "/"+url.PathEscape(index), nil)
	return err
}

// MigrateOptions tunes MigrateIndex.
type MigrateOptions struct {
	// Script optionally transforms documents during the copy (Painless).
	Script string
	// DeleteOld drops the previous physical indices after the alias swap.
	// Leave false to keep them as instant rollback targets (SwapAlias back),
	// and delete later once the new index has proven itself.
	DeleteOld bool
}

// MigrateIndex performs the zero-downtime (blue/green) index rebuild behind a
// stable alias:
//
//  1. create newIndex with body (settings+mappings — a MappingBuilder.Build())
//  2. copy documents from the alias's current indices (Reindex, optional script)
//  3. atomically swap the alias to newIndex
//  4. optionally delete the old indices
//
// First-time provisioning (alias doesn't exist yet) degrades to: create index,
// point alias — no copy.
//
// Writes during step 2 land in the OLD index and are not re-copied — for
// strict completeness pause writes, or run a second catch-up Reindex after
// the swap, or dual-write during the window. Reads are unaffected throughout.
func MigrateIndex(ctx context.Context, c *client.Client, alias, newIndex string, body map[string]any, opts MigrateOptions) error {
	if alias == newIndex {
		return fmt.Errorf("elastic: alias %q must differ from the physical index name", alias)
	}

	olds, err := AliasIndices(ctx, c, alias)
	if err != nil {
		return fmt.Errorf("elastic: migrate %s: resolve alias: %w", alias, err)
	}

	if _, err := EnsureIndex(ctx, c, newIndex, body); err != nil {
		return fmt.Errorf("elastic: migrate %s: create %s: %w", alias, newIndex, err)
	}

	if len(olds) == 0 {
		if err := EnsureAlias(ctx, c, newIndex, alias); err != nil {
			return fmt.Errorf("elastic: migrate %s: point alias: %w", alias, err)
		}
		return nil
	}

	for _, old := range olds {
		if old == newIndex {
			continue // alias already includes the target — nothing to copy from itself
		}
		if err := indexing.Reindex(ctx, c, old, newIndex, opts.Script); err != nil {
			return fmt.Errorf("elastic: migrate %s: reindex %s → %s: %w", alias, old, newIndex, err)
		}
	}
	for _, old := range olds {
		if old == newIndex {
			continue
		}
		if err := SwapAlias(ctx, c, alias, old, newIndex); err != nil {
			return fmt.Errorf("elastic: migrate %s: swap %s → %s: %w", alias, old, newIndex, err)
		}
	}
	if opts.DeleteOld {
		for _, old := range olds {
			if old == newIndex {
				continue
			}
			if err := DeleteIndex(ctx, c, old); err != nil {
				return fmt.Errorf("elastic: migrate %s: delete old %s: %w", alias, old, err)
			}
		}
	}
	return nil
}

// Refresh makes recent writes searchable immediately. Never call it per
// document — that defeats the refresh_interval batching; it exists for tests
// and for read-your-write moments after a bulk load.
func Refresh(ctx context.Context, c *client.Client, index string) error {
	_, err := c.Do(ctx, http.MethodPost, "/"+url.PathEscape(index)+"/_refresh", nil)
	return err
}

// UpdateIndexSettings changes dynamic index settings, e.g. bulk-load pattern:
// set {"refresh_interval": "-1", "number_of_replicas": 0} before a mass
// ingest, restore {"refresh_interval": "30s", "number_of_replicas": 1} after.
func UpdateIndexSettings(ctx context.Context, c *client.Client, index string, settings map[string]any) error {
	_, err := c.Do(ctx, http.MethodPut, "/"+url.PathEscape(index)+"/_settings",
		map[string]any{"index": settings})
	return err
}

// PutILMPolicy installs an Index Lifecycle Management policy (retention for
// time-series/log indices: hot → warm → delete). phases is the policy's
// "phases" tree. Pair with an index template that sets
// "index.lifecycle.name" so new indices adopt it.
func PutILMPolicy(ctx context.Context, c *client.Client, name string, phases map[string]any) error {
	_, err := c.Do(ctx, http.MethodPut, "/_ilm/policy/"+url.PathEscape(name),
		map[string]any{"policy": map[string]any{"phases": phases}})
	return err
}
