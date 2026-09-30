package secrets_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

func TestNodeKeyRingWithStoredWebhookSecret(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := database.Open(filepath.Join(dir, "node.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "master.key")
	ring, err := secrets.LoadNodeKeyRing(ctx, db.DB, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "synthetic-webhook-secret"
	hooks := webhook.NewService(db, ring)
	endpoint, _, err := hooks.Create(ctx, "https://hooks.example/wg", []string{webhook.EventUserCreated}, secret)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := secrets.LoadNodeKeyRing(ctx, db.DB, keyFile)
	if err != nil {
		t.Fatalf("valid webhook blocks node key loading: %v", err)
	}
	if got, err := webhook.NewService(db, reloaded).Secret(ctx, endpoint); err != nil || got != secret {
		t.Fatal("webhook secret not usable after node reload")
	}
	after, err := os.ReadFile(keyFile)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("loading webhook data changed the master key")
	}
	// Missing/wrong keys and malformed stored text must still fail closed.
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.LoadNodeKeyRing(ctx, db.DB, keyFile); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("missing key accepted or secret leaked")
	}
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatal("missing master key was recreated")
	}
	wrong := bytes.Repeat([]byte{0x7f}, 32)
	if bytes.Equal(wrong, before) {
		t.Fatal("wrong-key fixture equals original")
	}
	if err := os.WriteFile(keyFile, wrong, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.LoadNodeKeyRing(ctx, db.DB, keyFile); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("wrong master key accepted or secret leaked")
	}
	if err := os.WriteFile(keyFile, before, 0600); err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{secret, "enc:invalid-base64", "enc:"} {
		if _, err := db.ExecContext(ctx, `UPDATE webhook_endpoints SET secret_encrypted = ? WHERE id = ?`, malformed, endpoint.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := secrets.LoadNodeKeyRing(ctx, db.DB, keyFile); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("malformed webhook secret accepted or leaked")
		}
	}
}
