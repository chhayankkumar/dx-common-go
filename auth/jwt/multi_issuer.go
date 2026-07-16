package jwt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	jose "gopkg.in/go-jose/go-jose.v2"
)

// IssuerConfig configures validation for a single token issuer.
type IssuerConfig struct {
	// JwksURL is the JWKS endpoint for this issuer, e.g. Keycloak's
	// ".../protocol/openid-connect/certs" (not necessarily at the OIDC
	// well-known discovery path, so it's always set explicitly here).
	JwksURL string `mapstructure:"jwks_url"`
	// Audience values accepted for this issuer's tokens.
	Audience []string `mapstructure:"audience"`
	// Algorithm is the expected signature algorithm, e.g. "RS256" or "ES256".
	// Defaults to RS256 (Keycloak's default).
	Algorithm string `mapstructure:"algorithm"`
	// LeewaySeconds bounds clock-skew tolerance for exp/nbf/iat.
	LeewaySeconds int `mapstructure:"leeway_seconds"`
	// JWKSCacheTTL controls how long fetched keys are cached. Defaults to 5m.
	JWKSCacheTTL time.Duration `mapstructure:"jwks_cache_ttl"`
	// ClaimsProfile selects how this issuer's custom claims are parsed —
	// different sources (this platform's Keycloak realm vs. an external
	// partner IdP) may shape their payload differently. Defaults to
	// "keycloak" (DxCustomClaims). See RegisterClaimsProfile.
	ClaimsProfile string `mapstructure:"claims_profile"`
}

func (c IssuerConfig) algorithm() validator.SignatureAlgorithm {
	if c.Algorithm == "" {
		return validator.RS256
	}
	return validator.SignatureAlgorithm(c.Algorithm)
}

func (c IssuerConfig) jwksCacheTTL() time.Duration {
	if c.JWKSCacheTTL <= 0 {
		return 5 * time.Minute
	}
	return c.JWKSCacheTTL
}

func (c IssuerConfig) claimsProfile() string {
	if c.ClaimsProfile == "" {
		return ProfileKeycloak
	}
	return c.ClaimsProfile
}

// MultiIssuerConfig maps the exact "iss" claim value expected from a source
// to how tokens from it should be validated. This mirrors the Java stack's
// MultiIssuerJwtAuthHandler/JwksResolver: the issuer string in the token
// selects which JWKS/audience/claims-shape to validate against, so a single
// service can accept tokens from this platform's Keycloak realm and from an
// external partner IdP side by side.
type MultiIssuerConfig map[string]IssuerConfig

// ProfileKeycloak is the default claims profile: this platform's Keycloak
// token shape, decoded into *DxCustomClaims.
const ProfileKeycloak = "keycloak"

// ClaimsFactory returns a fresh validator.CustomClaims instance to decode a
// token's custom claims into.
type ClaimsFactory func() validator.CustomClaims

var claimsProfiles = map[string]ClaimsFactory{
	ProfileKeycloak: func() validator.CustomClaims { return &DxCustomClaims{} },
}

// RegisterClaimsProfile registers a named custom-claims shape so an
// IssuerConfig can select it via ClaimsProfile. Call during package/service
// initialisation, before constructing a MultiIssuerValidator that references
// the profile. Not safe for concurrent use with validation traffic.
func RegisterClaimsProfile(name string, factory ClaimsFactory) {
	claimsProfiles[name] = factory
}

// DxCustomClaims is the default ("keycloak") custom-claims shape: the same
// fields as DxClaims, minus the registered claims that go-jwt-middleware's
// validator already parses separately into validator.RegisteredClaims.
type DxCustomClaims struct {
	Email             string                 `json:"email"`
	EmailVerified     bool                   `json:"email_verified"`
	Name              string                 `json:"name"`
	PreferredUsername string                 `json:"preferred_username"`
	RealmAccess       RealmAccess            `json:"realm_access"`
	ResourceAccess    map[string]Roles       `json:"resource_access"`
	OrganisationID    string                 `json:"organisation_id"`
	OrganisationName  string                 `json:"organisation_name"`
	DelegatorID       string                 `json:"did,omitempty"`
	Scope             string                 `json:"scope,omitempty"`
	DelegationScopes  []DelegationScopeClaim `json:"delegation_scopes,omitempty"`
	Cnf               *CnfClaim              `json:"cnf,omitempty"`
}

// Validate satisfies validator.CustomClaims. Role/scope authorization is
// handled downstream (auth/authorization, auth/opa); this is not the place
// to enforce it, so there's nothing to check here beyond successful decode.
func (c *DxCustomClaims) Validate(context.Context) error {
	return nil
}

// AllRoles returns a deduplicated flat list combining realm roles and roles
// from every client in ResourceAccess.
func (c *DxCustomClaims) AllRoles() []string {
	seen := make(map[string]struct{})
	var roles []string
	for _, r := range c.RealmAccess.Roles {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			roles = append(roles, r)
		}
	}
	for _, ra := range c.ResourceAccess {
		for _, r := range ra.Roles {
			if _, ok := seen[r]; !ok {
				seen[r] = struct{}{}
				roles = append(roles, r)
			}
		}
	}
	return roles
}

