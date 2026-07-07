package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

// TrustProvider supplies the dynamic trust material for an mTLS server. A
// trust-store implementation backs this; mtls only wires it into crypto/tls.
type TrustProvider interface {
	// ClientCAs returns the current pool of CAs whose certificates are accepted
	// for client (peer) authentication. Called on every inbound handshake, so it
	// must return the latest snapshot cheaply.
	ClientCAs() *x509.CertPool

	// VerifyPeer is the final authority on a peer certificate after the TLS
	// stack has built a chain. Returning a non-nil error aborts the handshake.
	// It receives the raw certificates and any verified chains, exactly like
	// tls.Config.VerifyPeerCertificate.
	VerifyPeer(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error
}

// ServerConfig returns a *tls.Config that presents serverCert, requires a
// client certificate, and defers all trust decisions to tp on every handshake.
// The returned config can be reused across the process lifetime; trust changes
// take effect immediately because the pool and verify hook are read live.
func ServerConfig(serverCert tls.Certificate, tp TrustProvider) (*tls.Config, error) {
	if tp == nil {
		return nil, errors.New("mtls: trust provider is required")
	}
	base := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	base.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		c := base.Clone()
		c.ClientCAs = tp.ClientCAs()
		c.VerifyPeerCertificate = tp.VerifyPeer
		return c, nil
	}
	return base, nil
}

// ClientConfig returns a *tls.Config for the calling (outbound) side of an mTLS
// connection: it presents clientCert and validates the server against tp. When
// serverName is non-empty it is used for SNI and hostname verification;
// otherwise hostname verification is skipped and tp.VerifyPeer is solely
// responsible for authenticating the server (common in node-to-node trust
// fabrics keyed on certificate identity rather than DNS).
func ClientConfig(clientCert tls.Certificate, tp TrustProvider, serverName string) (*tls.Config, error) {
	if tp == nil {
		return nil, errors.New("mtls: trust provider is required")
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      tp.ClientCAs(),
		ServerName:   serverName,
	}
	if serverName == "" {
		cfg.InsecureSkipVerify = true // hostname check off; VerifyPeer authenticates instead
		cfg.VerifyPeerCertificate = tp.VerifyPeer
	}
	return cfg, nil
}
