package jwt

import (
	"context"
	"fmt"
	"time"

	keyfunc "github.com/MicahParks/keyfunc/v3"
	gojwt "github.com/golang-jwt/jwt/v5"
)

// KeycloakJWKS wraps the keyfunc JWKS with auto-refresh support.
type KeycloakJWKS struct {
	jwks keyfunc.Keyfunc
	cfg  Config
}

// NewKeycloakJWKS creates a JWKS client that fetches and caches public keys
// from the Keycloak JWKS endpoint, refreshing them at the configured interval.
func NewKeycloakJWKS(cfg Config) (*KeycloakJWKS, error) {
	refreshInterval := cfg.RefreshInterval
	if refreshInterval == 0 {
		refreshInterval = 5 * time.Minute
	}

	// The context passed to keyfunc owns the background refresh goroutine, so
	// it must outlive this constructor. The initial fetch is bounded separately
	// so an unreachable Keycloak cannot hang start-up indefinitely.
	ctx := context.Background()

	type result struct {
		jwks keyfunc.Keyfunc
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		j, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JwksURL})
		resCh <- result{jwks: j, err: err}
	}()

	select {
	case res := <-resCh:
		if res.err != nil {
			return nil, fmt.Errorf("initialising keyfunc JWKS from %q: %w", cfg.JwksURL, res.err)
		}
		return &KeycloakJWKS{jwks: res.jwks, cfg: cfg}, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("initialising keyfunc JWKS from %q: timed out after 30s", cfg.JwksURL)
	}
}

// Keyfunc returns the jwt.Keyfunc suitable for use with golang-jwt/jwt/v5.
func (k *KeycloakJWKS) Keyfunc() gojwt.Keyfunc {
	return k.jwks.Keyfunc
}
