package dpop

import (
	"errors"
	"fmt"
	"time"

	"github.com/datakaveri/dx-common-go/cache"
)

// maxLeewaySeconds bounds clock-skew tolerance so misconfiguration cannot
// silently accept stale proofs indefinitely.
const maxLeewaySeconds = 300

// defaultMaxAge is how long a proof's "iat" is considered fresh, absent
// Config.MaxAge. RFC 9449 recommends a short window; 60s matches the HMAC
// subject-header replay window already used elsewhere in this platform
// (transport/headers.DefaultMaxAge).
const defaultMaxAge = 60 * time.Second

// Config carries settings needed to validate DPoP proofs.
type Config struct {
	// Enabled controls whether DPoP proof validation is active.
	Enabled bool `mapstructure:"enabled"`
	// MaxAge bounds how old a proof's "iat" may be before it's considered
	// stale. Defaults to 60s.
	MaxAge time.Duration `mapstructure:"max_age"`
	// LeewaySeconds is added to the freshness check in both directions to
	// account for clock skew between client and server.
	LeewaySeconds int `mapstructure:"leeway_seconds"`
	// AllowedAlgorithms restricts accepted proof signature algorithms.
	// Defaults to {ES256, EdDSA} — asymmetric, proof-of-possession-friendly
	// algorithms. HS256 and "none" are never accepted regardless of this list.
	AllowedAlgorithms []string `mapstructure:"allowed_algorithms"`
	// Cache stores seen "jti" values to reject replayed proofs. Required when
	// Enabled. Wired programmatically (e.g. dx-common-go/cache.NewRedisCache
	// in prod, cache.NewMemoryCache in dev) — not populated from config files.
	Cache cache.Cache `mapstructure:"-"`
}

// Validate checks that the configuration is safe to use.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.LeewaySeconds < 0 || c.LeewaySeconds > maxLeewaySeconds {
		return fmt.Errorf("dpop config: leeway_seconds must be between 0 and %d", maxLeewaySeconds)
	}
	if c.Cache == nil {
		return errors.New("dpop config: cache is required when dpop is enabled")
	}
	return nil
}

func (c Config) maxAge() time.Duration {
	if c.MaxAge <= 0 {
		return defaultMaxAge
	}
	return c.MaxAge
}

func (c Config) leeway() time.Duration {
	return time.Duration(c.LeewaySeconds) * time.Second
}

func (c Config) allowedAlgorithms() []string {
	if len(c.AllowedAlgorithms) == 0 {
		return []string{"ES256", "EdDSA"}
	}
	return c.AllowedAlgorithms
}
