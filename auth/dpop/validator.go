package dpop

import (
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	jose "gopkg.in/square/go-jose.v2"

	"github.com/datakaveri/dx-common-go/cache"
)

// Validator validates DPoP proofs and tracks seen "jti" values to reject
// replays.
type Validator struct {
	cfg     Config
	algs    map[string]struct{}
	maxAge  time.Duration
	leeway  time.Duration
	jtiTTL  time.Duration
	jtiPfx  string
	jtiKeep cache.Cache
}

// New creates a Validator from cfg.
func New(cfg Config) (*Validator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("dpop.New: %w", err)
	}
	algs := make(map[string]struct{})
	for _, a := range cfg.allowedAlgorithms() {
		algs[a] = struct{}{}
	}
	maxAge := cfg.maxAge()
	leeway := cfg.leeway()
	return &Validator{
		cfg:     cfg,
		algs:    algs,
		maxAge:  maxAge,
		leeway:  leeway,
		jtiTTL:  maxAge + 2*leeway,
		jtiPfx:  "dpop:jti:",
		jtiKeep: cfg.Cache,
	}, nil
}

// Validate parses and verifies a DPoP proof (the raw value of the "DPoP"
// request header). method and htu are the HTTP method and target URI
// (scheme + host + path, no query/fragment) of the request the proof is
// attached to — the caller is responsible for reconstructing an absolute htu
// since a server-side *http.Request rarely carries scheme/host directly.
//
// On success, the proof's "jti" is recorded so a second presentation of the
// same proof is rejected as a replay.
func (v *Validator) Validate(ctx context.Context, proofToken, method, htu string) (*Proof, error) {
	sig, err := jose.ParseSigned(proofToken)
	if err != nil {
		return nil, fmt.Errorf("dpop: parse proof: %w", err)
	}
	if len(sig.Signatures) != 1 {
		return nil, errors.New("dpop: proof must have exactly one signature")
	}
	header := sig.Signatures[0].Header

	typ, _ := header.ExtraHeaders[jose.HeaderKey("typ")].(string)
	if typ != "dpop+jwt" {
		return nil, fmt.Errorf("dpop: unexpected typ %q, want \"dpop+jwt\"", typ)
	}

	if _, ok := v.algs[header.Algorithm]; !ok {
		return nil, fmt.Errorf("dpop: algorithm %q not allowed", header.Algorithm)
	}

	jwk := header.JSONWebKey
	if jwk == nil || !jwk.Valid() {
		return nil, errors.New("dpop: proof is missing a valid embedded jwk header")
	}
	if !jwk.IsPublic() {
		return nil, errors.New("dpop: proof's embedded jwk must be a public key")
	}

	// The proof is self-signed: verified against the very key it embeds. This
	// establishes possession of the private key, not trust in the key itself —
	// trust comes from binding jkt to the access token's "cnf.jkt" via
	// CheckBinding.
	payload, err := sig.Verify(jwk)
	if err != nil {
		return nil, fmt.Errorf("dpop: signature verification failed: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("dpop: decode claims: %w", err)
	}

	if claims.Htm != method {
		return nil, fmt.Errorf("dpop: htm %q does not match request method %q", claims.Htm, method)
	}
	if claims.Htu != htu {
		return nil, fmt.Errorf("dpop: htu %q does not match request URI %q", claims.Htu, htu)
	}
	if claims.Jti == "" {
		return nil, errors.New("dpop: proof is missing jti")
	}

	if err := v.checkFreshness(claims.Iat); err != nil {
		return nil, err
	}

	thumb, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("dpop: compute jwk thumbprint: %w", err)
	}

	if err := v.checkReplay(ctx, base64.RawURLEncoding.EncodeToString(thumb), claims.Jti); err != nil {
		return nil, err
	}

	return &Proof{
		Claims: claims,
		JKT:    base64.RawURLEncoding.EncodeToString(thumb),
	}, nil
}

func (v *Validator) checkFreshness(iat int64) error {
	age := time.Since(time.Unix(iat, 0))
	if age > v.maxAge+v.leeway {
		return fmt.Errorf("dpop: proof is stale (iat %ds old, max age %s)", int64(age.Seconds()), v.maxAge)
	}
	if age < -v.leeway {
		return errors.New("dpop: proof iat is in the future")
	}
	return nil
}

func (v *Validator) checkReplay(ctx context.Context, jkt, jti string) error {
	key := v.jtiPfx + jkt + ":" + jti
	seen, err := v.jtiKeep.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("dpop: replay check: %w", err)
	}
	if seen {
		return errors.New("dpop: proof jti has already been used (replay)")
	}
	if err := v.jtiKeep.Set(ctx, key, "1", v.jtiTTL); err != nil {
		return fmt.Errorf("dpop: record jti: %w", err)
	}
	return nil
}

// CheckBinding verifies that proof was created with the private key
// corresponding to expectedJkt — typically the "cnf.jkt" claim of the access
// token presented alongside it. A mismatch means the caller possesses a valid
// access token but not the key it's bound to.
func (v *Validator) CheckBinding(proof *Proof, expectedJkt string) error {
	if expectedJkt == "" {
		return errors.New("dpop: access token has no cnf.jkt to bind against")
	}
	if proof.JKT != expectedJkt {
		return errors.New("dpop: proof key does not match access token's cnf.jkt")
	}
	return nil
}

// CheckAccessTokenHash verifies the proof's "ath" claim against accessToken,
// per RFC 9449 §4.2. Use when the DPoP proof is expected to be bound to a
// specific access token presentation (recommended for every resource request,
// distinct from the token-endpoint proof which has no "ath").
func (v *Validator) CheckAccessTokenHash(proof *Proof, accessToken string) error {
	sum := sha256.Sum256([]byte(accessToken))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if proof.Claims.Ath != want {
		return errors.New("dpop: proof ath does not match presented access token")
	}
	return nil
}
