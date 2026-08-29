package tunnel

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// KeyPair is a Curve25519 keypair in WireGuard's base64 encoding. Generation
// uses the standard library (crypto/ecdh): WireGuard/AWG keys *are* X25519
// keys — this is standard cryptography, not a reinvented scheme. The `awg
// genkey` CLI remains the pinned engine's own generator for parity checks in
// Phase 2; services only need correct key material.
type KeyPair struct {
	Private string // base64, 32 bytes
	Public  string // base64, 32 bytes
}

// GenerateKeyPair creates a fresh Curve25519 keypair.
func GenerateKeyPair() (KeyPair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("tunnel: generate keypair: %w", err)
	}
	return KeyPair{
		Private: base64.StdEncoding.EncodeToString(priv.Bytes()),
		Public:  base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()),
	}, nil
}

// GeneratePresharedKey returns 32 random bytes, base64 — exactly what
// `awg genpsk` produces.
func GeneratePresharedKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("tunnel: generate psk: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ValidatePublicKey checks base64 form and 32-byte length.
func ValidatePublicKey(b64 string) error {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("public key is not valid base64")
	}
	if len(raw) != 32 {
		return fmt.Errorf("public key must decode to 32 bytes, got %d", len(raw))
	}
	return nil
}

// ValidatePrivateKey checks base64 form and 32-byte length.
func ValidatePrivateKey(b64 string) error {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("private key is not valid base64")
	}
	if len(raw) != 32 {
		return fmt.Errorf("private key must decode to 32 bytes, got %d", len(raw))
	}
	return nil
}

// PublicKeyFromPrivate derives the public key for a base64 private key.
func PublicKeyFromPrivate(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("private key must be 32 base64 bytes")
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("tunnel: private key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}
