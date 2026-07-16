package opa

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writePathRoles(t *testing.T, entries []map[string]any) string {
	t.Helper()
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "path_roles.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func newTestEvaluator(t *testing.T, entries []map[string]any) *Evaluator {
	t.Helper()
	e, err := New(Config{Enabled: true, DataPath: writePathRoles(t, entries)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestEvaluator_AllowsExactMatch(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{
		{"method": "POST", "path_pattern": "/iudx/v2/resource_servers", "roles": []string{"cos_admin"}},
	})
	allowed, err := e.Allow(context.Background(), Input{Method: "POST", Path: "/iudx/v2/resource_servers", Roles: []string{"cos_admin"}})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !allowed {
		t.Fatal("expected exact method+path+role match to be allowed")
	}
}

func TestEvaluator_DeniesByDefault(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{
		{"method": "POST", "path_pattern": "/iudx/v2/resource_servers", "roles": []string{"cos_admin"}},
	})
	allowed, err := e.Allow(context.Background(), Input{Method: "GET", Path: "/iudx/v2/resource_servers", Roles: []string{"cos_admin"}})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if allowed {
		t.Fatal("expected unmatched method to be denied")
	}
}

func TestEvaluator_DeniesWrongRole(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{
		{"method": "DELETE", "path_pattern": "/iudx/v2/resource_servers/1", "roles": []string{"cos_admin"}},
	})
	allowed, err := e.Allow(context.Background(), Input{Method: "DELETE", Path: "/iudx/v2/resource_servers/1", Roles: []string{"consumer"}})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if allowed {
		t.Fatal("expected mismatched role to be denied")
	}
}

func TestEvaluator_PrefixWildcard(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{
		{"method": "GET", "path_pattern": "/iudx/v2/resource_servers*", "roles": []string{"consumer"}},
	})
	allowed, err := e.Allow(context.Background(), Input{Method: "GET", Path: "/iudx/v2/resource_servers/abc/def", Roles: []string{"consumer"}})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !allowed {
		t.Fatal("expected wildcard prefix match to be allowed")
	}
}

func TestEvaluator_AnyMatchingRoleAllows(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{
		{"method": "GET", "path_pattern": "/x", "roles": []string{"cos_admin", "org_admin"}},
	})
	allowed, err := e.Allow(context.Background(), Input{Method: "GET", Path: "/x", Roles: []string{"org_admin"}})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !allowed {
		t.Fatal("expected one-of-many role match to be allowed")
	}
}

func TestEvaluator_EmptyRoleSetDenied(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{
		{"method": "GET", "path_pattern": "/x", "roles": []string{"cos_admin"}},
	})
	allowed, err := e.Allow(context.Background(), Input{Method: "GET", Path: "/x", Roles: nil})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if allowed {
		t.Fatal("expected no-roles request to be denied")
	}
}

func TestNew_FailsClosedOnMissingDataPath(t *testing.T) {
	if _, err := New(Config{Enabled: true}); err == nil {
		t.Fatal("expected default policy without data_path to be rejected at construction")
	}
}

func TestNew_FailsOnMalformedDataFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := New(Config{Enabled: true, DataPath: path}); err == nil {
		t.Fatal("expected malformed data file to be rejected at construction")
	}
}

func TestNew_FailsOnMalformedCustomPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.rego")
	if err := os.WriteFile(path, []byte("this is not valid rego {{{"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := New(Config{Enabled: true, PolicyPath: path}); err == nil {
		t.Fatal("expected malformed rego policy to be rejected at construction")
	}
}

func TestReload_PicksUpChangedData(t *testing.T) {
	dataPath := writePathRoles(t, []map[string]any{
		{"method": "GET", "path_pattern": "/x", "roles": []string{"cos_admin"}},
	})
	e, err := New(Config{Enabled: true, DataPath: dataPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	allowed, err := e.Allow(context.Background(), Input{Method: "GET", Path: "/x", Roles: []string{"consumer"}})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if allowed {
		t.Fatal("expected consumer to be denied before reload")
	}

	b, err := json.Marshal([]map[string]any{
		{"method": "GET", "path_pattern": "/x", "roles": []string{"consumer"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(dataPath, b, 0o600); err != nil {
		t.Fatalf("rewrite data file: %v", err)
	}
	if err := e.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	allowed, err = e.Allow(context.Background(), Input{Method: "GET", Path: "/x", Roles: []string{"consumer"}})
	if err != nil {
		t.Fatalf("Allow after reload: %v", err)
	}
	if !allowed {
		t.Fatal("expected consumer to be allowed after reload picked up the updated policy store")
	}
}
