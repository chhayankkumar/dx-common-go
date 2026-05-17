package resolver

import (
	dxjwt "github.com/datakaveri/dx-common-go/auth/jwt"
	dxheaders "github.com/datakaveri/dx-common-go/transport/headers"
)

// Config tells the middleware how to verify each path.
type Config struct {
	// Headers is the HMAC verifier config (shared secret + max age + rotation keys).
	// If Headers.Secret is empty the HMAC path is disabled entirely — all
	// requests must use the JWT path.
	Headers dxheaders.Config

	// JWT is the Keycloak validator config (JWKS URL, issuer, audience, leeway).
	// If JWT.Enabled is false the JWT fallback is disabled — all requests must
	// use the HMAC path.
	JWT dxjwt.Config

	// AllowDirect controls whether the direct (JWT) path is permitted at all.
	// When false, only gateway-signed (HMAC) requests are accepted globally.
	//
	// Defaults to false. Set true to enable JWT fallback. When AllowDirect
	// is true, dxjwt.Middleware is built from cfg.JWT — its Enabled flag
	// determines real validation vs dev-mode synthetic-user injection.
	//
	// For per-route enforcement (allow JWT for most routes, deny for a few)
	// keep AllowDirect=true and apply RequireGatewayOrigin on the routes
	// that must be gateway-only.
	AllowDirect bool
}
