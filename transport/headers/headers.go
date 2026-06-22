// Package headers signs and verifies the internal subject headers the gateway
// mints for upstream services.
//
// Why: the gateway validates the user's JWT, but forwarding that JWT to every
// upstream is a leak surface and couples each service to Keycloak availability.
// Instead, the gateway extracts the resolved user and signs a small set of
// X-Subject-* headers with an HMAC. Upstreams trust the gateway's signature
// and skip the JWT round-trip.
//
// Headers minted:
//
//	X-Subject-Id          stable user identifier (sub claim)
//	X-Subject-Email       optional
//	X-Subject-Roles       comma-joined realm roles
//	X-Subject-Org-Id      organisation the user belongs to
//	X-Subject-Issued-At   Unix seconds when these headers were minted
//	X-Subject-Sig         hex(HMAC-SHA256(canonical, shared_secret))
//
// The signature covers the canonical string:
//
//	id|email|roles|org|issued_at
//
// Replay protection: Verify rejects anything older than MaxAge (default 60s).
// Rotate the shared secret by accepting two keys during rollover.
package headers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/datakaveri/dx-common-go/auth"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// Header names — exported so callers can also strip/inspect them.
const (
	HdrSubjectID       = "X-Subject-Id"
	HdrSubjectEmail    = "X-Subject-Email"
	HdrSubjectRoles    = "X-Subject-Roles"
	HdrSubjectOrgID    = "X-Subject-Org-Id"
	HdrSubjectIssuedAt = "X-Subject-Issued-At"
	HdrSubjectSig      = "X-Subject-Sig"
)

// DefaultMaxAge is the validity window applied when Config.MaxAge is zero.
const DefaultMaxAge = 60 * time.Second

// Config controls Signer/Verifier behaviour.
type Config struct {
	// Secret is the active HMAC key used by Sign and accepted by Verify.
	Secret []byte
	// AdditionalSecrets are also accepted during Verify (for key rotation).
	// Sign always uses Secret.
	AdditionalSecrets [][]byte
	// MaxAge bounds how stale headers can be. Defaults to 60s.
	MaxAge time.Duration
}

// ErrNotSigned indicates the request has no X-Subject-Sig header.
var ErrNotSigned = errors.New("request has no subject signature")

// ErrInvalidSignature indicates a signature mismatch (or expired headers).
var ErrInvalidSignature = errors.New("invalid subject signature")

// Sign returns the X-Subject-* headers for a user. Caller copies them onto
// the outgoing request via h.Set(name, value).
func Sign(user auth.DxUser, cfg Config) (http.Header, error) {
	if len(cfg.Secret) == 0 {
		return nil, errors.New("headers.Sign: Secret is required")
	}
	now := time.Now().Unix()
	rolesJoined := joinRoles(user.Roles)
	canonical := canonicalString(user.ID, user.Email, rolesJoined, user.OrganisationID, now)
	sig := hmacHex(cfg.Secret, canonical)

	h := http.Header{}
	h.Set(HdrSubjectID, user.ID)
	if user.Email != "" {
		h.Set(HdrSubjectEmail, user.Email)
	}
	if rolesJoined != "" {
		h.Set(HdrSubjectRoles, rolesJoined)
	}
	if user.OrganisationID != "" {
		h.Set(HdrSubjectOrgID, user.OrganisationID)
	}
	h.Set(HdrSubjectIssuedAt, strconv.FormatInt(now, 10))
	h.Set(HdrSubjectSig, sig)
	return h, nil
}

// Apply copies all signed headers onto an outbound request, overwriting any
// existing values for those header names.
func Apply(req *http.Request, signed http.Header) {
	for k := range signed {
		req.Header.Set(k, signed.Get(k))
	}
}

// Verify checks the signature + freshness and returns the user encoded in
// the headers.
func Verify(h http.Header, cfg Config) (auth.DxUser, error) {
	sig := h.Get(HdrSubjectSig)
	if sig == "" {
		return auth.DxUser{}, ErrNotSigned
	}

	issued := h.Get(HdrSubjectIssuedAt)
	issuedAt, err := strconv.ParseInt(issued, 10, 64)
	if err != nil {
		return auth.DxUser{}, fmt.Errorf("%w: bad issued_at", ErrInvalidSignature)
	}

	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = DefaultMaxAge
	}
	age := time.Since(time.Unix(issuedAt, 0))
	if age < -10*time.Second || age > maxAge {
		return auth.DxUser{}, fmt.Errorf("%w: outside validity window (age=%s)", ErrInvalidSignature, age)
	}

	id := h.Get(HdrSubjectID)
	email := h.Get(HdrSubjectEmail)
	roles := h.Get(HdrSubjectRoles)
	org := h.Get(HdrSubjectOrgID)
	canonical := canonicalString(id, email, roles, org, issuedAt)

	if !verifyAgainst(cfg.Secret, canonical, sig) {
		for _, alt := range cfg.AdditionalSecrets {
			if verifyAgainst(alt, canonical, sig) {
				return makeUser(id, email, roles, org), nil
			}
		}
		return auth.DxUser{}, ErrInvalidSignature
	}
	return makeUser(id, email, roles, org), nil
}

// Middleware verifies subject headers on inbound requests and injects the
// resolved DxUser into the request context. Requests without a signature
// (or with an invalid one) get 401.
//
// Upstreams that sit *behind* the gateway use this middleware instead of
// validating the JWT themselves.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := Verify(r.Header, cfg)
			if err != nil {
				dxerrors.WriteError(w, dxerrors.NewUnauthorized("invalid or missing subject headers"))
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
		})
	}
}

// --- internals --------------------------------------------------------------

func canonicalString(id, email, roles, org string, issuedAt int64) string {
	return strings.Join([]string{id, email, roles, org, strconv.FormatInt(issuedAt, 10)}, "|")
}

func joinRoles(roles []string) string {
	if len(roles) == 0 {
		return ""
	}
	// Sort so callers can't accidentally vary order and break verification.
	sorted := append([]string(nil), roles...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

func splitRoles(joined string) []string {
	if joined == "" {
		return nil
	}
	return strings.Split(joined, ",")
}

func hmacHex(key []byte, data string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

func verifyAgainst(key []byte, canonical, expected string) bool {
	if len(key) == 0 {
		return false
	}
	got, err := hex.DecodeString(expected)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(canonical))
	return hmac.Equal(m.Sum(nil), got)
}

func makeUser(id, email, roles, org string) auth.DxUser {
	return auth.DxUser{
		ID:             id,
		Email:          email,
		Roles:          splitRoles(roles),
		OrganisationID: org,
	}
}
