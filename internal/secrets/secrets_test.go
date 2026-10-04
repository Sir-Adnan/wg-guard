package secrets

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("device private key material")
	ct, err := c.Encrypt(msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, msg) {
		t.Fatal("ciphertext leaks plaintext")
	}
	pt, err := c.Decrypt(ct)
	if err != nil || !bytes.Equal(pt, msg) {
		t.Fatalf("round trip: %v %q", err, pt)
	}

	// Fresh nonce per call.
	ct2, _ := c.Encrypt(msg)
	if bytes.Equal(ct, ct2) {
		t.Fatal("nonce reuse detected")
	}
}

func TestCipherRejectsTampering(t *testing.T) {
	c, _ := NewCipher(bytes.Repeat([]byte{7}, 32))
	other, _ := NewCipher(bytes.Repeat([]byte{8}, 32))
	ct, _ := c.Encrypt([]byte("secret"))

	if _, err := other.Decrypt(ct); !errors.Is(err, ErrTampered) {
		t.Fatalf("wrong key must fail with ErrTampered, got %v", err)
	}
	ct[len(ct)-1] ^= 1
	if _, err := c.Decrypt(ct); !errors.Is(err, ErrTampered) {
		t.Fatalf("bit flip must fail with ErrTampered, got %v", err)
	}
	if _, err := c.Decrypt([]byte("junk")); !errors.Is(err, ErrTampered) {
		t.Fatalf("junk must fail with ErrTampered, got %v", err)
	}
	if _, err := NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("short key must be rejected")
	}
}

func TestTextEnvelopeRoundTrip(t *testing.T) {
	c, _ := NewCipher(bytes.Repeat([]byte{9}, 32))
	enc, err := c.EncryptString("token-here")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncryptedText(enc) {
		t.Fatal("IsEncryptedText must recognize envelope")
	}
	got, err := c.DecryptString(enc)
	if err != nil || got != "token-here" {
		t.Fatalf("round trip: %v %q", err, got)
	}
	if IsEncryptedText("plain value") {
		t.Fatal("plain value misdetected")
	}
}

func TestLoadKeyRingCreatesFile(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "sub", "master.key")
	ring, err := LoadKeyRing(keyFile)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if runtime := info.Size(); runtime != 32 {
		t.Fatalf("key file size = %d, want 32", runtime)
	}
	if err := ring.SelfTest(); err != nil {
		t.Fatal(err)
	}

	// Reload must reuse the same key (values stay decryptable).
	ring2, err := LoadKeyRing(keyFile)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	ct, _ := ring.EncryptString("x")
	if _, err := ring2.DecryptString(ct); err != nil {
		t.Fatalf("reloaded ring cannot decrypt: %v", err)
	}
}

func TestNodeKeyRingRefusesMissingOrWrongKeyForEncryptedData(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "node.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "master.key")
	ring, err := LoadNodeKeyRing(ctx, db.DB, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ring.EncryptString("private-test-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)",
		"backup.password", value, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNodeKeyRing(ctx, db.DB, keyFile); err != nil {
		t.Fatalf("correct existing key rejected: %v", err)
	}
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNodeKeyRing(ctx, db.DB, keyFile); err == nil || strings.Contains(err.Error(), "private-test-value") {
		t.Fatalf("missing key was regenerated or exposed data: %v", err)
	}
	if _, err := os.Stat(keyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing key was recreated: %v", err)
	}
	wrong := bytes.Repeat([]byte{0x7f}, 32)
	if bytes.Equal(wrong, original) {
		t.Fatal("test replacement unexpectedly equals original key")
	}
	if err := os.WriteFile(keyFile, wrong, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNodeKeyRing(ctx, db.DB, keyFile); err == nil || strings.Contains(err.Error(), "private-test-value") {
		t.Fatalf("wrong key was accepted or exposed data: %v", err)
	}
}

func TestRotateCrashSafeOrder(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "master.key")
	ring, err := LoadKeyRing(keyFile)
	if err != nil {
		t.Fatal(err)
	}

	carrier := &mapCarrier{data: map[string]string{}}
	oldEnc, _ := ring.EncryptString("telegram-token")
	carrier.data["backup.telegram_token"] = oldEnc

	newRing, err := Rotate(keyFile, carrier)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if got, err := newRing.DecryptString(carrier.data["backup.telegram_token"]); err != nil || got != "telegram-token" {
		t.Fatalf("post-rotation decrypt: %v %q", err, got)
	}
	// New key file active, previous removed.
	if _, err := os.Stat(keyFile + KeyFileSuffixPrev); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".prev must be removed after successful rotation, got %v", err)
	}
	// Reload from disk (new key only) still decrypts everything.
	reloaded, err := LoadKeyRing(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.DecryptString(carrier.data["backup.telegram_token"]); err != nil {
		t.Fatalf("reloaded ring after rotation: %v", err)
	}
}

func TestRotateAbortsOnCarrierFailure(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "master.key")
	if _, err := LoadKeyRing(keyFile); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("carrier failed")
	failing := &failingCarrier{err: boom}
	if _, err := Rotate(keyFile, failing); !errors.Is(err, boom) {
		t.Fatalf("expected carrier error, got %v", err)
	}
	// Key files were swapped before re-encryption, so the on-disk ring is the
	// NEW key and .prev holds the old one — a ring reloaded from disk must
	// still decrypt old-key envelopes.
	reloaded, err := LoadKeyRing(keyFile)
	if err != nil {
		t.Fatalf("reload after failed rotation: %v", err)
	}
	oldText := "survives"
	// The important property: a value encrypted with the OLD key still
	// decrypts via .prev on the reloaded ring.
	prevData, err := os.ReadFile(keyFile + KeyFileSuffixPrev)
	if err != nil {
		t.Fatalf("prev key must exist after failed rotation: %v", err)
	}
	prevCipher, err := NewCipher(prevData)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := prevCipher.EncryptString(oldText)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reloaded.DecryptString(enc); err != nil || got != oldText {
		t.Fatalf("previous-key envelope must decrypt: %v %q", err, got)
	}
}

