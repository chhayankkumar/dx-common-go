// Package envelope implements an encrypt-then-sign message envelope for
// confidential, authenticated payloads exchanged between two parties that do
// not trust the transport or any intermediary.
//
// The scheme is deliberately small and dependency-light (Go standard library
// plus golang.org/x/crypto/hkdf). It is suitable for sealing a payload to a
// single recipient whose long-term P-256 key is known, and signing it with the
// sender's long-term P-256 key so the recipient can authenticate the origin
// before decrypting (verify-then-decrypt).
//
//   - Confidentiality: ECDH-ES (ephemeral-static, P-256) → HKDF-SHA256 →
//     AES-256-GCM. Only the holder of the recipient private key can decrypt.
//   - Authenticity/Integrity: ECDSA P-256 (ES256) signature over the ciphertext,
//     verified before any decryption is attempted.
//
// All keys are standard *ecdsa.PrivateKey / *ecdsa.PublicKey on the P-256 curve
// (so they marshal to ordinary PKCS#8 / PKIX PEM). Key agreement uses the
// standard library's ecdsa→ecdh bridge.
//
// This is generic, reusable cryptographic plumbing — it contains no business
// logic and no SADx/DX-specific assumptions.
package envelope
