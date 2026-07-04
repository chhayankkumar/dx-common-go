package elastic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

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
// stable alias, per DATABASE.md §8.4:
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
func (c *Client) MigrateIndex(ctx context.Context, alias, newIndex string, body map[string]any, opts MigrateOptions) error {
	if alias == newIndex {
		return fmt.Errorf("elastic: alias %q must differ from the physical index name", alias)
	}

	olds, err := c.AliasIndices(ctx, alias)
	if err != nil {
		return fmt.Errorf("elastic: migrate %s: resolve alias: %w", alias, err)
	}

	if _, err := c.EnsureIndex(ctx, newIndex, body); err != nil {
		return fmt.Errorf("elastic: migrate %s: create %s: %w", alias, newIndex, err)
	}

	if len(olds) == 0 {
		if err := c.EnsureAlias(ctx, newIndex, alias); err != nil {
			return fmt.Errorf("elastic: migrate %s: point alias: %w", alias, err)
		}
		return nil
	}

	for _, old := range olds {
		if old == newIndex {
			continue // alias already includes the target — nothing to copy from itself
		}
		if err := c.Reindex(ctx, old, newIndex, opts.Script); err != nil {
			return fmt.Errorf("elastic: migrate %s: reindex %s → %s: %w", alias, old, newIndex, err)
		}
	}
	for _, old := range olds {
		if old == newIndex {
			continue
		}
		if err := c.SwapAlias(ctx, alias, old, newIndex); err != nil {
			return fmt.Errorf("elastic: migrate %s: swap %s → %s: %w", alias, old, newIndex, err)
		}
	}
	if opts.DeleteOld {
		for _, old := range olds {
			if old == newIndex {
				continue
			}
			if err := c.DeleteIndex(ctx, old); err != nil {
				return fmt.Errorf("elastic: migrate %s: delete old %s: %w", alias, old, err)
			}
		}
	}
	return nil
}

// Refresh makes recent writes searchable immediately. Never call it per
// document — that defeats the refresh_interval batching; it exists for tests
// and for read-your-write moments after a bulk load.
func (c *Client) Refresh(ctx context.Context, index string) error {
	_, err := c.do(ctx, http.MethodPost, "/"+url.PathEscape(index)+"/_refresh", nil)
	return err
}

// UpdateIndexSettings changes dynamic index settings, e.g. bulk-load pattern:
// set {"refresh_interval": "-1", "number_of_replicas": 0} before a mass
// ingest, restore {"refresh_interval": "30s", "number_of_replicas": 1} after.
func (c *Client) UpdateIndexSettings(ctx context.Context, index string, settings map[string]any) error {
	_, err := c.do(ctx, http.MethodPut, "/"+url.PathEscape(index)+"/_settings",
		map[string]any{"index": settings})
	return err
}

// PutILMPolicy installs an Index Lifecycle Management policy (retention for
// time-series/log indices: hot → warm → delete). body is the policy's
// "phases" tree. Pair with an index template that sets
// "index.lifecycle.name" so new indices adopt it.
func (c *Client) PutILMPolicy(ctx context.Context, name string, phases map[string]any) error {
	_, err := c.do(ctx, http.MethodPut, "/_ilm/policy/"+url.PathEscape(name),
		map[string]any{"policy": map[string]any{"phases": phases}})
	return err
}
