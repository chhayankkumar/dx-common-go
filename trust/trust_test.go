package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestVerifyTrustedAndUntrusted(t *testing.T) {
	caCert, caKey := makeCA(t, "Country-A National CA")
	leaf, _ := makeLeaf(t, caCert, caKey, "node-a.border.gov", big.NewInt(1001))

	otherCA, otherKey := makeCA(t, "Rogue CA")
	rogue, _ := makeLeaf(t, otherCA, otherKey, "rogue.example", big.NewInt(2002))

	store := New(nil)
	store.Replace([]*x509.Certificate{caCert}, nil)

	if err := store.Verify(leaf, nil); err != nil {
		t.Fatalf("trusted leaf should verify: %v", err)
	}
	if err := store.Verify(rogue, nil); err == nil {
		t.Fatal("leaf from untrusted CA must be rejected")
	}
}

func TestVerifyEmptyStoreFailsClosed(t *testing.T) {
	caCert, caKey := makeCA(t, "CA")
	leaf, _ := makeLeaf(t, caCert, caKey, "node", big.NewInt(1))
	if err := New(nil).Verify(leaf, nil); err == nil {
		t.Fatal("empty store must fail closed")
	}
}

func TestVerifyRevoked(t *testing.T) {
	caCert, caKey := makeCA(t, "CA")
	leaf, _ := makeLeaf(t, caCert, caKey, "node", big.NewInt(7))

	crl := makeCRL(t, caCert, caKey, []*x509.Certificate{leaf})
	store := New(nil)
	store.Replace([]*x509.Certificate{caCert}, []*x509.RevocationList{crl})

	if err := store.Verify(leaf, nil); err == nil {
		t.Fatal("revoked leaf must be rejected")
	}
}

func TestPolicyRejection(t *testing.T) {
	caCert, caKey := makeCA(t, "CA")
	leaf, _ := makeLeaf(t, caCert, caKey, "blocked.node", big.NewInt(9))

	store := New(func(c *x509.Certificate) error {
		if c.Subject.CommonName == "blocked.node" {
			return errors.New("blocked by policy")
		}
		return nil
	})
	store.Replace([]*x509.Certificate{caCert}, nil)

	if err := store.Verify(leaf, nil); err == nil {
		t.Fatal("policy must reject the blocked node")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func makeCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ca: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}
	return cert, key
}

func makeLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, serial *big.Int) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return cert, key
}

func makeCRL(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, revoked []*x509.Certificate) *x509.RevocationList {
	t.Helper()
	entries := make([]x509.RevocationListEntry, 0, len(revoked))
	for _, c := range revoked {
		entries = append(entries, x509.RevocationListEntry{
			SerialNumber:   c.SerialNumber,
			RevocationTime: time.Now(),
		})
	}
	tmpl := &x509.RevocationList{
		Number:                    big.NewInt(1),
		ThisUpdate:                time.Now().Add(-time.Minute),
		NextUpdate:                time.Now().Add(time.Hour),
		RevokedCertificateEntries: entries,
	}
	der, err := x509.CreateRevocationList(rand.Reader, tmpl, ca, caKey)
	if err != nil {
		t.Fatalf("create crl: %v", err)
	}
	crl, err := x509.ParseRevocationList(der)
	if err != nil {
		t.Fatalf("parse crl: %v", err)
	}
	return crl
}
