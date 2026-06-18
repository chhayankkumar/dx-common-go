package elastic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// SearchRequest describes one search (the Go counterpart of the Java
// QueryModel's request-level fields).
type SearchRequest struct {
	Query Query
	Size  int
	From  int
	// Sort entries like {"created_at": "desc"}; applied in order.
	Sort []map[string]string
	// SourceIncludes / SourceExcludes filter the returned _source.
	SourceIncludes []string
	SourceExcludes []string
	// Aggregations by name.
	Aggregations map[string]Agg
	// TrackTotalHits forces ES to count all matches when true (default ES caps
	// at 10 000). Set to true for exact counts on large result sets.
	TrackTotalHits bool
	// SizeZero signals "return zero hits" (aggs-only query). When true, Size is
	// written as 0 to the request body even if the Size field is zero-valued.
	SizeZero bool
}

// Hit is one search hit.
type Hit struct {
	ID     string          `json:"_id"`
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"`
}

// SearchResult carries hits, the total match count, and raw aggregations.
type SearchResult struct {
	Hits         []Hit
	Total        int64
	Aggregations map[string]json.RawMessage
}

// HitsAs decodes every hit's _source into T.
func HitsAs[T any](r *SearchResult) ([]T, error) {
	out := make([]T, 0, len(r.Hits))
	for _, h := range r.Hits {
		var item T
		if err := json.Unmarshal(h.Source, &item); err != nil {
			return nil, fmt.Errorf("elastic: decode hit %s: %w", h.ID, err)
		}
		out = append(out, item)
	}
	return out, nil
}

func (r SearchRequest) body() map[string]any {
	body := map[string]any{}
	if r.Query != nil {
		body["query"] = r.Query
	}
	if r.Size > 0 {
		body["size"] = r.Size
	} else if r.SizeZero {
		body["size"] = 0
	}
	if r.From > 0 {
		body["from"] = r.From
	}
	if r.TrackTotalHits {
		body["track_total_hits"] = true
	}
	if len(r.Sort) > 0 {
		body["sort"] = r.Sort
	}
	if len(r.SourceIncludes) > 0 || len(r.SourceExcludes) > 0 {
		src := map[string]any{}
		if len(r.SourceIncludes) > 0 {
			src["includes"] = r.SourceIncludes
		}
		if len(r.SourceExcludes) > 0 {
			src["excludes"] = r.SourceExcludes
		}
		body["_source"] = src
	}
	if len(r.Aggregations) > 0 {
		body["aggs"] = r.Aggregations
	}
	return body
}

// Search executes req against index.
func (c *Client) Search(ctx context.Context, index string, req SearchRequest) (*SearchResult, error) {
	payload, err := c.do(ctx, http.MethodPost, "/"+url.PathEscape(index)+"/_search", req.body())
	if err != nil {
		return nil, err
	}

	var resp struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []Hit `json:"hits"`
		} `json:"hits"`
		Aggregations map[string]json.RawMessage `json:"aggregations"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("elastic: decode search response: %w", err)
	}
	return &SearchResult{
		Hits:         resp.Hits.Hits,
		Total:        resp.Hits.Total.Value,
		Aggregations: resp.Aggregations,
	}, nil
}

// Count returns the number of documents matching query (nil = all).
func (c *Client) Count(ctx context.Context, index string, query Query) (int64, error) {
	var body map[string]any
	if query != nil {
		body = map[string]any{"query": query}
	}
	payload, err := c.do(ctx, http.MethodPost, "/"+url.PathEscape(index)+"/_count", body)
	if err != nil {
		return 0, err
	}
	var resp struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return 0, fmt.Errorf("elastic: decode count response: %w", err)
	}
	return resp.Count, nil
}

// IndexDoc stores doc under id (empty id lets Elasticsearch assign one) and
// returns the document id.
func (c *Client) IndexDoc(ctx context.Context, index, id string, doc any) (string, error) {
	method, path := http.MethodPost, "/"+url.PathEscape(index)+"/_doc"
	if id != "" {
		method, path = http.MethodPut, path+"/"+url.PathEscape(id)
	}
	payload, err := c.do(ctx, method, path, doc)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"_id"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return "", fmt.Errorf("elastic: decode index response: %w", err)
	}
	return resp.ID, nil
}

// GetDoc fetches a document's _source into dest. Returns NotFound when absent.
func (c *Client) GetDoc(ctx context.Context, index, id string, dest any) error {
	payload, err := c.do(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	var resp struct {
		Source json.RawMessage `json:"_source"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return fmt.Errorf("elastic: decode get response: %w", err)
	}
	return json.Unmarshal(resp.Source, dest)
}

