// Package elastic is the Go counterpart of the Java dx-common Elasticsearch
// module: a thin client plus composable query-DSL builders, so services
// describe searches structurally instead of hand-writing JSON.
package elastic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	es "github.com/elastic/go-elasticsearch/v8"

	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// Config holds Elasticsearch connection settings.
type Config struct {
	// Addresses is the list of node URLs, e.g. ["http://elasticsearch:9200"].
	Addresses []string `mapstructure:"addresses"`
	Username  string   `mapstructure:"username"`
	Password  string   `mapstructure:"password"`
	APIKey    string   `mapstructure:"api_key"`
	// Timeout bounds each request. Default 10s.
	Timeout time.Duration `mapstructure:"timeout"`
}

// Client wraps the official low-level client with JSON helpers and
// dxerrors translation. Safe for concurrent use.
type Client struct {
	es      *es.Client
	timeout time.Duration
}

// NewClient validates cfg, connects, and pings the cluster so configuration
// errors surface at startup.
func NewClient(cfg Config) (*Client, error) {
	if len(cfg.Addresses) == 0 {
		return nil, errors.New("elastic config: addresses is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	esClient, err := es.NewClient(es.Config{
		Addresses: cfg.Addresses,
		Username:  cfg.Username,
		Password:  cfg.Password,
		APIKey:    cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("elastic.NewClient: %w", err)
	}

	c := &Client{es: esClient, timeout: cfg.Timeout}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	if _, err := c.do(ctx, http.MethodGet, "/", nil); err != nil {
		return nil, fmt.Errorf("elastic.NewClient: ping: %w", err)
	}
	return c, nil
}

// do performs one JSON request against the cluster and returns the decoded
// response body. Non-2xx statuses are translated to dxerrors.
func (c *Client) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("elastic: marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return nil, fmt.Errorf("elastic: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.es.Perform(req)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch unreachable: %w", err)
	}
	defer res.Body.Close()

	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("elastic: read response: %w", err)
	}

	if res.StatusCode >= 400 {
		detail := extractESError(payload)
		switch res.StatusCode {
		case http.StatusNotFound:
			return nil, dxerrors.NewNotFound(detail)
		case http.StatusBadRequest:
			return nil, dxerrors.NewValidation(detail)
		case http.StatusConflict:
			return nil, dxerrors.NewConflict(detail)
		default:
			return nil, dxerrors.NewInternal(fmt.Sprintf("elasticsearch %d: %s", res.StatusCode, detail))
		}
	}
	return payload, nil
}

func extractESError(payload []byte) string {
	var body struct {
		Error struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &body) == nil && body.Error.Reason != "" {
		return body.Error.Type + ": " + body.Error.Reason
	}
	s := strings.TrimSpace(string(payload))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// readAll drains a response body.
func readAll(res *http.Response) ([]byte, error) {
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("elastic: read response: %w", err)
	}
	return payload, nil
}

// dxIsNotFound reports whether err is a dxerrors NotFound.
func dxIsNotFound(err error) bool {
	var dxe dxerrors.DxError
	return errors.As(err, &dxe) && dxe.Code() == dxerrors.ErrNotFound
}
