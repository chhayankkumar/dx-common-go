// Package mtls builds mutual-TLS configurations whose trust decisions are
// delegated to a pluggable TrustProvider rather than baked into a static CA
// bundle. This lets a server hot-swap its trust anchors at runtime (e.g. after
// syncing an updated trust list) without restarting or dropping its listener.
//
// The server config uses GetConfigForClient so every inbound handshake reads
// the provider's *current* snapshot, and VerifyPeerCertificate so the provider
// gets the final say on each peer certificate (membership, revocation, policy)
// beyond ordinary chain building.
//
// It is generic transport plumbing: no business logic, no opinion about what
// makes a peer trustworthy — that lives entirely behind the TrustProvider.
package mtls
