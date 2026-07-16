package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "gopkg.in/go-jose/go-jose.v2"
)

// issuerFixture stands up a JWKS endpoint for one issuer and can mint tokens
// signed by its key.
type issuerFixture struct {
	iss string
	aud string
	srv *httptest.Server
	key *rsa.PrivateKey
}

func newIssuerFixture(t *testing.T, iss, aud string) *issuerFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	jwk := jose.JSONWebKey{Key: key.Public(), KeyID: testKID, Algorithm: "RS256", Use: "sig"}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	return &issuerFixture{iss: iss, aud: aud, srv: srv, key: key}
}

// sign mints a token combining registered claims (iss/aud/sub/exp/iat) and
// arbitrary custom claims into one flat JSON payload, signed RS256.
func (f *issuerFixture) sign(t *testing.T, custom map[string]any) string {
	t.Helper()

	payload := map[string]any{
		"iss": f.iss,
		"aud": []string{f.aud},
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	}
	for k, v := range custom {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithHeader("kid", testKID).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	sig, err := signer.Sign(body)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	out, err := sig.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return out
}

func TestMultiIssuer_RoutesToCorrectIssuer(t *testing.T) {
	a := newIssuerFixture(t, "https://issuer-a.example/realms/a", "account")
	b := newIssuerFixture(t, "https://issuer-b.example/realms/b", "account")

	mv, err := NewMultiIssuer(MultiIssuerConfig{
		a.iss: {JwksURL: a.srv.URL, Audience: []string{a.aud}},
		b.iss: {JwksURL: b.srv.URL, Audience: []string{b.aud}},
	})
	if err != nil {
		t.Fatalf("NewMultiIssuer: %v", err)
	}

	tokA := a.sign(t, map[string]any{"email": "alice@a.example", "realm_access": map[string]any{"roles": []string{"consumer"}}})
	claims, err := mv.Validate(context.Background(), tokA)
	if err != nil {
		t.Fatalf("issuer A token rejected: %v", err)
	}
	custom, ok := claims.CustomClaims.(*DxCustomClaims)
	if !ok {
		t.Fatalf("CustomClaims type = %T, want *DxCustomClaims", claims.CustomClaims)
	}
	if custom.Email != "alice@a.example" {
		t.Fatalf("email = %q", custom.Email)
	}
	if len(custom.AllRoles()) != 1 || custom.AllRoles()[0] != "consumer" {
		t.Fatalf("roles = %v", custom.AllRoles())
	}
	if claims.RegisteredClaims.Issuer != a.iss {
		t.Fatalf("issuer = %q, want %q", claims.RegisteredClaims.Issuer, a.iss)
	}

	tokB := b.sign(t, map[string]any{"email": "bob@b.example"})
	claimsB, err := mv.Validate(context.Background(), tokB)
	if err != nil {
		t.Fatalf("issuer B token rejected: %v", err)
	}
	if claimsB.RegisteredClaims.Issuer != b.iss {
		t.Fatalf("issuer = %q, want %q", claimsB.RegisteredClaims.Issuer, b.iss)
	}
}

func TestMultiIssuer_RejectsUnknownIssuer(t *testing.T) {
	a := newIssuerFixture(t, "https://issuer-a.example/realms/a", "account")
	mv, err := NewMultiIssuer(MultiIssuerConfig{
		a.iss: {JwksURL: a.srv.URL, Audience: []string{a.aud}},
	})
	if err != nil {
		t.Fatalf("NewMultiIssuer: %v", err)
	}

	stranger := newIssuerFixture(t, "https://not-configured.example/realms/x", "account")
	tok := stranger.sign(t, nil)
	if _, err := mv.Validate(context.Background(), tok); err == nil {
		t.Fatal("expected token from unconfigured issuer to be rejected")
	}
}

func TestMultiIssuer_RejectsWrongAudience(t *testing.T) {
	a := newIssuerFixture(t, "https://issuer-a.example/realms/a", "account")
	mv, err := NewMultiIssuer(MultiIssuerConfig{
		a.iss: {JwksURL: a.srv.URL, Audience: []string{"expected-aud"}},
	})
	if err != nil {
		t.Fatalf("NewMultiIssuer: %v", err)
	}

	tok := a.sign(t, nil) // signed with aud "account", not "expected-aud"
	if _, err := mv.Validate(context.Background(), tok); err == nil {
		t.Fatal("expected wrong-audience token to be rejected")
	}
}

func TestMultiIssuer_DecodesCnfClaim(t *testing.T) {
	a := newIssuerFixture(t, "https://issuer-a.example/realms/a", "account")
	mv, err := NewMultiIssuer(MultiIssuerConfig{
		a.iss: {JwksURL: a.srv.URL, Audience: []string{a.aud}},
	})
	if err != nil {
		t.Fatalf("NewMultiIssuer: %v", err)
	}

	tok := a.sign(t, map[string]any{"cnf": map[string]any{"jkt": "thumbprint-value"}})
	claims, err := mv.Validate(context.Background(), tok)
	if err != nil {
		t.Fatalf("token rejected: %v", err)
	}
	custom := claims.CustomClaims.(*DxCustomClaims)
	if custom.Cnf == nil || custom.Cnf.Jkt != "thumbprint-value" {
		t.Fatalf("cnf = %+v", custom.Cnf)
	}
}

func TestNewMultiIssuer_RequiresAtLeastOneIssuer(t *testing.T) {
	if _, err := NewMultiIssuer(MultiIssuerConfig{}); err == nil {
		t.Fatal("expected empty config to be rejected")
	}
}

func TestNewMultiIssuer_RejectsUnknownClaimsProfile(t *testing.T) {
	_, err := NewMultiIssuer(MultiIssuerConfig{
		"https://issuer.example": {JwksURL: "https://issuer.example/jwks", Audience: []string{"aud"}, ClaimsProfile: "does-not-exist"},
	})
	if err == nil {
		t.Fatal("expected unknown claims profile to be rejected")
	}
}
