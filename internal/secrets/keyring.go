package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// KeyRing holds the current master key and, during a rotation window, the
// previous one. Encryption always uses the current key; decryption tries
// current then previous, which makes rotation crash-safe: the key files are
// swapped *before* rows are re-encrypted, so at any instant every stored
// envelope is decryptable (docs/operations/security.md).
type KeyRing struct {
	current *Cipher
	prev    *Cipher // nil when no rotation is in flight
}

// KeyFileSuffixPrev is the file the old key occupies during rotation.
const KeyFileSuffixPrev = ".prev"

// LoadKeyRing reads (or creates) the master key file. The file holds exactly
// 32 bytes, 0600, inside a 0700 directory (best-effort on filesystems without
// permission bits).
func LoadKeyRing(keyFile string) (*KeyRing, error) {
	return loadKeyRing(keyFile, true)
}

func loadKeyRing(keyFile string, create bool) (*KeyRing, error) {
	if keyFile == "" {
		return nil, fmt.Errorf("secrets: empty key file path")
	}
	data, err := os.ReadFile(keyFile)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, fmt.Errorf("secrets: existing encrypted node data requires its original master key")
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("secrets: generate key: %w", err)
		}
		if err := writeKeyFile(keyFile, key); err != nil {
			return nil, err
		}
		c, err := NewCipher(key)
		if err != nil {
			return nil, err
		}
		return &KeyRing{current: c}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secrets: read %s: %w", keyFile, err)
	}
	cur, err := NewCipher(data)
	if err != nil {
		return nil, fmt.Errorf("secrets: %s: %w", keyFile, err)
	}
	ring := &KeyRing{current: cur}

	prevData, err := os.ReadFile(keyFile + KeyFileSuffixPrev)
	if err == nil {
		if prev, err := NewCipher(prevData); err == nil {
			ring.prev = prev
		}
		// An unreadable .prev is non-fatal: it only mattered for rows that
		// should already have been re-encrypted before the swap.
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secrets: read %s%s: %w", keyFile, KeyFileSuffixPrev, err)
	}
	if err := ring.selfTest(); err != nil {
		return nil, err
	}
	return ring, nil
}

// LoadNodeKeyRing permits first-boot key creation only when the migrated
// database contains no encrypted carriers. Existing carriers must decrypt
// under the on-disk key (or its rotation-window predecessor) before the node
// or an offline data command can proceed.
func LoadNodeKeyRing(ctx context.Context, db *sql.DB, keyFile string) (*KeyRing, error) {
	queries := []struct {
		statement string
		text      bool
	}{
		{"SELECT private_key_encrypted FROM tunnel_interfaces LIMIT 1", false},
		{"SELECT private_key_encrypted FROM devices LIMIT 1", false},
		{"SELECT token_encrypted FROM sub_links LIMIT 1", false},
		// Webhook writers use EncryptString (enc: + base64), even though
		// SQLite's original column declaration is BLOB. Decode the envelope
		// just as the webhook delivery reader does before checking the key.
		{"SELECT secret_encrypted FROM webhook_endpoints LIMIT 1", true},
		// Keep in step with the secret definitions in internal/settings.
		{"SELECT value FROM settings WHERE key IN ('backup.password', 'backup.telegram_token') LIMIT 1", true},
	}
	type sample struct {
		value []byte
		text  bool
	}
	var samples []sample
	for _, query := range queries {
		var value []byte
		err := db.QueryRowContext(ctx, query.statement).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("secrets: inspect encrypted node data: %w", err)
		}
		samples = append(samples, sample{value: value, text: query.text})
	}
	ring, err := loadKeyRing(keyFile, len(samples) == 0)
	if err != nil {
		return nil, err
	}
	for _, item := range samples {
		ciphertext := item.value
		if item.text {
			if !bytes.HasPrefix(ciphertext, []byte("enc:")) {
				return nil, fmt.Errorf("secrets: stored encrypted text secret is invalid")
			}
			ciphertext, err = base64.StdEncoding.DecodeString(string(ciphertext[4:]))
			if err != nil {
				return nil, fmt.Errorf("secrets: stored encrypted text secret is invalid")
			}
		}
		plaintext, err := ring.Decrypt(ciphertext)
		clear(plaintext)
		if err != nil {
			return nil, fmt.Errorf("secrets: master key does not decrypt existing node data; restore the matching key or archive")
		}
	}
	return ring, nil
}

