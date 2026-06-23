// Package trust provides a hot-swappable certificate trust store: a set of
// trusted CA anchors plus optional revocation lists, against which peer
// certificates are validated offline. The current snapshot can be replaced
// atomically (e.g. after syncing an updated trust list) without locking out
// in-flight verifications.
//
// A Store structurally satisfies dx-common-go/mtls.TrustProvider, so it can be
// dropped straight into an mTLS server or client config and have its trust
// decisions take effect live.
//
// It is generic PKI plumbing — chain building, expiry, and CRL checks only. Any
// higher-level policy (which countries/nodes are admissible, how the trust list
// is distributed) belongs in the calling service, which can layer an extra
// PolicyFunc on top.
package trust
