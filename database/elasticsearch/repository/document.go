package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/datakaveri/dx-common-go/database/elasticsearch/client"
	"github.com/datakaveri/dx-common-go/database/elasticsearch/query"
)

// IndexDoc stores doc under id (empty id lets Elasticsearch assign one) and
// returns the document id.
func IndexDoc(ctx context.Context, c *client.Client, index, id string, doc any) (string, error) {
	method, path := http.MethodPost, "/"+url.PathEscape(index)+"/_doc"
	if id != "" {
		method, path = http.MethodPut, path+"/"+url.PathEscape(id)
	}
	payload, err := c.Do(ctx, method, path, doc)
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
func GetDoc(ctx context.Context, c *client.Client, index, id string, dest any) error {
	payload, err := c.Do(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id), nil)
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
func UpdateDoc(ctx context.Context, c *client.Client, index, id string, partial any) error {
	_, err := c.Do(ctx, http.MethodPost,
		"/"+url.PathEscape(index)+"/_update/"+url.PathEscape(id),
		map[string]any{"doc": partial})
	return err
}

// ScriptUpdate runs a Painless script against a single document. Use for atomic
// field increments, conditional updates, etc. that a partial-doc merge cannot do.
func ScriptUpdate(ctx context.Context, c *client.Client, index, id, script string, params map[string]any) error {
	body := map[string]any{
		"script": map[string]any{
			"source": script,
			"lang":   "painless",
			"params": params,
		},
	}
	_, err := c.Do(ctx, http.MethodPost,
		"/"+url.PathEscape(index)+"/_update/"+url.PathEscape(id), body)
	return err
}

// UpdateByQuery runs a Painless script against all documents matching q.
// Returns the number of updated documents.
func UpdateByQuery(ctx context.Context, c *client.Client, index string, q query.Query, script string, params map[string]any) (int64, error) {
	body := map[string]any{
		"query": q,
		"script": map[string]any{
			"source": script,
			"lang":   "painless",
			"params": params,
		},
	}
	payload, err := c.Do(ctx, http.MethodPost,
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
func DeleteDoc(ctx context.Context, c *client.Client, index, id string) error {
	_, err := c.Do(ctx, http.MethodDelete, "/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id), nil)
	return err
}

// DeleteByQuery removes all documents matching q.
func DeleteByQuery(ctx context.Context, c *client.Client, index string, q query.Query) (int64, error) {
	payload, err := c.Do(ctx, http.MethodPost,
		"/"+url.PathEscape(index)+"/_delete_by_query",
		map[string]any{"query": q})
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