// UpdateDoc applies a partial-document update.
func (c *Client) UpdateDoc(ctx context.Context, index, id string, partial any) error {
	_, err := c.do(ctx, http.MethodPost,
		"/"+url.PathEscape(index)+"/_update/"+url.PathEscape(id),
		map[string]any{"doc": partial})
	return err
}

// ScriptUpdate runs a Painless script against a single document. Use for atomic
// field increments, conditional updates, etc. that a partial-doc merge cannot do.
func (c *Client) ScriptUpdate(ctx context.Context, index, id, script string, params map[string]any) error {
	body := map[string]any{
		"script": map[string]any{
			"source": script,
			"lang":   "painless",
			"params": params,
		},
	}
	_, err := c.do(ctx, http.MethodPost,
		"/"+url.PathEscape(index)+"/_update/"+url.PathEscape(id), body)
	return err
}

// UpdateByQuery runs a Painless script against all documents matching query.
// Returns the number of updated documents.
func (c *Client) UpdateByQuery(ctx context.Context, index string, query Query, script string, params map[string]any) (int64, error) {
	body := map[string]any{
		"query": query,
		"script": map[string]any{
			"source": script,
			"lang":   "painless",
			"params": params,
		},
	}
	payload, err := c.do(ctx, http.MethodPost,
		"/"+url.PathEscape(index)+"/_update_by_query", body)
	if err != nil {
		return 0, err
	}
	var resp struct {
		Updated int64 `json:"updated"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return 0, fmt.Errorf("elastic: decode update_by_query response: %w", err)
	}
	return resp.Updated, nil
}

// DeleteDoc removes one document.
func (c *Client) DeleteDoc(ctx context.Context, index, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id), nil)
	return err
}

// DeleteByQuery removes all documents matching query.
func (c *Client) DeleteByQuery(ctx context.Context, index string, query Query) (int64, error) {
	payload, err := c.do(ctx, http.MethodPost,
		"/"+url.PathEscape(index)+"/_delete_by_query",
		map[string]any{"query": query})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return 0, fmt.Errorf("elastic: decode delete_by_query response: %w", err)
	}
	return resp.Deleted, nil
}

// BulkIndex indexes docs (id → document) in one bulk request. Returns an
// error when any item fails.
func (c *Client) BulkIndex(ctx context.Context, index string, docs map[string]any) error {
	if len(docs) == 0 {
		return nil
	}
	var sb strings.Builder
	for id, doc := range docs {
		meta, _ := json.Marshal(map[string]any{"index": map[string]any{"_index": index, "_id": id}})
		body, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("elastic: marshal bulk doc %s: %w", id, err)
		}
		sb.Write(meta)
		sb.WriteByte('\n')
		sb.Write(body)
		sb.WriteByte('\n')
	}

	payload, err := c.doNDJSON(ctx, "/_bulk", sb.String())
	if err != nil {
		return err
	}
	var resp struct {
		Errors bool `json:"errors"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return fmt.Errorf("elastic: decode bulk response: %w", err)
	}
	if resp.Errors {
		return fmt.Errorf("elastic: bulk request reported item failures")
	}
	return nil
}

// CreateIndex creates an index with the given settings/mappings body (may be nil).
func (c *Client) CreateIndex(ctx context.Context, index string, body map[string]any) error {
	_, err := c.do(ctx, http.MethodPut, "/"+url.PathEscape(index), body)
	return err
}

// IndexExists reports whether the index exists.
func (c *Client) IndexExists(ctx context.Context, index string) (bool, error) {
	_, err := c.do(ctx, http.MethodGet, "/"+url.PathEscape(index), nil)
	if err != nil {
		if dxIsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// doNDJSON sends a newline-delimited JSON body (bulk API).
func (c *Client) doNDJSON(ctx context.Context, path, body string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("elastic: build bulk request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	res, err := c.es.Perform(req)
	if err != nil {
		return nil, fmt.Errorf("elastic: bulk request: %w", err)
	}
	defer res.Body.Close()
	payload, err := readAll(res)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("elastic: bulk status %d: %s", res.StatusCode, extractESError(payload))
	}
	return payload, nil
}
