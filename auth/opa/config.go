package opa

import "errors"

// defaultQuery is the entrypoint evaluated for both the built-in policy and
// any custom PolicyPath — a custom policy must define
// "package dx.authz" / "allow" (or set Query to point at its own).
const defaultQuery = "data.dx.authz.allow"

// Config controls how a service's embedded policy store is loaded.
type Config struct {
	// Enabled controls whether OPA authorization is active.
	Enabled bool `mapstructure:"enabled"`
	// PolicyPath is a .rego file or directory (bundle) to load instead of
	// the built-in default policy (policy.rego). Empty uses the default.
	PolicyPath string `mapstructure:"policy_path"`
	// DataPath is a JSON file merged into the policy's `data` document —
	// for the default policy, this is the path_roles.json array described
	// in doc.go. Required when PolicyPath is empty (the default policy has
	// nothing to evaluate without it); optional for a custom policy that
	// doesn't need external data.
	DataPath string `mapstructure:"data_path"`
	// Query is the Rego query to evaluate. Defaults to the default policy's
	// entrypoint; set this when PolicyPath points at a differently-named
	// package/rule.
	Query string `mapstructure:"query"`
}

// Validate checks that the configuration is safe to use.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.PolicyPath == "" && c.DataPath == "" {
		return errors.New("opa config: data_path is required when policy_path is unset (the default policy reads path_roles from it)")
	}
	return nil
}

func (c Config) query() string {
	if c.Query == "" {
		return defaultQuery
	}
	return c.Query
}
