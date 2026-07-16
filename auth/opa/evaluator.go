package opa

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/open-policy-agent/opa/v1/rego"
)

//go:embed policy.rego
var defaultPolicy string

// Input is what a request is evaluated against. The default policy only
// looks at Method/Path/Roles; OrgID is available to custom policies that
// need org-scoped rules.
type Input struct {
	Method string   `json:"method"`
	Path   string   `json:"path"`
	Roles  []string `json:"roles"`
	OrgID  string   `json:"org_id,omitempty"`
}

// Evaluator holds a prepared Rego query for a service's policy store.
// Safe for concurrent use, including a Reload racing with Allow calls.
type Evaluator struct {
	cfg Config

	mu    sync.RWMutex
	query rego.PreparedEvalQuery
}

// New builds an Evaluator from cfg, loading and compiling the policy (and
// its data document, if any) immediately so a malformed policy fails at
// start-up rather than on first request — a fail-fast, default-deny package
// should never fail open.
func New(cfg Config) (*Evaluator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("opa.New: %w", err)
	}
	e := &Evaluator{cfg: cfg}
	if err := e.load(context.Background()); err != nil {
		return nil, fmt.Errorf("opa.New: %w", err)
	}
	return e, nil
}

// Reload re-reads PolicyPath/DataPath from disk and swaps in the newly
// compiled query, letting an operator update the policy store without a
// redeploy (wire it to SIGHUP or a poll loop — there's no built-in file
// watcher here). On error, the previously loaded policy keeps serving.
func (e *Evaluator) Reload(ctx context.Context) error {
	return e.load(ctx)
}

func (e *Evaluator) load(ctx context.Context) error {
	opts := []func(*rego.Rego){rego.Query(e.cfg.query())}

	if e.cfg.PolicyPath == "" {
		opts = append(opts, rego.Module("policy.rego", defaultPolicy))
	} else {
		opts = append(opts, rego.Load([]string{e.cfg.PolicyPath}, nil))
	}

	if e.cfg.DataPath != "" {
		data, err := loadJSONData(e.cfg.DataPath)
		if err != nil {
			return fmt.Errorf("load data_path %q: %w", e.cfg.DataPath, err)
		}
		opts = append(opts, rego.Data(data))
	}

	prepared, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("compile policy: %w", err)
	}
	e.mu.Lock()
	e.query = prepared
	e.mu.Unlock()
	return nil
}

// Allow evaluates in against the loaded policy. A query that produces no
// result (undefined) is treated as deny, matching Rego's own convention for
// an unmatched default-false rule.
func (e *Evaluator) Allow(ctx context.Context, in Input) (bool, error) {
	e.mu.RLock()
	q := e.query
	e.mu.RUnlock()

	rs, err := q.Eval(ctx, rego.EvalInput(in))
	if err != nil {
		return false, fmt.Errorf("opa: evaluate: %w", err)
	}
	return rs.Allowed(), nil
}

func loadJSONData(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	// The default policy expects data.path_roles to be the array itself;
	// wrap a bare array under that key so `rego.Data` (which requires a
	// map[string]any) can carry it. A custom policy pointing DataPath at an
	// object is passed through as-is.
	if arr, ok := raw.([]any); ok {
		return map[string]any{"path_roles": arr}, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("data_path must contain a JSON array or object, got %T", raw)
	}
	return obj, nil
}
