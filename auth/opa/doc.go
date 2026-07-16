// Package opa is an embedded, per-service authorization policy store: each
// service loads its own Rego policy (or the built-in default) and a JSON
// data document via config, and evaluates every request against it
// in-process — no network hop, no shared PDP.
//
// This is additive to dx-common-go/auth/fga (dx-authz-go's OpenFGA-backed
// PDP): fga answers relationship questions ("does this user have delegated
// access to this resource"); opa answers declarative per-request questions
// ("is this method+path+role combination allowed"). A service can use
// either, both, or neither.
//
// The default policy (policy.rego) reads a JSON array of
// {method, path_pattern, roles, description} entries from the configured
// data_path — the literal "API paths to role mapping" case — and denies by
// default. A service with needs beyond that shape supplies its own Rego
// policy via config, evaluated against the same {method, path, roles, org_id}
// input; nothing here requires the default policy's data shape.
package opa
