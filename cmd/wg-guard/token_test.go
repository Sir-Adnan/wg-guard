package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/token"
)

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func testTokenConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Complete()
	path := filepath.Join(dir, "wg-guard.toml")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func openTokenDB(t *testing.T, cfgPath string) *database.DB {
	t.Helper()
	db, closeDB, err := openForToken(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeDB)
	return db
}

func TestTokenCLIRoundTrip(t *testing.T) {
	cfgPath := testTokenConfig(t)
	db := openTokenDB(t, cfgPath) // runs migrations, proves fresh-node operation

	// Create: plaintext printed exactly once to stdout.
	out := captureStdout(t, func() {
		if err := runToken([]string{"create", "-config", cfgPath, "-name", "ci",
			"-scopes", "users.read,users.bulk", "-expires-in", "48h"}); err != nil {
			t.Fatalf("token create: %v", err)
		}
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	plaintext := lines[len(lines)-1]
	if !strings.HasPrefix(plaintext, token.Prefix) || len(plaintext) != len(token.Prefix)+43 {
		t.Fatalf("stdout does not end with a minted token: %q", out)
	}
	// The plaintext verifies against the service.
	if _, err := token.NewService(db).Verify(t.Context(), plaintext, "127.0.0.1"); err != nil {
		t.Fatalf("minted token does not verify: %v", err)
	}

	tokens, err := token.NewService(db).List(t.Context())
	if err != nil || len(tokens) != 1 {
		t.Fatalf("list after create: %d %v", len(tokens), err)
	}
	if tokens[0].Name != "ci" || tokens[0].ExpiresAt == nil {
		t.Fatalf("stored token shape: %+v", tokens[0])
	}
	id := tokens[0].ID

	// Create without scopes must be refused (least privilege).
	if err := runToken([]string{"create", "-config", cfgPath, "-name", "wide"}); err == nil {
		t.Fatal("token create without -scopes must fail")
	}

	// Revoke: the token no longer verifies.
	if err := runToken([]string{"revoke", "-config", cfgPath, id}); err != nil {
		t.Fatalf("token revoke: %v", err)
	}
	if _, err := token.NewService(db).Verify(t.Context(), plaintext, "127.0.0.1"); err == nil {
		t.Fatal("revoked token still verifies")
	}

	// Unknown id is a clean error, not a crash.
	if err := runToken([]string{"revoke", "-config", cfgPath, "nope"}); err == nil {
		t.Fatal("revoking an unknown id must fail")
	}
}
