package secrets

import (
	"bytes"
	"crypto/rand"
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
	if keyFile == "" {
		return nil, fmt.Errorf("secrets: empty key file path")
	}
	data, err := os.ReadFile(keyFile)
	if errors.Is(err, os.ErrNotExist) {
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
