package headers

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/datakaveri/dx-common-go/auth"
)

func secret(s string) Config { return Config{Secret: []byte(s)} }

func TestSignVerify_RoundTrip(t *testing.T) {
	cfg := secret("k1")
	user := auth.DxUser{
		ID:             "u-1",
		Email:          "u@x.io",
		Roles:          []string{"consumer", "provider"},
		OrganisationID: "org-1",
	}
	h, err := Sign(user, cfg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := Verify(h, cfg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.ID != user.ID || got.Email != user.Email || got.OrganisationID != user.OrganisationID {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	// Roles are returned sorted/normalised.
	if len(got.Roles) != 2 {
		t.Fatalf("roles = %v", got.Roles)
	}
}

func TestSign_RejectsEmptyID(t *testing.T) {
	if _, err := Sign(auth.DxUser{ID: "  "}, secret("k")); err == nil {
		t.Fatal("expected error signing empty id")
	}
}

func TestSign_RejectsSeparatorInjection(t *testing.T) {
	if _, err := Sign(auth.DxUser{ID: "u", Email: "a|b"}, secret("k")); err == nil {
		t.Fatal("expected error for '|' in field")
	}
	if _, err := Sign(auth.DxUser{ID: "u", Roles: []string{"cos_admin,provider"}}, secret("k")); err == nil {
		t.Fatal("expected error for ',' in role")
	}
}

func TestVerify_RejectsTamperedRoles(t *testing.T) {
	cfg := secret("k1")
	h, _ := Sign(auth.DxUser{ID: "u-1", Roles: []string{"consumer"}}, cfg)
	// Attacker escalates roles without re-signing.
	h.Set(HdrSubjectRoles, "cos_admin")
	if _, err := Verify(h, cfg); err == nil {
		t.Fatal("expected signature failure after role tampering")
	}
}

func TestVerify_RejectsWrongSecret(t *testing.T) {
	h, _ := Sign(auth.DxUser{ID: "u-1"}, secret("k1"))
	if _, err := Verify(h, secret("k2")); err == nil {
		t.Fatal("expected failure with wrong secret")
	}
}

func TestVerify_MissingSignature(t *testing.T) {
	if _, err := Verify(http.Header{}, secret("k")); err != ErrNotSigned {
		t.Fatalf("expected ErrNotSigned, got %v", err)
	}
}

func TestVerify_RejectsExpired(t *testing.T) {
	cfg := Config{Secret: []byte("k"), MaxAge: 30 * time.Second}
	h, _ := Sign(auth.DxUser{ID: "u-1"}, cfg)
	// Backdate issued-at beyond MaxAge and re-sign with the real secret so only
	// freshness (not the signature) is the failing condition.
	old := time.Now().Add(-time.Hour).Unix()
	h.Set(HdrSubjectIssuedAt, strconv.FormatInt(old, 10))
	canonical := canonicalString(h.Get(HdrSubjectID), "", "", "", "", "", old)
	h.Set(HdrSubjectSig, hmacHex(cfg.Secret, canonical))
	if _, err := Verify(h, cfg); err == nil {
		t.Fatal("expected expiry failure")
	}
}

func TestVerify_RejectsBlankSignedID(t *testing.T) {
	// Forge a validly-signed but blank id: signer can't produce this, but verify
	// must still refuse it.
	cfg := secret("k")
	now := time.Now().Unix()
	canonical := canonicalString("", "", "", "", "", "", now)
	h := http.Header{}
	h.Set(HdrSubjectID, "")
	h.Set(HdrSubjectIssuedAt, strconv.FormatInt(now, 10))
	h.Set(HdrSubjectSig, hmacHex(cfg.Secret, canonical))
	if _, err := Verify(h, cfg); err == nil {
		t.Fatal("expected rejection of validly-signed blank id")
	}
}

func TestSignVerify_AgentRoundTrip(t *testing.T) {
	cfg := secret("k1")
	user := auth.DxUser{
		ID:           "u-1",
		AgentSubject: "agent-7a1b",
		DelegationID: "dg-01",
	}
	h, err := Sign(user, cfg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if h.Get(HdrAgentSubject) != "agent-7a1b" || h.Get(HdrDelegationID) != "dg-01" {
		t.Fatalf("agent headers not minted: %v", h)
	}
	got, err := Verify(h, cfg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.AgentSubject != user.AgentSubject || got.DelegationID != user.DelegationID {
		t.Fatalf("agent roundtrip mismatch: %+v", got)
	}
	if !got.IsAgent() {
		t.Fatal("IsAgent() = false for delegated user")
	}
}

func TestVerify_RejectsInjectedAgentHeaders(t *testing.T) {
	// A plain user signature must not verify once agent headers are bolted on.
	cfg := secret("k1")
	h, _ := Sign(auth.DxUser{ID: "u-1"}, cfg)
	h.Set(HdrAgentSubject, "agent-evil")
	if _, err := Verify(h, cfg); err == nil {
		t.Fatal("expected signature failure after agent-header injection")
	}
}

func TestVerify_RejectsStrippedAgentHeaders(t *testing.T) {
	// Dropping the agent headers from a delegated signature must break it —
	// otherwise an agent call could masquerade as a direct user call.
	cfg := secret("k1")
	h, _ := Sign(auth.DxUser{ID: "u-1", AgentSubject: "agent-7a1b", DelegationID: "dg-01"}, cfg)
	h.Del(HdrAgentSubject)
	h.Del(HdrDelegationID)
	if _, err := Verify(h, cfg); err == nil {
		t.Fatal("expected signature failure after agent-header stripping")
	}
}

func TestSignVerify_LegacyCompatibility(t *testing.T) {
	// A signature computed by pre-agent code (5-field canonical) must still
	// verify: rebuilds are not atomic across the fleet.
	cfg := secret("k1")
	now := time.Now().Unix()
	legacyCanonical := "u-1||||" + strconv.FormatInt(now, 10)
	h := http.Header{}
	h.Set(HdrSubjectID, "u-1")
	h.Set(HdrSubjectIssuedAt, strconv.FormatInt(now, 10))
	h.Set(HdrSubjectSig, hmacHex(cfg.Secret, legacyCanonical))
	if _, err := Verify(h, cfg); err != nil {
		t.Fatalf("legacy signature no longer verifies: %v", err)
	}
}

func TestSign_RejectsSeparatorInAgentFields(t *testing.T) {
	if _, err := Sign(auth.DxUser{ID: "u", AgentSubject: "a|b"}, secret("k")); err == nil {
		t.Fatal("expected error for '|' in agent subject")
	}
}

func TestVerify_KeyRotation(t *testing.T) {
	// Signed with the old key; verifier accepts it via AdditionalSecrets.
	signed, _ := Sign(auth.DxUser{ID: "u-1"}, secret("old"))
	rotated := Config{Secret: []byte("new"), AdditionalSecrets: [][]byte{[]byte("old")}}
	if _, err := Verify(signed, rotated); err != nil {
		t.Fatalf("rotation verify failed: %v", err)
	}
}
