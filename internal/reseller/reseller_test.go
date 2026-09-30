package reseller

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func TestResellerGrantCeiling(t *testing.T) {
	for _, scopes := range [][]string{{"users.*"}, {"node.read"}, {"node.settings"}, {"admins.manage"}, {"interfaces.write"}, {"backup.manage"}} {
		if _, err := ValidatePermissions(scopes); err == nil {
			t.Fatalf("unsafe grant accepted: %v", scopes)
		}
	}
	grants, err := ValidatePermissions([]string{"users.read", "devices.write", "users.read"})
	if err != nil || len(grants) != 2 || grants[0] != "devices.write" || grants[1] != "users.read" {
		t.Fatalf("normalized grants = %v, %v", grants, err)
	}
}

func TestResellerAccountPersistenceAndDisable(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(filepath.Join(t.TempDir(), "reseller.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	a, err := svc.Create(ctx, "North-Team", "North team", []string{"users.read", "users.create"})
	if err != nil || a.Slug != "north-team" {
		t.Fatalf("create = %+v, %v", a, err)
	}
	if err := svc.SetPermissions(ctx, a.ID, []string{"users.read"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEnabled(ctx, a.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, a.ID)
	if err != nil || got.Enabled || len(got.Permissions) != 1 || got.Permissions[0] != "users.read" {
		t.Fatalf("persisted account = %+v, %v", got, err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO templates (id, name, enabled, created_at, updated_at) VALUES
		('p1', 'Basic', 1, 'test', 'test'), ('p2', 'Retired', 0, 'test', 'test')`); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTemplates(ctx, a.ID, []string{"p1"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTemplates(ctx, a.ID, []string{"p2"}); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("disabled plan assigned: %v", err)
	}
	plans, err := svc.Templates(ctx, a.ID)
	if err != nil || len(plans) != 1 || plans[0] != "p1" {
		t.Fatalf("failed assignment changed plans: %v, %v", plans, err)
	}
	if err := svc.SetTemplates(ctx, a.ID, nil); err != nil {
		t.Fatal(err)
	}
	plans, err = svc.Templates(ctx, a.ID)
	if err != nil || len(plans) != 0 {
		t.Fatalf("clear assignments: %v, %v", plans, err)
	}
}
