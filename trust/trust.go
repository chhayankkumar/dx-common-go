package trust

import (
	"crypto/x509"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// PolicyFunc is an optional extra check applied to a peer leaf certificate after
// it has chained to a trusted anchor and passed revocation checks. Returning a
// non-nil error rejects the peer. Use it for service-specific rules (allowed
// subjects, country codes, node identifiers).
type PolicyFunc func(leaf *x509.Certificate) error

// snapshot is an immutable view of the trust material, swapped atomically.
type snapshot struct {
	pool    *x509.CertPool
	anchors []*x509.Certificate
	crls    []*x509.RevocationList
}

// Store is a hot-swappable trust store. The zero value is not usable; call New.
// All methods are safe for concurrent use.
type Store struct {
	snap   atomic.Pointer[snapshot]
	policy PolicyFunc
}

// New returns an empty Store. Until Replace is called it trusts nothing (every
// verification fails closed), which is the safe default before the first sync.
func New(policy PolicyFunc) *Store {
	s := &Store{policy: policy}
	s.snap.Store(&snapshot{pool: x509.NewCertPool()})
	return s
}

// Replace atomically swaps the trusted anchors and revocation lists. In-flight
// verifications continue against the previous snapshot; new ones use this one.
func (s *Store) Replace(anchors []*x509.Certificate, crls []*x509.RevocationList) {
	pool := x509.NewCertPool()
	for _, a := range anchors {
		pool.AddCert(a)
	}
	s.snap.Store(&snapshot{
		pool:    pool,
		anchors: anchors,
		crls:    crls,
	})
}

// Anchors returns the trusted CA certificates in the current snapshot.
func (s *Store) Anchors() []*x509.Certificate {
	return s.snap.Load().anchors
}

// ClientCAs returns the current trust pool (satisfies mtls.TrustProvider).
func (s *Store) ClientCAs() *x509.CertPool {
	return s.snap.Load().pool
}

// Verify builds a chain from leaf to a trusted anchor, rejects it if revoked by
// any held CRL, and applies the optional policy. It does not perform hostname
// verification — node trust fabrics authenticate on certificate identity.
func (s *Store) Verify(leaf *x509.Certificate, intermediates *x509.CertPool) error {
	snap := s.snap.Load()
	if len(snap.anchors) == 0 {
		return errors.New("trust: store is empty (no anchors synced yet)")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         snap.pool,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return fmt.Errorf("trust: chain verification failed: %w", err)
	}
	if err := checkRevocation(leaf, snap.crls); err != nil {
		return err
	}
	if s.policy != nil {
		if err := s.policy(leaf); err != nil {
			return fmt.Errorf("trust: policy rejected peer: %w", err)
		}
	}
	return nil
}

// VerifyPeer satisfies mtls.TrustProvider. It runs the TLS-supplied verified
// chains (if any) through the store's policy and revocation checks, falling
// back to building a chain from the raw certificates when the stack did not.
func (s *Store) VerifyPeer(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if len(verifiedChains) > 0 {
		leaf := verifiedChains[0][0]
		snap := s.snap.Load()
		if err := checkRevocation(leaf, snap.crls); err != nil {
			return err
		}
		if s.policy != nil {
			if err := s.policy(leaf); err != nil {
				return fmt.Errorf("trust: policy rejected peer: %w", err)
			}
		}
		return nil
	}
	if len(rawCerts) == 0 {
		return errors.New("trust: no peer certificate presented")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("trust: parse peer certificate: %w", err)
	}
	intermediates := x509.NewCertPool()
	for _, raw := range rawCerts[1:] {
		if c, err := x509.ParseCertificate(raw); err == nil {
			intermediates.AddCert(c)
		}
	}
	return s.Verify(leaf, intermediates)
}

// checkRevocation rejects leaf if any CRL issued by a CA whose name matches the
// leaf's issuer lists the leaf's serial number.
func checkRevocation(leaf *x509.Certificate, crls []*x509.RevocationList) error {
	for _, crl := range crls {
		if crl.Issuer.String() != leaf.Issuer.String() {
			continue
		}
		for i := range crl.RevokedCertificateEntries {
			if crl.RevokedCertificateEntries[i].SerialNumber.Cmp(leaf.SerialNumber) == 0 {
				return fmt.Errorf("trust: certificate %s is revoked", leaf.SerialNumber)
			}
		}
	}
	return nil
}