// issuerValidator pairs a built validator.Validator with its JWKS provider
// so the provider (and its background refresh) stays alive for the
// validator's lifetime.
type issuerValidator struct {
	v    *validator.Validator
	jwks *jwks.CachingProvider
}

// MultiIssuerValidator validates tokens from any of several configured
// issuers, dispatching on the token's unverified "iss" claim — the same
// pattern the Java stack's MultiIssuerJwtAuthHandler uses. Built on
// github.com/auth0/go-jwt-middleware/v2, independent of the single-issuer
// Validator/Middleware in this package (which remains unchanged for existing
// callers).
type MultiIssuerValidator struct {
	byIssuer map[string]*issuerValidator
}

// NewMultiIssuer builds a MultiIssuerValidator, constructing (and JWKS-fetch
// probing) every configured issuer up front so misconfiguration is surfaced
// at start-up rather than on first request.
func NewMultiIssuer(cfg MultiIssuerConfig) (*MultiIssuerValidator, error) {
	if len(cfg) == 0 {
		return nil, errors.New("jwt.NewMultiIssuer: at least one issuer must be configured")
	}

	byIssuer := make(map[string]*issuerValidator, len(cfg))
	for iss, ic := range cfg {
		if iss == "" {
			return nil, errors.New("jwt.NewMultiIssuer: issuer key must not be empty")
		}
		if ic.JwksURL == "" {
			return nil, fmt.Errorf("jwt.NewMultiIssuer: issuer %q: jwks_url is required", iss)
		}
		if len(ic.Audience) == 0 {
			return nil, fmt.Errorf("jwt.NewMultiIssuer: issuer %q: audience is required", iss)
		}
		factory, ok := claimsProfiles[ic.claimsProfile()]
		if !ok {
			return nil, fmt.Errorf("jwt.NewMultiIssuer: issuer %q: unknown claims_profile %q", iss, ic.ClaimsProfile)
		}

		issuerURL, err := url.Parse(iss)
		if err != nil {
			return nil, fmt.Errorf("jwt.NewMultiIssuer: issuer %q: parse issuer URL: %w", iss, err)
		}
		jwksURL, err := url.Parse(ic.JwksURL)
		if err != nil {
			return nil, fmt.Errorf("jwt.NewMultiIssuer: issuer %q: parse jwks_url: %w", iss, err)
		}

		provider := jwks.NewCachingProvider(issuerURL, ic.jwksCacheTTL(), jwks.WithCustomJWKSURI(jwksURL))

		opts := []validator.Option{}
		if ic.LeewaySeconds > 0 {
			opts = append(opts, validator.WithAllowedClockSkew(time.Duration(ic.LeewaySeconds)*time.Second))
		}
		opts = append(opts, validator.WithCustomClaims(factory))

		v, err := validator.New(provider.KeyFunc, ic.algorithm(), iss, ic.Audience, opts...)
		if err != nil {
			return nil, fmt.Errorf("jwt.NewMultiIssuer: issuer %q: %w", iss, err)
		}

		byIssuer[iss] = &issuerValidator{v: v, jwks: provider}
	}

	return &MultiIssuerValidator{byIssuer: byIssuer}, nil
}

// Validate peeks the token's unverified "iss" claim to select the matching
// issuer's validator, then fully validates signature and registered/custom
// claims against it. Returns validator.ErrIssuerNotConfigured-shaped errors
// (via fmt.Errorf, wrapped) for tokens from unregistered issuers.
func (m *MultiIssuerValidator) Validate(ctx context.Context, tokenString string) (*validator.ValidatedClaims, error) {
	iss, err := peekIssuer(tokenString)
	if err != nil {
		return nil, fmt.Errorf("jwt: %w", err)
	}

	iv, ok := m.byIssuer[iss]
	if !ok {
		return nil, fmt.Errorf("jwt: token issuer %q is not configured", iss)
	}

	claims, err := iv.v.ValidateToken(ctx, tokenString)
	if err != nil {
		return nil, fmt.Errorf("jwt: token validation failed: %w", err)
	}
	validated, ok := claims.(*validator.ValidatedClaims)
	if !ok {
		return nil, errors.New("jwt: unexpected claims type from validator")
	}
	return validated, nil
}

// peekIssuer extracts the "iss" claim from a JWS-compact token WITHOUT
// verifying its signature — used only to select which issuer's validator
// (and JWKS) to verify against next. The signature is always checked before
// any claim is trusted.
func peekIssuer(tokenString string) (string, error) {
	sig, err := jose.ParseSigned(tokenString)
	if err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	var payload struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(sig.UnsafePayloadWithoutVerification(), &payload); err != nil {
		return "", fmt.Errorf("decode unverified claims: %w", err)
	}
	if payload.Issuer == "" {
		return "", errors.New("token has no iss claim")
	}
	return payload.Issuer, nil
}
