// Package dpop validates RFC 9449 DPoP proofs: the short-lived, per-request
// JWS a client sends in the "DPoP" header to demonstrate possession of the
// private key bound to its access token.
//
// This package only validates the proof itself (signature, htm/htu, freshness,
// replay). It does not fetch or validate the access token — callers combine it
// with an access-token validator (e.g. dx-common-go/auth/jwt) and bind the two
// via CheckBinding (against the access token's "cnf.jkt" claim) and optionally
// CheckAccessTokenHash (against the proof's "ath" claim).
package dpop
