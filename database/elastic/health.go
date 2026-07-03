package elastic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ClusterHealth returns the cluster status: "green", "yellow", or "red".
func (c *Client) ClusterHealth(ctx context.Context) (string, error) {
	payload, err := c.do(ctx, http.MethodGet, "/_cluster/health", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return "", fmt.Errorf("elastic: decode cluster health: %w", err)
	}
	return resp.Status, nil
}

// HealthCheck reports whether the cluster is usable — nil for green or
// yellow, an error when unreachable or red. Yellow is healthy on purpose: a
// single-node dev cluster can never place replicas, so it is permanently
// yellow. Plugs straight into dx-common-go/health:
//
//	hh.Register("elasticsearch", health.NewCustomChecker("elasticsearch", esClient.HealthCheck))
func (c *Client) HealthCheck(ctx context.Context) error {
	status, err := c.ClusterHealth(ctx)
	if err != nil {
		return err
	}
	if status == "red" {
		return fmt.Errorf("elasticsearch cluster status is red")
	}
	return nil
}
