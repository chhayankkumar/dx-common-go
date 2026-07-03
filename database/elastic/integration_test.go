package elastic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// testClient connects to a real Elasticsearch instance for integration
// tests. Set ES_TEST_ADDR (e.g. "http://localhost:19200") to run these;
// otherwise they skip, since a database dependency shouldn't block a plain
// `go test ./...`.
func testClient(t *testing.T) *Client {
	t.Helper()
	addr := os.Getenv("ES_TEST_ADDR")
	if addr == "" {
		t.Skip("ES_TEST_ADDR not set; skipping Elasticsearch integration test")
	}
	c, err := NewClient(Config{Addresses: []string{addr}, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("connect to test elasticsearch at %s: %v", addr, err)
	}
	return c
}

type widgetDoc struct {
	Name string `json:"name"`
	Rank int    `json:"rank"`
}

func freshIndex(t *testing.T, c *Client) string {
	t.Helper()
	index := fmt.Sprintf("test-widgets-%d", time.Now().UnixNano())
	if err := c.CreateIndex(context.Background(), index, nil); err != nil {
		t.Fatalf("create index %s: %v", index, err)
	}
	t.Cleanup(func() {
		_, _ = c.do(context.Background(), "DELETE", "/"+index, nil)
	})
	return index
}

func refresh(t *testing.T, c *Client, index string) {
	t.Helper()
	if _, err := c.do(context.Background(), "POST", "/"+index+"/_refresh", nil); err != nil {
		t.Fatalf("refresh %s: %v", index, err)
	}
}

func TestRepo_IndexGetSearch(t *testing.T) {
	c := testClient(t)
	index := freshIndex(t, c)
	repo := NewRepo[widgetDoc](c, index)
	ctx := context.Background()

	if err := repo.Index(ctx, "w1", widgetDoc{Name: "alpha", Rank: 1}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	refresh(t, c, index)

	got, err := repo.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "alpha" || got.Rank != 1 {
		t.Fatalf("Get returned %+v", got)
	}

	items, total, err := repo.Search(ctx, Match("name", "alpha"), SearchOpts{Size: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "alpha" {
		t.Fatalf("Search returned total=%d items=%+v", total, items)
	}
}

func TestRepo_Get_NotFound(t *testing.T) {
	c := testClient(t)
	index := freshIndex(t, c)
	repo := NewRepo[widgetDoc](c, index)

	_, err := repo.Get(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected an error for a missing document")
	}
	var dxe dxerrors.DxError
	if !errors.As(err, &dxe) || dxe.Code() != dxerrors.ErrNotFound {
		t.Fatalf("expected a dxerrors NotFound, got %v", err)
	}
}

func TestRepo_BulkIndexAndSearchAfter(t *testing.T) {
	c := testClient(t)
	index := freshIndex(t, c)
	repo := NewRepo[widgetDoc](c, index)
	ctx := context.Background()

	docs := map[string]widgetDoc{}
	for i := 0; i < 5; i++ {
		docs[fmt.Sprintf("w%d", i)] = widgetDoc{Name: fmt.Sprintf("item-%d", i), Rank: i}
	}
	stats, err := repo.BulkIndex(ctx, docs)
	if err != nil {
		t.Fatalf("BulkIndex: %v", err)
	}
	if stats.Indexed != 5 || stats.Failed != 0 {
		t.Fatalf("unexpected BulkStats: %+v", stats)
	}
	refresh(t, c, index)

	sort := []map[string]string{{"rank": "asc"}}
	var seen []int
	var after []any
	for {
		items, next, err := repo.SearchAfter(ctx, MatchAll(), sort, after, 2)
		if err != nil {
			t.Fatalf("SearchAfter: %v", err)
		}
		if len(items) == 0 {
			break
		}
		for _, it := range items {
			seen = append(seen, it.Rank)
		}
		if next == nil {
			break
		}
		after = next
	}
	if len(seen) != 5 {
		t.Fatalf("SearchAfter pagination should have visited all 5 docs, saw %v", seen)
	}
}

func TestAdmin_AliasLifecycle(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	indexA := freshIndex(t, c)
	indexB := freshIndex(t, c)
	alias := fmt.Sprintf("test-alias-%d", time.Now().UnixNano())

	if err := c.EnsureAlias(ctx, indexA, alias); err != nil {
		t.Fatalf("EnsureAlias: %v", err)
	}
	repo := NewRepo[widgetDoc](c, alias)
	if err := repo.Index(ctx, "w1", widgetDoc{Name: "via-alias", Rank: 1}); err != nil {
		t.Fatalf("Index via alias: %v", err)
	}
	refresh(t, c, indexA)

	if err := c.SwapAlias(ctx, alias, indexA, indexB); err != nil {
		t.Fatalf("SwapAlias: %v", err)
	}

	// The alias now resolves to indexB, which has no documents yet.
	if _, err := repo.Get(ctx, "w1"); err == nil {
		t.Fatal("expected NotFound after SwapAlias moved the alias to an empty index")
	}
}

func TestAdmin_PutMappingAndReindex(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	src := freshIndex(t, c)
	dst := freshIndex(t, c)

	if err := c.PutMapping(ctx, src, map[string]any{
		"properties": map[string]any{"name": map[string]any{"type": "keyword"}},
	}); err != nil {
		t.Fatalf("PutMapping: %v", err)
	}

	repo := NewRepo[widgetDoc](c, src)
	if err := repo.Index(ctx, "w1", widgetDoc{Name: "reindex-me", Rank: 1}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	refresh(t, c, src)

	if err := c.Reindex(ctx, src, dst, ""); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	refresh(t, c, dst)

	dstRepo := NewRepo[widgetDoc](c, dst)
	got, err := dstRepo.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get from reindexed target: %v", err)
	}
	if got.Name != "reindex-me" {
		t.Fatalf("reindexed doc mismatch: %+v", got)
	}
}