// Current exposes the active cipher (Encrypt path). Decryption goes through
// the KeyRing so previous-key envelopes still open during rotation.
func (k *KeyRing) Encrypt(plaintext []byte) ([]byte, error) { return k.current.Encrypt(plaintext) }

func (k *KeyRing) EncryptString(s string) (string, error) { return k.current.EncryptString(s) }

func (k *KeyRing) Decrypt(data []byte) ([]byte, error) {
	pt, err := k.current.Decrypt(data)
	if err == nil {
		return pt, nil
	}
	if k.prev != nil {
		return k.prev.Decrypt(data)
	}
	return nil, err
}

func (k *KeyRing) DecryptString(s string) (string, error) {
	pt, err := k.current.DecryptString(s)
	if err == nil {
		return pt, nil
	}
	if k.prev != nil {
		return k.prev.DecryptString(s)
	}
	return "", err
}

// Carrier is one storage area that holds encrypted values. Rotation asks each
// carrier to re-encrypt in place (its own transaction).
type Carrier interface {
	ReencryptSecrets(from, to *Cipher) error
}

// Rotate generates a new master key, swaps the key files (crash-safe window
// with the previous key retained), re-encrypts every carrier, and removes the
// previous key file on success.
func Rotate(keyFile string, carriers ...Carrier) (*KeyRing, error) {
	oldData, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("secrets: rotate: read current key: %w", err)
	}
	if len(oldData) != 32 {
		return nil, fmt.Errorf("secrets: rotate: %s is not a 32-byte key", keyFile)
	}
	oldCipher, err := NewCipher(oldData)
	if err != nil {
		return nil, err
	}
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return nil, fmt.Errorf("secrets: rotate: generate: %w", err)
	}
	newCipher, err := NewCipher(newKey)
	if err != nil {
		return nil, err
	}

	// 1. Swap key files first (old -> .prev, new -> current) so every stored
	//    envelope stays decryptable no matter where we are interrupted.
	if err := writeKeyFile(keyFile+KeyFileSuffixPrev, oldData); err != nil {
		return nil, err
	}
	if err := writeKeyFile(keyFile, newKey); err != nil {
		return nil, err
	}

	// 2. Re-encrypt all carriers. A failure aborts rotation; the previous key
	//    file remains so the ring still decrypts untouched rows.
	for _, c := range carriers {
		if err := c.ReencryptSecrets(oldCipher, newCipher); err != nil {
			return nil, fmt.Errorf("secrets: rotate: re-encrypt %T: %w", c, err)
		}
	}

	// 3. Success: drop the previous key.
	if err := os.Remove(keyFile + KeyFileSuffixPrev); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secrets: rotate: remove previous key: %w", err)
	}
	return &KeyRing{current: newCipher}, nil
}

func writeKeyFile(path string, key []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// Best-effort hardening; may be a no-op on some filesystems.
		_ = os.MkdirAll(dir, 0o700)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, key, 0o600); err != nil {
		return fmt.Errorf("secrets: write key %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secrets: swap key %s: %w", path, err)
	}
	return nil
}

// tamperProbe guards tests and doctor: verifies the ring can decrypt what it
// just encrypted (catches a corrupt key file early, at boot).
func (k *KeyRing) selfTest() error {
	probe := []byte("wg-guard-keyring-probe")
	ct, err := k.Encrypt(probe)
	if err != nil {
		return err
	}
	pt, err := k.Decrypt(ct)
	if err != nil || !bytes.Equal(pt, probe) {
		return fmt.Errorf("secrets: keyring self-test failed: %w", err)
	}
	return nil
}

// SelfTest verifies the key ring round-trips (boot/doctor diagnostics).
func (k *KeyRing) SelfTest() error { return k.selfTest() }
