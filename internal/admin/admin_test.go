package admin

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func TestConcurrentOwnerEntryPoints(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var err error
			if i%2 == 0 {
				_, err = svc.BootstrapOwner(ctx, fmt.Sprintf("owner%d", i), "synthetic-password-123")
			} else {
				_, err = svc.Create(ctx, fmt.Sprintf("owner%d", i), "synthetic-password-123", auth.RoleOwner, nil)
			}
			if err != nil && domain.CodeOf(err) != domain.CodeOwnerProtected {
				t.Errorf("owner creation: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("single owner invariant: count=%d err=%v", len(list), err)
	}
	created, err := svc.BootstrapOwner(ctx, "replacement", "replacement-password")
	if err != nil || created {
		t.Fatalf("reuse: %v %v", created, err)
	}
	if _, err := svc.Authenticate(ctx, list[0].Username, "synthetic-password-123"); err != nil {
		t.Fatalf("existing owner changed: %v", err)
	}
}

func newService(t *testing.T) (*Service, *auth.SessionStore) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "a.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	store := auth.NewSessionStore(db, time.Hour, 24*time.Hour)
	return NewService(db, store), store
}

func TestBootstrapOwnerOnce(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, err := svc.BootstrapOwner(ctx, "root", "long-password-1")
	if err != nil || !created {
		t.Fatalf("bootstrap: %v %v", created, err)
	}
	created, err = svc.BootstrapOwner(ctx, "root2", "long-password-2")
	if err != nil || created {
		t.Fatalf("second bootstrap must be a no-op, got %v %v", created, err)
	}
	list, _ := svc.List(ctx)
	if len(list) != 1 || list[0].Role != auth.RoleOwner {
		t.Fatalf("unexpected admin list: %+v", list)
	}
}

func TestAuthenticate(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, "root", "long-password-1", auth.RoleOwner, nil); err != nil {
		t.Fatal(err)
	}
	a, err := svc.Authenticate(ctx, "root", "long-password-1")
	if err != nil || a.Role != auth.RoleOwner {
		t.Fatalf("login: %v %+v", err, a)
	}
	if _, err := svc.Authenticate(ctx, "root", "wrong-password-x"); domain.CodeOf(err) != domain.CodeCredentialInvalid {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "nobody", "wrong-password-x"); domain.CodeOf(err) != domain.CodeCredentialInvalid {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestDisabledResellerCannotAuthenticate(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO resellers
		(id, slug, created_at, updated_at) VALUES ('reseller-1', 'north', 'test', 'test')`); err != nil {
		t.Fatal(err)
	}
	a, err := svc.Create(ctx, "northadmin", "long-password-1", auth.RoleAdmin, []string{"users.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE admins SET reseller_id = 'reseller-1' WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, "northadmin", "long-password-1"); err != nil {
		t.Fatalf("enabled reseller login: %v", err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE resellers SET enabled = 0 WHERE id = 'reseller-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, "northadmin", "long-password-1"); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("disabled reseller login must fail: %v", err)
	}
}

func TestResellerAdminCreationAndGrantCeiling(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO resellers
		(id, slug, permissions, created_at, updated_at)
		VALUES ('reseller-1', 'north', '["users.read","devices.read"]', 'test', 'test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateForReseller(ctx, "reseller-1", "badgrant", "long-password-1", []string{"users.create"}); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("grant above reseller ceiling: %v", err)
	}
	if _, err := svc.CreateForReseller(ctx, "reseller-1", "wildcard", "long-password-1", []string{"users.*"}); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("wildcard grant: %v", err)
	}
	a, err := svc.CreateForReseller(ctx, "reseller-1", "northadmin", "long-password-1", []string{"users.read"})
	if err != nil || a.ResellerID == nil || *a.ResellerID != "reseller-1" {
		t.Fatalf("create bound admin: %+v %v", a, err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || list[0].ResellerID == nil || *list[0].ResellerID != "reseller-1" {
		t.Fatalf("list bound admin: %+v %v", list, err)
	}
	if err := svc.SetPermissions(ctx, a.ID, []string{"users.read", "users.create"}); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("updated grant above ceiling: %v", err)
	}
	if err := svc.SetPermissions(ctx, a.ID, []string{"users.read", "devices.read"}); err != nil {
		t.Fatalf("update within ceiling: %v", err)
	}
}

func TestOwnerProtection(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	owner, _ := svc.Create(ctx, "root", "long-password-1", auth.RoleOwner, nil)
	staff, err := svc.Create(ctx, "helper", "long-password-2", auth.RoleAdmin, []string{"users.read"})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := svc.Create(ctx, "root2", "long-password-3", auth.RoleOwner, nil); domain.CodeOf(err) != domain.CodeOwnerProtected {
		t.Fatalf("second owner must be refused: %v", err)
	}
	if err := svc.SetEnabled(ctx, owner.ID, false); domain.CodeOf(err) != domain.CodeOwnerProtected {
		t.Fatalf("owner disable must be refused: %v", err)
	}
	if err := svc.Delete(ctx, owner.ID); domain.CodeOf(err) != domain.CodeOwnerProtected {
		t.Fatalf("owner delete must be refused: %v", err)
	}
	if err := svc.SetPermissions(ctx, owner.ID, []string{"users.read"}); domain.CodeOf(err) != domain.CodeOwnerProtected {
		t.Fatalf("owner demotion must be refused: %v", err)
	}
	// Non-owner management works, including session revocation on disable.
	if err := svc.SetEnabled(ctx, staff.ID, false); err != nil {
		t.Fatalf("disable admin: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "helper", "long-password-2"); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("disabled admin must fail closed: %v", err)
	}
}

func TestSetPasswordRevokesSessions(t *testing.T) {
	svc, sessions := newService(t)
	ctx := context.Background()
	owner, _ := svc.Create(ctx, "root", "long-password-1", auth.RoleOwner, nil)
	tok, _, err := sessions.Create(ctx, owner.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPassword(ctx, owner.ID, "changed-password-9"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, "root", "changed-password-9"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
	if _, err := sessions.Validate(ctx, tok); err == nil {
		t.Fatal("session survived password rotation")
	}
}

func TestUsernameRules(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	for _, name := range []string{"ab", "", "with space", "unicode-ع", "way-too-long-username-xxxxxxxxxxxxxxx"} {
		if _, err := svc.Create(ctx, name, "long-password-1", auth.RoleAdmin, nil); err == nil {
			t.Fatalf("username %q accepted", name)
		}
	}
	if _, err := svc.Create(ctx, "root", "long-password-1", auth.RoleOwner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, "root", "long-password-2", auth.RoleAdmin, nil); domain.CodeOf(err) != domain.CodeAdminExists {
		t.Fatalf("duplicate username must be ADMIN_EXISTS, got %v", err)
	}
}
