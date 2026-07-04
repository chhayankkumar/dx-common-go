// Package elastic is the Go counterpart of the Java dx-common Elasticsearch
// module: a thin client plus composable query-DSL builders, so services
// describe searches structurally instead of hand-writing JSON.
package elastic

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	es "github.com/elastic/go-elasticsearch/v8"
	"go.uber.org/zap"

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

	// CACertPath points at a PEM bundle to trust for TLS connections
	// (self-signed / private-CA clusters). Ignored when Transport is set.
	CACertPath string `mapstructure:"ca_cert_path"`
	// InsecureSkipVerify disables TLS certificate verification — dev/test
	// only, never production. Ignored when Transport is set.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`

	// MaxRetries caps transport-level retries on 429/502/503/504 responses
	// (exponential backoff). Default 3. Set DisableRetry to turn retries off
	// entirely — e.g. for non-idempotent scripted updates where a replayed
	// request must not run twice.
	MaxRetries   int  `mapstructure:"max_retries"`
	DisableRetry bool `mapstructure:"disable_retry"`

	// MaxIdleConnsPerHost sizes the HTTP keep-alive pool per node. Go's
	// default (2) throttles concurrent ES traffic badly; services with real
	// search volume should set 32–100. Ignored when Transport is set.
	MaxIdleConnsPerHost int `mapstructure:"max_idle_conns_per_host"`

	// EnableMetrics publishes dx_elastic_requests_total{method,status} and
	// dx_elastic_request_duration_seconds{method} to the default Prometheus
	// registry (served by dx-common-go/metrics.Handler).
	EnableMetrics bool `mapstructure:"enable_metrics"`

	// Logger, when set, logs each request at Debug and failures at Warn.
	// Runtime-only — not loadable from config files.
	Logger *zap.Logger `mapstructure:"-"`
	// Transport overrides the HTTP transport — the seam for tests (canned
	// responses without a server) and for future instrumentation such as
	// OpenTelemetry. When set, the TLS fields above are ignored: an explicit
	// transport owns its own TLS configuration.
	Transport http.RoundTripper `mapstructure:"-"`
}

// buildTransport resolves the effective RoundTripper for cfg: an explicit
// Transport wins; otherwise a TLS-configured clone of the default transport
// is built when CACertPath / InsecureSkipVerify demand one. The result (or
// nil, meaning "library default") is then wrapped with observability when
// metrics or logging are enabled.
func buildTransport(cfg Config) (http.RoundTripper, error) {
	rt := cfg.Transport
	if rt == nil && (cfg.CACertPath != "" || cfg.InsecureSkipVerify || cfg.MaxIdleConnsPerHost > 0) {
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("elastic config: default transport unavailable for TLS/pool configuration")
		}
		t := base.Clone()
		if cfg.MaxIdleConnsPerHost > 0 {
			t.MaxIdleConnsPerHost = cfg.MaxIdleConnsPerHost
			if t.MaxIdleConns < cfg.MaxIdleConnsPerHost {
				t.MaxIdleConns = cfg.MaxIdleConnsPerHost
			}
		}
		if cfg.CACertPath != "" || cfg.InsecureSkipVerify {
			tlsCfg := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} // #nosec G402 — explicit dev-only opt-in
			if cfg.CACertPath != "" {
				pem, err := os.ReadFile(cfg.CACertPath)
				if err != nil {
					return nil, fmt.Errorf("elastic config: read ca_cert_path: %w", err)
				}
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM(pem) {
					return nil, errors.New("elastic config: ca_cert_path contains no valid PEM certificates")
				}
				tlsCfg.RootCAs = pool
			}
			t.TLSClientConfig = tlsCfg
		}
		rt = t
	}
	if cfg.EnableMetrics || cfg.Logger != nil {
		if rt == nil {
			rt = http.DefaultTransport
		}
		rt = newObservedTransport(rt, cfg.Logger, cfg.EnableMetrics)
	}
	return rt, nil
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

	esCfg := es.Config{
		Addresses: cfg.Addresses,
		Username:  cfg.Username,
		Password:  cfg.Password,
		APIKey:    cfg.APIKey,
	}
	if cfg.DisableRetry {
		esCfg.DisableRetry = true
	} else {
		maxRetries := cfg.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 3
		}
		esCfg.MaxRetries = maxRetries
		esCfg.RetryOnStatus = []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout}
		esCfg.RetryBackoff = func(attempt int) time.Duration {
			return time.Duration(attempt*attempt) * 100 * time.Millisecond
		}
	}
	transport, err := buildTransport(cfg)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		esCfg.Transport = transport
	}

	esClient, err := es.NewClient(esCfg)
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
