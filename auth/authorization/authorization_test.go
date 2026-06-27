package authorization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/datakaveri/dx-common-go/auth"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func reqWithUser(u *auth.DxUser) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if u != nil {
		r = r.WithContext(auth.WithUser(r.Context(), *u))
	}
	return r
}

// scopeReq builds a request carrying an authenticated user plus a chi URL param.
func scopeReq(u auth.DxUser, entityIDParam, entityID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(auth.WithUser(r.Context(), u))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(entityIDParam, entityID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ── RoleSet ──────────────────────────────────────────────────────────────────

func TestRoleSet_HasAndHasAny(t *testing.T) {
	s := NewRoleSet(RoleConsumer, RoleProvider)
	if !s.Has(RoleConsumer) || s.Has(RoleCosAdmin) {
		t.Fatal("Has wrong")
	}
	if !s.HasAny([]DxRole{RoleCosAdmin, RoleProvider}) {
		t.Fatal("HasAny should match provider")
	}
	if s.HasAny([]DxRole{RoleCosAdmin, RoleOrgAdmin}) {
		t.Fatal("HasAny should not match")
	}
	if NewRoleSet().HasAny([]DxRole{RoleConsumer}) {
		t.Fatal("empty set matches nothing")
	}
}

// ── ForRoles — deny by default ───────────────────────────────────────────────

func TestForRoles_NoUser_401(t *testing.T) {
	w := httptest.NewRecorder()
	ForRoles(RoleCosAdmin)(okHandler()).ServeHTTP(w, reqWithUser(nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestForRoles_InsufficientRole_403(t *testing.T) {
	w := httptest.NewRecorder()
	u := auth.DxUser{ID: "u", Roles: []string{"consumer"}}
	ForRoles(RoleCosAdmin, RoleOrgAdmin)(okHandler()).ServeHTTP(w, reqWithUser(&u))
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestForRoles_Allowed(t *testing.T) {
	w := httptest.NewRecorder()
	u := auth.DxUser{ID: "u", Roles: []string{"consumer", "cos_admin"}}
	ForRoles(RoleCosAdmin)(okHandler()).ServeHTTP(w, reqWithUser(&u))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestForRoles_EmptyUserRoles_403(t *testing.T) {
	w := httptest.NewRecorder()
	u := auth.DxUser{ID: "u"} // no roles
	ForRoles(RoleConsumer)(okHandler()).ServeHTTP(w, reqWithUser(&u))
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

// ── ForScope — delegation, deny by default ───────────────────────────────────

func TestForScope_NoUser_401(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	ForScope(ScopeDataAccess, "entity_id")(okHandler()).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestForScope_Matrix(t *testing.T) {
	cases := []struct {
		name   string
		scopes []auth.DelegationScopeEntry
		entity string
		want   int
	}{
		{"exact match", []auth.DelegationScopeEntry{{Scope: "data-access", EntityID: "e1"}}, "e1", http.StatusOK},
		{"wildcard entity", []auth.DelegationScopeEntry{{Scope: "data-access", EntityID: "*"}}, "e9", http.StatusOK},
		{"wildcard scope", []auth.DelegationScopeEntry{{Scope: "*", EntityID: "e1"}}, "e1", http.StatusOK},
		{"wrong entity", []auth.DelegationScopeEntry{{Scope: "data-access", EntityID: "other"}}, "e1", http.StatusForbidden},
		{"wrong scope", []auth.DelegationScopeEntry{{Scope: "api", EntityID: "e1"}}, "e1", http.StatusForbidden},
		{"no scopes", nil, "e1", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := auth.DxUser{ID: "u", Scopes: c.scopes}
			w := httptest.NewRecorder()
			ForScope(ScopeDataAccess, "entity_id")(okHandler()).ServeHTTP(w, scopeReq(u, "entity_id", c.entity))
			if w.Code != c.want {
				t.Fatalf("%s: want %d, got %d", c.name, c.want, w.Code)
			}
		})
	}
}

// ── ScopeSet ─────────────────────────────────────────────────────────────────

func TestScopeSet_WildcardGrantsAll(t *testing.T) {
	s := NewScopeSet(ScopeWildcard)
	if !s.Has(ScopeDataAccess) {
		t.Fatal("wildcard scope set should grant any scope")
	}
	if NewScopeSet(ScopeUserManagement).Has(ScopeDataAccess) {
		t.Fatal("non-wildcard set should not grant unrelated scope")
	}
}

// Scope string values must match the Java dx platform (kebab-case) so they line
// up with token claims; a regression here silently breaks scoped authz.
func TestScopes_JavaParityValues(t *testing.T) {
	want := map[DelegationScope]string{
		ScopeDataAccess: "data-access", ScopeOwnAssetManagement: "own-asset-management",
		ScopeOrgAssetManagement: "org-asset-management", ScopeAssetManagement: "asset-management",
		ScopeOrgUserManagement: "org-user-management", ScopeUserManagement: "user-management",
		ScopeOrgAssetPublish: "org-asset-publish", ScopeAssetPublish: "asset-publish",
		ScopeOrgPublisherManagement: "org-publisher-management", ScopePublisherManagement: "publisher-management",
		ScopeOrgManagement: "org-management", ScopeRoleManagement: "role-management",
		ScopeComputeManagement: "compute-management", ScopeCreditManagement: "credit-management",
	}
	for sc, str := range want {
		if string(sc) != str {
			t.Fatalf("scope value mismatch: got %q want %q", string(sc), str)
		}
	}
	if len(AllSystemScopes) != 14 {
		t.Fatalf("expected 14 system scopes, got %d", len(AllSystemScopes))
	}
}
