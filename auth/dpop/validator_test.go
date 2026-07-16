package dpop

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	jose "gopkg.in/square/go-jose.v2"

	"github.com/datakaveri/dx-common-go/cache"
)

func athOf(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const (
	testMethod = "POST"
	testHTU    = "https://gateway.example/iudx/v2/resource_servers"
)

func newTestValidator(t *testing.T) *Validator {
	t.Helper()
	v, err := New(Config{
		Enabled:       true,
		MaxAge:        60 * time.Second,
		LeewaySeconds: 5,
		Cache:         cache.NewMemoryCache(),
	})
	if err != nil {
		t.Fatalf("New validator: %v", err)
	}
	return v
}

type proofOpts struct {
	typ    string
	alg    jose.SignatureAlgorithm
	htm    string
	htu    string
	iat    int64
	jti    string
	ath    string
	noJWK  bool
	badJWK bool // embed a JWK the signer key doesn't actually match
}

func signProof(t *testing.T, o proofOpts) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}

	pub := jose.JSONWebKey{Key: key.Public(), Algorithm: "ES256", Use: "sig"}
	if o.badJWK {
		otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("genkey: %v", err)
		}
		pub = jose.JSONWebKey{Key: otherKey.Public(), Algorithm: "ES256", Use: "sig"}
	}

	extra := map[jose.HeaderKey]interface{}{}
	typ := o.typ
	if typ == "" {
		typ = "dpop+jwt"
	}
	extra["typ"] = typ

	signerOpts := &jose.SignerOptions{ExtraHeaders: extra}
	if !o.noJWK {
		signerOpts = signerOpts.WithHeader("jwk", pub)
	}

	alg := o.alg
	if alg == "" {
		alg = jose.ES256
	}

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key}, signerOpts)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	claims := Claims{
		Htm: o.htm,
		Htu: o.htu,
		Iat: o.iat,
		Jti: o.jti,
		Ath: o.ath,
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	sig, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	out, err := sig.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return out
}

func baseProof(t *testing.T) string {
	return signProof(t, proofOpts{
		htm: testMethod,
		htu: testHTU,
		iat: time.Now().Unix(),
		jti: "jti-1",
	})
}

func TestValidate_ValidProof(t *testing.T) {
	v := newTestValidator(t)
	proof, err := v.Validate(context.Background(), baseProof(t), testMethod, testHTU)
	if err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}
	if proof.JKT == "" {
		t.Fatal("expected non-empty jkt")
	}
	if proof.Claims.Jti != "jti-1" {
		t.Fatalf("jti = %q", proof.Claims.Jti)
	}
}

func TestValidate_RejectsReplay(t *testing.T) {
	v := newTestValidator(t)
	proof := baseProof(t)
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err != nil {
		t.Fatalf("first use rejected: %v", err)
	}
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected replayed proof to be rejected")
	}
}

func TestValidate_RejectsStaleIat(t *testing.T) {
	v := newTestValidator(t)
	proof := signProof(t, proofOpts{
		htm: testMethod, htu: testHTU, jti: "jti-2",
		iat: time.Now().Add(-5 * time.Minute).Unix(),
	})
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected stale proof to be rejected")
	}
}

func TestValidate_RejectsFutureIat(t *testing.T) {
	v := newTestValidator(t)
	proof := signProof(t, proofOpts{
		htm: testMethod, htu: testHTU, jti: "jti-3",
		iat: time.Now().Add(5 * time.Minute).Unix(),
	})
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected future-dated proof to be rejected")
	}
}

func TestValidate_RejectsWrongHtu(t *testing.T) {
	v := newTestValidator(t)
	proof := signProof(t, proofOpts{
		htm: testMethod, htu: "https://gateway.example/other/path",
		iat: time.Now().Unix(), jti: "jti-4",
	})
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected wrong-htu proof to be rejected")
	}
}

