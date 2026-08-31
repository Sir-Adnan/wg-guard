package subscription

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func newService(t *testing.T) (*Service, *user.Service) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "sub.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	usvc := user.NewService(db)
	return NewService(db, ring), usvc
}

// mkUser inserts a real user row (sub_links references users).
func mkUser(t *testing.T, usvc *user.Service, name string) string {
	t.Helper()
	u, err := usvc.Create(context.Background(), user.Input{Username: name})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestEnsureRegenerateResolve(t *testing.T) {
	svc, usvc := newService(t)
	ctx := context.Background()

	// Ensure is idempotent: two calls return the same token.
	uid := mkUser(t, usvc, "user1")
	l1, err := svc.Ensure(ctx, uid)
	if err != nil || l1 == nil || l1.Token == "" {
		t.Fatalf("ensure: %v %v", l1, err)
	}
	l2, err := svc.Ensure(ctx, uid)
	if err != nil || l2.Token != l1.Token {
		t.Fatalf("ensure not idempotent: %q vs %q (%v)", l1.Token, l2.Token, err)
	}
	if len(l1.Token) < 40 {
		t.Fatalf("token too short (want 256-bit): %q", l1.Token)
	}

	// Resolution works for the minted token.
	got, err := svc.Resolve(ctx, l1.Token)
	if err != nil || got != uid {
		t.Fatalf("resolve: %q %v", got, err)
	}

	// Regenerate invalidates the old token and returns a new one.
	l3, err := svc.Regenerate(ctx, uid)
	if err != nil || l3.Token == "" || l3.Token == l1.Token {
		t.Fatalf("regenerate: %v %v", l3, err)
	}
	if _, err := svc.Resolve(ctx, l1.Token); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("old token still resolves: %v", err)
	}
	if uid2, err := svc.Resolve(ctx, l3.Token); err != nil || uid2 != uid {
		t.Fatalf("new token does not resolve: %q %v", uid, err)
	}

	// Revoked links resolve to the same not-found error (no oracle).
	if _, err := svc.SetRevoked(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, l3.Token); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("revoked token resolves: %v", err)
	}
	// Restore re-enables the same URL.
	if _, err := svc.SetRevoked(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	if uid2, err := svc.Resolve(ctx, l3.Token); err != nil || uid2 != uid {
		t.Fatalf("restored token broken: %q %v", uid, err)
	}

	// Unknown tokens are not found.
	if _, err := svc.Resolve(ctx, "garbage"); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("garbage token: %v", err)
	}
}

func TestForUsers(t *testing.T) {
	svc, usvc := newService(t)
	ctx := context.Background()
	for _, name := range []string{"usera", "userb", "userc"} {
		if _, err := svc.Ensure(ctx, mkUser(t, usvc, name)); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{}
	rows, err := svc.db.QueryContext(ctx, `SELECT id FROM users WHERE username IN ('usera','userb')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	links, err := svc.ForUsers(ctx, append(ids, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("batch: %v", links)
	}
	for _, l := range links {
		if l.Token == "" {
			t.Fatal("batch lost decrypted tokens")
		}
	}
}

func TestTokensAreHashedAtRest(t *testing.T) {
	svc, usvc := newService(t)
	ctx := context.Background()
	uid := mkUser(t, usvc, "user1")
	l, err := svc.Ensure(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := svc.db.QueryRowContext(ctx, `SELECT token_hash FROM sub_links WHERE user_id = ?`, uid).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == l.Token || strings.Contains(stored, l.Token) {
		t.Fatal("plaintext token leaked into storage")
	}
	if stored != HashToken(l.Token) {
		t.Fatal("stored hash mismatch")
	}
}
