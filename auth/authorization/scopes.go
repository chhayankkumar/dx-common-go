package authorization

// DelegationScope is a system scope — the unit of authorization. Endpoints
// require scopes; a principal's effective scope set is checked against them.
//
// The values are kebab-case and match the Java dx platform's
// org.cdpg.dx.auth.model.Scopes EXACTLY, so they line up with the scope claims
// minted in tokens (a mismatch here silently fails every scoped authorization).
type DelegationScope string

const (
	ScopeDataAccess             DelegationScope = "data-access"
	ScopeOwnAssetManagement     DelegationScope = "own-asset-management"
	ScopeOrgAssetManagement     DelegationScope = "org-asset-management"
	ScopeAssetManagement        DelegationScope = "asset-management"
	ScopeOrgUserManagement      DelegationScope = "org-user-management"
	ScopeUserManagement         DelegationScope = "user-management"
	ScopeOrgAssetPublish        DelegationScope = "org-asset-publish"
	ScopeAssetPublish           DelegationScope = "asset-publish"
	ScopeOrgPublisherManagement DelegationScope = "org-publisher-management"
	ScopePublisherManagement    DelegationScope = "publisher-management"
	ScopeOrgManagement          DelegationScope = "org-management"
	ScopeRoleManagement         DelegationScope = "role-management"
	ScopeComputeManagement      DelegationScope = "compute-management"
	ScopeCreditManagement       DelegationScope = "credit-management"

	// ScopeWildcard grants every scope (a delegation convenience; not one of the
	// 14 Java system scopes).
	ScopeWildcard DelegationScope = "*"
)

// AllSystemScopes is the set of the 14 Java-parity system scopes (excludes the
// wildcard). Use to validate that a requested scope is a known system scope.
var AllSystemScopes = NewScopeSet(
	ScopeDataAccess, ScopeOwnAssetManagement, ScopeOrgAssetManagement, ScopeAssetManagement,
	ScopeOrgUserManagement, ScopeUserManagement, ScopeOrgAssetPublish, ScopeAssetPublish,
	ScopeOrgPublisherManagement, ScopePublisherManagement, ScopeOrgManagement, ScopeRoleManagement,
	ScopeComputeManagement, ScopeCreditManagement,
)

// ScopeSet is a set of DelegationScope values backed by a map for O(1) lookups.
type ScopeSet map[DelegationScope]struct{}

// NewScopeSet creates a ScopeSet from the provided scopes.
func NewScopeSet(scopes ...DelegationScope) ScopeSet {
	s := make(ScopeSet, len(scopes))
	for _, sc := range scopes {
		s[sc] = struct{}{}
	}
	return s
}

// Has returns true if scope is in the set or the wildcard "*" scope is present.
func (s ScopeSet) Has(scope DelegationScope) bool {
	if _, ok := s[ScopeWildcard]; ok {
		return true
	}
	_, ok := s[scope]
	return ok
}
