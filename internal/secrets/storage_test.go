package secrets

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

func TestNodeCarrierRefusesForeignKeyAndOversizedValues(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "node.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	current, _ := NewCipher(bytes.Repeat([]byte{1}, 32))
	foreign, _ := NewCipher(bytes.Repeat([]byte{2}, 32))
	newKey, _ := NewCipher(bytes.Repeat([]byte{3}, 32))
	sealed, _ := current.EncryptString("synthetic-storage-value")
	if _, err := db.Exec(`INSERT INTO settings (key,value,updated_at) VALUES ('backup.password',?,'2026-01-01T00:00:00Z')`, sealed); err != nil {
		t.Fatal(err)
	}
	if err := NodeCarrier(ctx, db.DB).ReencryptSecrets(foreign, newKey); !errors.Is(err, ErrTampered) {
		t.Fatal("foreign rotation key did not fail closed")
	}
	var unchanged string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='backup.password'`).Scan(&unchanged); err != nil || unchanged != sealed {
		t.Fatal("failed rotation changed the stored envelope")
	}
	if _, err := db.Exec(`UPDATE settings SET value=? WHERE key='backup.password'`, bytes.Repeat([]byte{'x'}, MaxStoredEnvelopeBytes+1)); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []bool{false, true} {
		if err := WalkStoredSecrets(ctx, db.DB, "", sample, func([]byte) error { t.Fatal("oversized value reached decryption"); return nil }); !errors.Is(err, ErrStoredEnvelope) {
			t.Fatal("oversized storage was not rejected before allocation")
		}
	}
	if err := NodeCarrier(ctx, db.DB).ReencryptSecrets(current, newKey); !errors.Is(err, ErrStoredEnvelope) {
		t.Fatal("oversized rotation value was not bounded")
	}
}

func TestRetainedInvalidKeyCannotBeIgnoredOrOverwritten(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "master.key")
	if _, err := LoadKeyRing(keyFile); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(keyFile)
	invalid := []byte("synthetic-invalid-retained-key")
	if err := os.WriteFile(keyFile+KeyFileSuffixPrev, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyRing(keyFile); err == nil {
		t.Fatal("invalid predecessor was silently ignored")
	}
	if _, err := Rotate(keyFile, &mapCarrier{data: map[string]string{}}); err == nil {
		t.Fatal("invalid predecessor was overwritten on retry")
	}
	after, _ := os.ReadFile(keyFile)
	retained, _ := os.ReadFile(keyFile + KeyFileSuffixPrev)
	if !bytes.Equal(before, after) || !bytes.Equal(invalid, retained) {
		t.Fatal("invalid retained key handling changed original files")
	}
}

func TestMissingCurrentKeyCannotStartNewRetainedRotation(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyFile+KeyFileSuffixPrev, bytes.Repeat([]byte{7}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyRing(keyFile); err == nil {
		t.Fatal("missing current key created an unrelated rotation key")
	}
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatal("missing current key was silently recreated")
	}
}
