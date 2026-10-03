package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
)

func TestMaintenanceAuthorizationRechecksGrantAndKeepsDataLease(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, DatabasePath: filepath.Join(dir, "wg-guard.db"), MasterKeyFile: filepath.Join(dir, "master.key")}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	account, err := admin.NewService(db, nil).Create(ctx, "maintenance", "synthetic-password-only", auth.RoleAdmin, []string{auth.ScopeUpdateManage})
	if err != nil {
		t.Fatal(err)
	}
	release, err := authorizeMaintenanceData(ctx, cfg, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := &backup.Service{Cfg: cfg}
	if exclusive, err := service.OpenData(true); err == nil {
		exclusive.Close()
		release()
		t.Fatal("authorization lease allowed concurrent data replacement")
	}
	release()
	if _, err := os.Stat(cfg.MasterKeyFile); !os.IsNotExist(err) {
		t.Fatal("account verification initialized a master key")
	}
	if _, err := db.Exec(`UPDATE admins SET permissions='[]' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	if release, err := authorizeMaintenanceData(ctx, cfg, account.ID); !errors.Is(err, ErrMaintenanceAuthorization) {
		if release != nil {
			release()
		}
		t.Fatal("revoked maintenance grant remained usable")
	}
}
