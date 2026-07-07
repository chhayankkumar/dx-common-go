package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/hkdf"
)

const (
	algSign = "ES256"
	encAlg  = "A256GCM"
	kdfInfo = "dx-envelope/v1 A256GCM"
)

// sealed is the inner, encrypted structure (opaque to anyone without the
// recipient private key).
type sealed struct {
	Enc string `json:"enc"` // content encryption alg, e.g. A256GCM
	EPK string `json:"epk"` // ephemeral public key, base64url(0x04||X||Y)
	IV  string `json:"iv"`  // base64url nonce
	CT  string `json:"ct"`  // base64url ciphertext||tag
	RID string `json:"rid"` // recipient key id (for routing key selection)
}

// outer is the signed envelope: a signature over the serialised inner payload.
type outer struct {
	Alg     string `json:"alg"`     // signature alg, ES256
	SID     string `json:"sid"`     // signer key id
	Payload string `json:"payload"` // base64url(JSON(sealed))
	Sig     string `json:"sig"`     // base64url(R||S), 64 bytes
}

var b64 = base64.RawURLEncoding

// Seal encrypts plaintext to recipientPub (ECDH-ES + AES-256-GCM) and signs the
// ciphertext with signerPriv (ES256). The returned string is a compact,
// self-describing token safe to carry over an untrusted transport.
func Seal(plaintext []byte, recipientPub *ecdsa.PublicKey, signerPriv *ecdsa.PrivateKey) (string, error) {
	if recipientPub == nil || signerPriv == nil {
		return "", errors.New("envelope: recipient public key and signer private key are required")
	}

	recipientECDH, err := recipientPub.ECDH()
	if err != nil {
		return "", fmt.Errorf("envelope: recipient key not ECDH-usable: %w", err)
	}

	// Ephemeral-static ECDH.
	ephemeral, err := GenerateKey()
	if err != nil {
		return "", fmt.Errorf("envelope: generate ephemeral key: %w", err)
	}
	ephemeralECDH, err := ephemeral.ECDH()
	if err != nil {
		return "", fmt.Errorf("envelope: ephemeral key not ECDH-usable: %w", err)
	}
	shared, err := ephemeralECDH.ECDH(recipientECDH)
	if err != nil {
		return "", fmt.Errorf("envelope: ECDH: %w", err)
	}

	key, err := deriveKey(shared)
	if err != nil {
		return "", err
	}

	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("envelope: nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)

	inner := sealed{
		Enc: encAlg,
		EPK: b64.EncodeToString(ephemeralECDH.PublicKey().Bytes()),
		IV:  b64.EncodeToString(nonce),
		CT:  b64.EncodeToString(ct),
		RID: KeyID(recipientPub),
	}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return "", fmt.Errorf("envelope: marshal inner: %w", err)
	}
	payload := b64.EncodeToString(innerJSON)

	sig, err := signES256(signerPriv, []byte(payload))
	if err != nil {
		return "", err
	}

	env := outer{
		Alg:     algSign,
		SID:     KeyID(&signerPriv.PublicKey),
		Payload: payload,
		Sig:     b64.EncodeToString(sig),
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("envelope: marshal outer: %w", err)
	}
	return b64.EncodeToString(envJSON), nil
}

// Open verifies the envelope signature against signerPub (rejecting on any
// mismatch) and only then decrypts the payload with recipientPriv. This
// verify-then-decrypt order means a tampered or forged ciphertext never reaches
// the AES-GCM open step.
func Open(token string, recipientPriv *ecdsa.PrivateKey, signerPub *ecdsa.PublicKey) ([]byte, error) {
	if recipientPriv == nil || signerPub == nil {
		return nil, errors.New("envelope: recipient private key and signer public key are required")
	}

	envJSON, err := b64.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("envelope: decode token: %w", err)
	}
	var env outer
	if err := json.Unmarshal(envJSON, &env); err != nil {
		return nil, fmt.Errorf("envelope: unmarshal outer: %w", err)
	}
	if env.Alg != algSign {
		return nil, fmt.Errorf("envelope: unsupported signature alg %q", env.Alg)
	}

	sig, err := b64.DecodeString(env.Sig)
	if err != nil {
		return nil, fmt.Errorf("envelope: decode sig: %w", err)
	}
	if !verifyES256(signerPub, []byte(env.Payload), sig) {
		return nil, errors.New("envelope: signature verification failed")
	}

	innerJSON, err := b64.DecodeString(env.Payload)
	if err != nil {
		return nil, fmt.Errorf("envelope: decode payload: %w", err)
	}
	var inner sealed
	if err := json.Unmarshal(innerJSON, &inner); err != nil {
		return nil, fmt.Errorf("envelope: unmarshal inner: %w", err)
	}
	if inner.Enc != encAlg {
		return nil, fmt.Errorf("envelope: unsupported content alg %q", inner.Enc)
	}

	recipientECDH, err := recipientPriv.ECDH()
	if err != nil {
		return nil, fmt.Errorf("envelope: recipient key not ECDH-usable: %w", err)
	}
	epkBytes, err := b64.DecodeString(inner.EPK)
	if err != nil {
		return nil, fmt.Errorf("envelope: decode epk: %w", err)
	}
	epk, err := recipientECDH.Curve().NewPublicKey(epkBytes)
	if err != nil {
		return nil, fmt.Errorf("envelope: parse epk: %w", err)
	}
	shared, err := recipientECDH.ECDH(epk)
	if err != nil {
		return nil, fmt.Errorf("envelope: ECDH: %w", err)
	}

	key, err := deriveKey(shared)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, err := b64.DecodeString(inner.IV)
	if err != nil {
		return nil, fmt.Errorf("envelope: decode iv: %w", err)
	}
	ct, err := b64.DecodeString(inner.CT)
	if err != nil {
		return nil, fmt.Errorf("envelope: decode ct: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("envelope: decrypt: %w", err)
	}
	return plaintext, nil
}

func deriveKey(shared []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, shared, nil, []byte(kdfInfo))
	key := make([]byte, 32) // AES-256
	if _, err := r.Read(key); err != nil {
		return nil, fmt.Errorf("envelope: hkdf: %w", err)
	}
	return key, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("envelope: aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("envelope: gcm: %w", err)
	}
	return gcm, nil
}

// signES256 produces a fixed-width 64-byte R||S signature (JOSE convention).
func signES256(priv *ecdsa.PrivateKey, msg []byte) ([]byte, error) {
	h := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, priv, h[:])
	if err != nil {
		return nil, fmt.Errorf("envelope: sign: %w", err)
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out, nil
}

func verifyES256(pub *ecdsa.PublicKey, msg, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	h := sha256.Sum256(msg)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	return ecdsa.Verify(pub, h[:], r, s)
}
