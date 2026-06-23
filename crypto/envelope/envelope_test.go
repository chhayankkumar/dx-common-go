package envelope

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	recipient, err := GenerateKey()
	if err != nil {
		t.Fatalf("recipient key: %v", err)
	}
	signer, err := GenerateKey()
	if err != nil {
		t.Fatalf("signer key: %v", err)
	}

	msg := []byte(`{"userId":"urn:dx:usr:abc","biometric":"…","stay":"30d"}`)
	token, err := Seal(msg, &recipient.PublicKey, signer)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := Open(token, recipient, &signer.PublicKey)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, msg)
	}
}

func TestOpenRejectsWrongSigner(t *testing.T) {
	recipient, _ := GenerateKey()
	signer, _ := GenerateKey()
	attacker, _ := GenerateKey()

	token, err := Seal([]byte("secret"), &recipient.PublicKey, signer)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := Open(token, recipient, &attacker.PublicKey); err == nil {
		t.Fatal("expected signature verification failure with wrong signer key")
	}
}

func TestOpenRejectsWrongRecipient(t *testing.T) {
	recipient, _ := GenerateKey()
	wrong, _ := GenerateKey()
	signer, _ := GenerateKey()

	token, err := Seal([]byte("secret"), &recipient.PublicKey, signer)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := Open(token, wrong, &signer.PublicKey); err == nil {
		t.Fatal("expected decryption failure with wrong recipient key")
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	recipient, _ := GenerateKey()
	signer, _ := GenerateKey()

	token, err := Seal([]byte("secret"), &recipient.PublicKey, signer)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	// Flip a character mid-token; the signature must catch it.
	tampered := token[:len(token)-4] + flip(token[len(token)-4:])
	if _, err := Open(tampered, recipient, &signer.PublicKey); err == nil {
		t.Fatal("expected failure opening a tampered token")
	}
}

func TestKeyPEMRoundTrip(t *testing.T) {
	key, _ := GenerateKey()
	privPEM, err := MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatalf("marshal priv: %v", err)
	}
	pubPEM, err := MarshalPublicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	if !strings.Contains(string(privPEM), "PRIVATE KEY") {
		t.Fatal("private PEM missing header")
	}
	gotPriv, err := ParsePrivateKeyPEM(privPEM)
	if err != nil {
		t.Fatalf("parse priv: %v", err)
	}
	gotPub, err := ParsePublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("parse pub: %v", err)
	}
	if KeyID(&gotPriv.PublicKey) != KeyID(gotPub) {
		t.Fatal("key id mismatch after PEM round-trip")
	}
}

func flip(s string) string {
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}
