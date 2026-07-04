package elastic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ScrollResult is one page of a scroll plus the cursor for the next page.
type ScrollResult struct {
	Hits     []Hit
	Total    int64
	ScrollID string
}

// Scroll opens a scroll over index and returns the first page. Scroll is the
// right tool for full exports / reindex-style jobs that must visit every
// document; for user-facing deep pagination prefer OpenPIT + SearchAfter,
// which doesn't hold heavyweight per-scroll state on the cluster. keepAlive
// (e.g. "1m") only needs to cover the processing time of a single page.
// Always ClearScroll when done — expiry works, but frees resources late.
func (c *Client) Scroll(ctx context.Context, index string, req SearchRequest, keepAlive string) (*ScrollResult, error) {
	if keepAlive == "" {
		keepAlive = "1m"
	}
	path := "/" + url.PathEscape(index) + "/_search?scroll=" + url.QueryEscape(keepAlive)
	payload, err := c.do(ctx, http.MethodPost, path, req.body())
	if err != nil {
		return nil, err
	}
	return parseScrollResult(payload)
}

// ScrollNext fetches the next page for scrollID. An empty Hits slice means
// the scroll is exhausted.
func (c *Client) ScrollNext(ctx context.Context, scrollID, keepAlive string) (*ScrollResult, error) {
	if keepAlive == "" {
		keepAlive = "1m"
	}
	payload, err := c.do(ctx, http.MethodPost, "/_search/scroll", map[string]any{
		"scroll":    keepAlive,
		"scroll_id": scrollID,
	})
	if err != nil {
		return nil, err
	}
	return parseScrollResult(payload)
}

// ClearScroll releases a scroll's cluster-side resources.
func (c *Client) ClearScroll(ctx context.Context, scrollID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/_search/scroll", map[string]any{"scroll_id": scrollID})
	return err
}

func parseScrollResult(payload json.RawMessage) (*ScrollResult, error) {
	var resp struct {
		ScrollID string `json:"_scroll_id"`
		Hits     struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []Hit `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("elastic: decode scroll response: %w", err)
	}
	return &ScrollResult{Hits: resp.Hits.Hits, Total: resp.Hits.Total.Value, ScrollID: resp.ScrollID}, nil
}

// OpenPIT opens a point-in-time view over index and returns its id. A PIT
// freezes the searcher so paginated reads (Sort + SearchAfter + PIT) see one
// consistent snapshot even while writes continue — the modern replacement
// for scroll in user-facing pagination. Pair with ClosePIT.
func (c *Client) OpenPIT(ctx context.Context, index, keepAlive string) (string, error) {
	if keepAlive == "" {
		keepAlive = "1m"
	}
	path := "/" + url.PathEscape(index) + "/_pit?keep_alive=" + url.QueryEscape(keepAlive)
	payload, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return "", fmt.Errorf("elastic: decode PIT response: %w", err)
	}
	return resp.ID, nil
}

// ClosePIT releases a point-in-time.
func (c *Client) ClosePIT(ctx context.Context, pitID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/_pit", map[string]any{"id": pitID})
	return err
}
