package opa

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datakaveri/dx-common-go/auth"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddleware_RejectsMissingUser(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{{"method": "GET", "path_pattern": "/x", "roles": []string{"cos_admin"}}})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	Middleware(e)(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_AllowsMatchingRole(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{{"method": "GET", "path_pattern": "/x", "roles": []string{"cos_admin"}}})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(auth.WithUser(req.Context(), auth.DxUser{ID: "u1", Roles: []string{"cos_admin"}}))
	rec := httptest.NewRecorder()
	Middleware(e)(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestMiddleware_DeniesMissingRole(t *testing.T) {
	e := newTestEvaluator(t, []map[string]any{{"method": "GET", "path_pattern": "/x", "roles": []string{"cos_admin"}}})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(auth.WithUser(req.Context(), auth.DxUser{ID: "u1", Roles: []string{"consumer"}}))
	rec := httptest.NewRecorder()
	Middleware(e)(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