type mapCarrier struct{ data map[string]string }

func (m *mapCarrier) ReencryptSecrets(from, to *Cipher) error {
	for k, v := range m.data {
		if _, err := to.DecryptString(v); err == nil {
			continue
		}
		pt, err := from.DecryptString(v)
		if err != nil {
			return err
		}
		enc, err := to.EncryptString(pt)
		if err != nil {
			return err
		}
		m.data[k] = enc
	}
	return nil
}

func TestRotateResumesWithoutReplacingRetainedKeys(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "master.key")
	db, err := database.Open(filepath.Join(dir, "node.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ring, err := LoadKeyRing(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range SecretSettingKeys() {
		sealed, err := ring.EncryptString("synthetic-rotation-value")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO settings (key,value,updated_at) VALUES (?,?,'2026-01-01T00:00:00Z')`, key, sealed); err != nil {
			t.Fatal(err)
		}
	}
	boom := errors.New("synthetic interrupted rotation")
	// One field is changed, one remains on the predecessor key.
	partial := partialStoredCarrier{db: db}
	if _, err := Rotate(keyFile, partial, &failingCarrier{err: boom}); !errors.Is(err, boom) {
		t.Fatal("rotation failure was not preserved")
	}
	current, _ := os.ReadFile(keyFile)
	previous, _ := os.ReadFile(keyFile + KeyFileSuffixPrev)
	if bytes.Equal(current, previous) {
		t.Fatal("fixture did not enter a dual-key window")
	}
	if _, err := Rotate(keyFile, &failingCarrier{err: boom}); !errors.Is(err, boom) {
		t.Fatal("retry failure was not preserved")
	}
	currentAgain, _ := os.ReadFile(keyFile)
	previousAgain, _ := os.ReadFile(keyFile + KeyFileSuffixPrev)
	if !bytes.Equal(current, currentAgain) || !bytes.Equal(previous, previousAgain) {
		t.Fatal("retry overwrote a key still required by stored rows")
	}
	finished, err := Rotate(keyFile, NodeCarrier(ctx, db.DB))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyFile + KeyFileSuffixPrev); !os.IsNotExist(err) {
		t.Fatal("completed rotation retained its predecessor")
	}
	if err := WalkStoredSecrets(ctx, db.DB, "", false, func(value []byte) error {
		plaintext, err := finished.Decrypt(value)
		clear(plaintext)
		return err
	}); err != nil {
		t.Fatal("completed rotation requires an unretained key")
	}
}

type partialStoredCarrier struct{ db *database.DB }

func (p partialStoredCarrier) ReencryptSecrets(from, to *Cipher) error {
	var stored string
	if err := p.db.QueryRow(`SELECT value FROM settings WHERE key='backup.password'`).Scan(&stored); err != nil {
		return err
	}
	plaintext, err := from.DecryptString(stored)
	if err != nil {
		return err
	}
	sealed, err := to.EncryptString(plaintext)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(`UPDATE settings SET value=? WHERE key='backup.password'`, sealed)
	return err
}

type failingCarrier struct{ err error }

func (f *failingCarrier) ReencryptSecrets(from, to *Cipher) error { return f.err }