func TestValidate_RejectsWrongMethod(t *testing.T) {
	v := newTestValidator(t)
	proof := signProof(t, proofOpts{
		htm: "GET", htu: testHTU,
		iat: time.Now().Unix(), jti: "jti-5",
	})
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected wrong-method proof to be rejected")
	}
}

func TestValidate_RejectsWrongTyp(t *testing.T) {
	v := newTestValidator(t)
	proof := signProof(t, proofOpts{
		typ: "JWT", htm: testMethod, htu: testHTU,
		iat: time.Now().Unix(), jti: "jti-6",
	})
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected non-dpop typ to be rejected")
	}
}

func TestValidate_RejectsMissingJWK(t *testing.T) {
	v := newTestValidator(t)
	proof := signProof(t, proofOpts{
		htm: testMethod, htu: testHTU,
		iat: time.Now().Unix(), jti: "jti-7", noJWK: true,
	})
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected proof without embedded jwk to be rejected")
	}
}

func TestValidate_RejectsKeyMismatch(t *testing.T) {
	v := newTestValidator(t)
	proof := signProof(t, proofOpts{
		htm: testMethod, htu: testHTU,
		iat: time.Now().Unix(), jti: "jti-8", badJWK: true,
	})
	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected proof signed by a key other than the embedded jwk to be rejected")
	}
}

// Algorithm confusion: an RS256-signed proof must be rejected even if it's
// otherwise well-formed, since RS256 isn't in the default allowlist.
func TestValidate_RejectsDisallowedAlgorithm(t *testing.T) {
	v := newTestValidator(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	pub := jose.JSONWebKey{Key: key.Public(), Algorithm: "RS256", Use: "sig"}
	signerOpts := (&jose.SignerOptions{ExtraHeaders: map[jose.HeaderKey]interface{}{"typ": "dpop+jwt"}}).WithHeader("jwk", pub)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, signerOpts)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	claims := Claims{Htm: testMethod, Htu: testHTU, Iat: time.Now().Unix(), Jti: "jti-9"}
	payload, _ := json.Marshal(claims)
	sig, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	proof, err := sig.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	if _, err := v.Validate(context.Background(), proof, testMethod, testHTU); err == nil {
		t.Fatal("expected RS256 proof to be rejected")
	}
}

func TestCheckBinding(t *testing.T) {
	v := newTestValidator(t)
	proof, err := v.Validate(context.Background(), baseProof(t), testMethod, testHTU)
	if err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}
	if err := v.CheckBinding(proof, proof.JKT); err != nil {
		t.Fatalf("matching jkt rejected: %v", err)
	}
	if err := v.CheckBinding(proof, "some-other-jkt"); err == nil {
		t.Fatal("expected mismatched jkt to be rejected")
	}
	if err := v.CheckBinding(proof, ""); err == nil {
		t.Fatal("expected empty expectedJkt to be rejected")
	}
}

func TestCheckAccessTokenHash(t *testing.T) {
	v := newTestValidator(t)
	accessToken := "some.access.token"
	proof := signProof(t, proofOpts{
		htm: testMethod, htu: testHTU, jti: "jti-10", iat: time.Now().Unix(),
		ath: athOf(accessToken),
	})
	p, err := v.Validate(context.Background(), proof, testMethod, testHTU)
	if err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}
	if err := v.CheckAccessTokenHash(p, accessToken); err != nil {
		t.Fatalf("matching ath rejected: %v", err)
	}
	if err := v.CheckAccessTokenHash(p, "different.token"); err == nil {
		t.Fatal("expected mismatched ath to be rejected")
	}
}

func TestConfig_ValidateRequiresCache(t *testing.T) {
	cfg := Config{Enabled: true}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing cache to be rejected")
	}
}

func TestConfig_ValidateRejectsExcessiveLeeway(t *testing.T) {
	cfg := Config{Enabled: true, LeewaySeconds: maxLeewaySeconds + 1, Cache: cache.NewMemoryCache()}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected excessive leeway to be rejected")
	}
}
