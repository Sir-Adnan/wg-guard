package auth

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "a.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestAuthzMatrix is the acceptance-critical authorization matrix: every
// (granted, required) combination that matters must resolve exactly as the
// contract says (docs/architecture/api.md scopes).
func TestAuthzMatrix(t *testing.T) {
	cases := []struct {
		granted  []string
		required string
		want     bool
		note     string
	}{
		{[]string{"users.read"}, "users.read", true, "exact match"},
		{[]string{"users.read"}, "users.write", false, "sibling scope does not imply"},
		{[]string{"users.write"}, "users.read", false, "write does not imply read"},
		{[]string{"devices.*"}, "devices.read", true, "family wildcard covers read"},
		{[]string{"devices.*"}, "devices.write", true, "family wildcard covers write"},
		{[]string{"devices.*"}, "users.read", false, "wildcard does not cross family"},
		{[]string{"users.read", "devices.write"}, "devices.write", true, "multi-grant hit"},
		{[]string{"node.settings"}, "node.read", false, "settings does not imply read"},
		{[]string{"interfaces.read"}, "interfaces.write", false, "read cannot mutate"},
		{[]string{"audit.view"}, "audit.view", true, "panel scope works"},
		{[]string{"backup.manage"}, "backup.manage", true, "backup scope works"},
		{nil, "stats.read", false, "empty grants deny"},
		{[]string{"traffic.update"}, "traffic.read", false, "update does not imply read"},
	}
	for _, tc := range cases {
		if got := Allows(tc.granted, tc.required); got != tc.want {
			t.Errorf("Allows(%v, %s) = %v, want %v (%s)", tc.granted, tc.required, got, tc.want, tc.note)
		}
	}
}

func TestOwnerBypassesScopes(t *testing.T) {
	if !Authorized(RoleOwner, nil, ScopeAdminsManage) {
		t.Fatal("owner must pass everything")
	}
	if Authorized(RoleAdmin, []string{"users.read"}, ScopeAdminsManage) {
		t.Fatal("admin without grant must fail")
	}
	if !Authorized(RoleAdmin, []string{"admins.manage"}, ScopeAdminsManage) {
		t.Fatal("admin with grant must pass")
	}
}

func TestValidateScopesRejectsUnknown(t *testing.T) {
	if err := ValidateScopes([]string{"users.read", "devices.*"}); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
	if err := ValidateScopes([]string{"users.readr"}); err == nil {
		t.Fatal("typo'd scope must be rejected")
	}
	if err := ValidateScopes([]string{"*"}); err == nil {
		t.Fatal("global wildcard must be rejected")
	}
	if err := ValidateScopes([]string{"nonexistent.*"}); err == nil {
		t.Fatal("unknown family must be rejected")
	}
	if len(AllScopes()) < 20 {
		t.Fatal("registry suspiciously small")
	}
}

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("correct horse battery", hash)
	if err != nil || !ok {
		t.Fatalf("correct password rejected: %v", err)
	}
	ok, _ = VerifyPassword("wrong password !!", hash)
	if ok {
		t.Fatal("wrong password accepted")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password must be rejected")
	}
	if _, err := VerifyPassword("x", "garbage"); err == nil {
		t.Fatal("malformed hash must error, not panic")
	}
	// Same password hashes differently (random salt).
	h2, _ := HashPassword("correct horse battery")
	if h2 == hash {
		t.Fatal("salt not random")
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewSessionStore(db, time.Hour, 24*time.Hour)

	// The session join needs an admin row.
	_, err := db.Exec(`INSERT INTO admins (id, username, password_hash, role, permissions, enabled, created_at, updated_at)
		VALUES ('a1', 'root', 'x', 'owner', '[]', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	token, exp, err := store.Create(ctx, "a1", "127.0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if exp.Before(time.Now()) {
		t.Fatal("expiry in the past")
	}
	admin, err := store.Validate(ctx, token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if admin.ID != "a1" || admin.Role != RoleOwner || !admin.Enabled {
		t.Fatalf("admin mismatch: %+v", admin)
	}
	if admin.Username != "root" {
		t.Fatalf("username not carried: %+v", admin)
	}

	if err := store.Revoke(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(ctx, token); err == nil {
		t.Fatal("revoked session validated")
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewSessionStore(db, time.Hour, 100*time.Hour)
	now := time.Now().UTC()
	store.now = func() time.Time { return now }

	_, _ = db.Exec(`INSERT INTO admins (id, username, password_hash, role, permissions, enabled, created_at, updated_at)
		VALUES ('a1', 'root', 'x', 'owner', '[]', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	token, _, _ := store.Create(ctx, "a1", "")

	now = now.Add(2 * time.Hour) // past idle TTL, before absolute cap
	if _, err := store.Validate(ctx, token); err == nil {
		t.Fatal("idle session must expire")
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM admin_sessions`).Scan(&n)
	if n != 0 {
		t.Fatal("expired session not pruned on validate")
	}
}

func TestSessionAbsoluteExpiry(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewSessionStore(db, time.Hour, time.Hour)
	now := time.Now().UTC()
	store.now = func() time.Time { return now }

	_, _ = db.Exec(`INSERT INTO admins (id, username, password_hash, role, permissions, enabled, created_at, updated_at)
		VALUES ('a1', 'root', 'x', 'owner', '[]', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	token, _, _ := store.Create(ctx, "a1", "")

	// Keep last_seen fresh (below idle TTL) but pass the absolute cap.
	now = now.Add(90 * time.Minute)
	if _, err := store.Validate(ctx, token); err == nil {
		t.Fatal("absolute cap must hold even with fresh activity")
	}
}

func TestDisabledAdminFailsClosed(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewSessionStore(db, time.Hour, 24*time.Hour)
	_, _ = db.Exec(`INSERT INTO admins (id, username, password_hash, role, permissions, enabled, created_at, updated_at)
		VALUES ('a1', 'root', 'x', 'owner', '[]', 0, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	token, _, _ := store.Create(ctx, "a1", "")
	if _, err := store.Validate(ctx, token); err == nil {
		t.Fatal("disabled admin session must fail")
	}
}

func TestSessionConcurrentValidate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewSessionStore(db, time.Hour, 24*time.Hour)
	_, _ = db.Exec(`INSERT INTO admins (id, username, password_hash, role, permissions, enabled, created_at, updated_at)
		VALUES ('a1', 'root', 'x', 'owner', '[]', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	token, _, _ := store.Create(ctx, "a1", "")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Validate(ctx, token); err != nil {
				t.Errorf("concurrent validate: %v", err)
			}
		}()
	}
	wg.Wait()
}
