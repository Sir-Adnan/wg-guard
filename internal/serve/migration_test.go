package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
	"github.com/Sir-Adnan/wg-guard/migrations"
)

func TestStartupStopsBeforeMigrationWhenRequiredArchiveFails(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001_init.sql", "0002_speed_limits.sql", "0003_admin_locale.sql", "0004_sub_links.sql", "0005_iface_advanced.sql", "0006_backup_schedules.sql"} {
		body, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO migrations VALUES(?,'test')`, name); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if _, err := secrets.LoadKeyRing(cfg.MasterKeyFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "backups-auto"), []byte("synthetic-blocker"), 0600); err != nil {
		t.Fatal(err)
	}
	if node, err := Start(context.Background(), Options{Config: cfg, Backend: fake.New(), Log: quietLogger()}); err == nil || node != nil {
		t.Fatal("startup continued after required backup failure")
	}
	db, err = database.Open(cfg.DatabasePath, database.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&version); err != nil || version != "0006_backup_schedules.sql" {
		t.Fatal("startup modified the old schema before a recovery archive existed")
	}
	lease, err := (&backup.Service{Cfg: cfg}).OpenData(true)
	if err != nil {
		t.Fatal("failed startup leaked data ownership")
	}
	lease.Close()
}
