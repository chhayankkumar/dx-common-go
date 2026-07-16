package dpop

// Claims are the payload fields of a DPoP proof JWT, per RFC 9449 §4.2.
type Claims struct {
	// Htm is the HTTP method of the request to which the proof is attached.
	Htm string `json:"htm"`
	// Htu is the HTTP target URI, without query or fragment components.
	Htu string `json:"htu"`
	// Iat is when the proof was created (Unix seconds).
	Iat int64 `json:"iat"`
	// Jti is a unique identifier for this proof, used for replay detection.
	Jti string `json:"jti"`
	// Ath is base64url(SHA-256(access_token)), present when the proof is
	// presented alongside a DPoP-bound access token.
	Ath string `json:"ath,omitempty"`
}

// Proof is a validated DPoP proof: its claims plus the RFC 7638 thumbprint of
// the public key embedded in its "jwk" header (the "jkt" value).
type Proof struct {
	Claims Claims
	// JKT is the base64url-encoded SHA-256 thumbprint of the proof's embedded
	// public key. Compare against an access token's "cnf.jkt" claim to bind
	// the two together.
	JKT string
}
