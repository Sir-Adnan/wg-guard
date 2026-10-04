package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	data, err := readKeyFile(keyFile)
	defer clear(data)
	if errors.Is(err, os.ErrNotExist) {
		if _, previousErr := os.Lstat(keyFile + KeyFileSuffixPrev); !errors.Is(previousErr, os.ErrNotExist) {
			return nil, fmt.Errorf("secrets: current key is missing during a retained rotation; recover the matching key pair")
		}
		if !create {
			return nil, fmt.Errorf("secrets: existing encrypted node data requires its original master key")
		}
		key := make([]byte, 32)
		defer clear(key)
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

	prevData, err := readKeyFile(keyFile + KeyFileSuffixPrev)
	defer clear(prevData)
	if err == nil {
		prev, err := NewCipher(prevData)
		if err != nil {
			return nil, fmt.Errorf("secrets: invalid retained rotation key")
		}
		ring.prev = prev
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
	var samples [][]byte
	defer func() {
		for _, sample := range samples {
			clear(sample)
		}
	}()
	if err := WalkStoredSecrets(ctx, db, "", true, func(value []byte) error {
		samples = append(samples, bytes.Clone(value))
		return nil
	}); err != nil {
		return nil, fmt.Errorf("secrets: inspect encrypted node data: %w", err)
	}
	ring, err := loadKeyRing(keyFile, len(samples) == 0)
	if err != nil {
		return nil, err
	}
	for _, item := range samples {
		plaintext, err := ring.Decrypt(item)
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
// carrier to re-encrypt in bounded passes. A carrier must skip already-current
// values when resuming an interrupted dual-key window.
type Carrier interface {
	ReencryptSecrets(from, to *Cipher) error
}

// Rotate generates a new master key, swaps the key files (crash-safe window
// with the previous key retained), re-encrypts every carrier, and removes the
// previous key file on success.
func Rotate(keyFile string, carriers ...Carrier) (*KeyRing, error) {
	oldData, err := readKeyFile(keyFile)
	defer clear(oldData)
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
	previous, err := readKeyFile(keyFile + KeyFileSuffixPrev)
	defer clear(previous)
	newCipher := oldCipher
	if err == nil {
		// Complete the existing window. Never replace its predecessor while
		// untouched rows may still depend on it.
		oldCipher, err = NewCipher(previous)
		if err != nil {
			return nil, fmt.Errorf("secrets: invalid retained rotation key")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		newKey := make([]byte, 32)
		defer clear(newKey)
		if _, err := rand.Read(newKey); err != nil {
			return nil, fmt.Errorf("secrets: rotate: generate: %w", err)
		}
		newCipher, err = NewCipher(newKey)
		if err != nil {
			return nil, err
		}
		// Publish the previous key durably before replacing the current key.
		if err := writeKeyFile(keyFile+KeyFileSuffixPrev, oldData); err != nil {
			return nil, err
		}
		if err := writeKeyFile(keyFile, newKey); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("secrets: read retained rotation key: %w", err)
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
	if err := syncKeyDirectory(filepath.Dir(keyFile)); err != nil {
		return nil, err
	}
	return &KeyRing{current: newCipher}, nil
}

func writeKeyFile(path string, key []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// Best-effort hardening; may be a no-op on some filesystems.
		_ = os.MkdirAll(dir, 0o700)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".wg-guard-key-*")
	if err != nil {
		return fmt.Errorf("secrets: write key %s: %w", path, err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(key); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("secrets: swap key %s: %w", path, err)
	}
	return syncKeyDirectory(filepath.Dir(path))
}

func readKeyFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("secrets: key is not a regular file")
	}
	return io.ReadAll(io.LimitReader(f, 33))
}

func syncKeyDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil // Windows does not support fsync on directory handles.
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
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
