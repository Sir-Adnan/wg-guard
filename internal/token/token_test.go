package token

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return NewService(db)
}

func TestCreateAndVerify(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	tok, plaintext, err := svc.Create(ctx, "billing bot", []string{"users.read", "devices.*"}, nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(tok.Prefix) != len(Prefix)+lookupLen {
		t.Fatalf("prefix length %d", len(tok.Prefix))
	}
	if tok.Prefix != plaintext[:len(Prefix)+lookupLen] {
		t.Fatal("prefix not derived from plaintext")
	}
	v, err := svc.Verify(ctx, plaintext, "10.0.0.5")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !v.Authorize("users.read") {
		t.Fatal("users.read must be authorized")
	}
	if !v.Authorize("devices.write") {
		t.Fatal("devices.* must authorize devices.write")
	}
	if v.Authorize("node.settings") {
		t.Fatal("ungranted scope authorized")
	}
}

func TestVerifyRejectsForgedAndRevoked(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	_, plaintext, err := svc.Create(ctx, "t", []string{"stats.read"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Forge a token guaranteed to differ from the original: overwriting the
	// last character with a fixed symbol reproduces the original 1 time in 16
	// (the final base64url character of a 32-byte token carries only 4 bits of
	// entropy), which would make this test flaky.
	forged := []byte(plaintext)
	if forged[len(forged)-1] == 'A' {
		forged[len(forged)-1] = 'B'
	} else {
		forged[len(forged)-1] = 'A'
	}
	if _, err := svc.Verify(ctx, string(forged), ""); domain.CodeOf(err) != domain.CodeTokenInvalid {
		t.Fatal("forged token accepted")
	}
	// A well-formed token that was never issued must not authenticate either
	// (the sk- shape above is rejected as malformed before the lookup).
	if _, err := svc.Verify(ctx, "wg_"+strings.Repeat("a", 43), ""); domain.CodeOf(err) != domain.CodeTokenInvalid {
		t.Fatal("unknown token accepted")
	}
	// Revoke then verify.
	toks, _ := svc.List(ctx)
	if err := svc.Revoke(ctx, toks[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, plaintext, ""); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("revoked token must be FORBIDDEN, got %v", err)
	}
}

func TestExpiryEnforced(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).UTC()
	_, plaintext, err := svc.Create(ctx, "old", []string{"stats.read"}, &past, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, plaintext, ""); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("expired token must be FORBIDDEN, got %v", err)
	}
}

func TestCIDRAllowlist(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	_, plaintext, err := svc.Create(ctx, "restricted", []string{"stats.read"}, nil, "10.8.0.0/16, 192.168.1.4/32")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, plaintext, "10.8.3.9"); err != nil {
		t.Fatalf("in-range IP rejected: %v", err)
	}
	if _, err := svc.Verify(ctx, plaintext, "192.168.1.4"); err != nil {
		t.Fatalf("host CIDR rejected: %v", err)
	}
	if _, err := svc.Verify(ctx, plaintext, "8.8.8.8"); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("out-of-range IP must be FORBIDDEN, got %v", err)
	}
	if err := ValidateCIDRList("10.0.0.0/8, bogus"); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
	if _, _, err := svc.Create(ctx, "bad", []string{"stats.read"}, nil, "not-a-cidr"); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("create with bad CIDR must fail: %v", err)
	}
}

func TestScopeValidationAtCreate(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if _, _, err := svc.Create(ctx, "bad", []string{"users.readr"}, nil, ""); err == nil {
		t.Fatal("unknown scope accepted at create")
	}
	if _, _, err := svc.Create(ctx, "bad", []string{"*"}, nil, ""); err == nil {
		t.Fatal("global wildcard accepted at create")
	}
}
