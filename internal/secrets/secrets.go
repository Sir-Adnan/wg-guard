// Package secrets provides the node-local secret-at-rest primitive: AES-256-GCM
// under a 32-byte master key stored outside the database (0600 file). Device
// private keys, preshared keys, webhook secrets, Telegram credentials, and the
// optional backup password are encrypted with it. Standard primitives only;
// no custom cryptography (docs/operations/security.md, ADR-0008).
//
// Root on the VPS can read the master key and the DB — this design raises the
// bar for lesser compromises (DB-file leak, backup leak), not root equivalence.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrTampered reports authentication failure (wrong key or corrupted data).
var ErrTampered = errors.New("secrets: authentication failed")

const envelopeVersion byte = 1

// Cipher encrypts/decrypts with one key. Envelope layout:
// [version u8][nonce 12B][ciphertext+tag].
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from 32 key bytes.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext. Output is self-describing (version byte).
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	out = append(out, envelopeVersion)
	out = append(out, nonce...)
	out = c.aead.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt opens an envelope; any tampering or wrong key returns ErrTampered.
func (c *Cipher) Decrypt(data []byte) ([]byte, error) {
	if len(data) < 1 || data[0] != envelopeVersion {
		return nil, ErrTampered
	}
	ns := c.aead.NonceSize()
	if len(data) < 1+ns+c.aead.Overhead() {
		return nil, ErrTampered
	}
	pt, err := c.aead.Open(nil, data[1:1+ns], data[1+ns:], nil)
	if err != nil {
		return nil, ErrTampered
	}
	return pt, nil
}

// EncryptString/DecryptString are base64-text conveniences for TEXT columns
// (settings values). Envelopes in BLOB columns use the byte forms directly.
func (c *Cipher) EncryptString(plaintext string) (string, error) {
	b, err := c.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return "enc:" + base64.StdEncoding.EncodeToString(b), nil
}

func (c *Cipher) DecryptString(s string) (string, error) {
	const prefix = "enc:"
	if len(s) <= len(prefix) || s[:len(prefix)] != prefix {
		return "", ErrTampered
	}
	raw, err := base64.StdEncoding.DecodeString(s[len(prefix):])
	if err != nil {
		return "", ErrTampered
	}
	pt, err := c.Decrypt(raw)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// IsEncryptedText reports whether a TEXT value carries an envelope (used by
// rotation sweeps to find rows needing re-encryption).
func IsEncryptedText(s string) bool {
	return len(s) > 4 && s[:4] == "enc:"
}
