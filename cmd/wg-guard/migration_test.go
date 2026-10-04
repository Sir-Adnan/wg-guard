package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/migrations"
)

func legacyCLIConfig(t *testing.T, blocked bool) string {
	t.Helper()
	path := testTokenConfig(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	ring, err := secrets.LoadKeyRing(cfg.MasterKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ring.EncryptString("synthetic-migration-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('backup.telegram_token',?,'test')`, value); err != nil {
		t.Fatal(err)
	}
	if blocked {
		if err := os.WriteFile(filepath.Join(cfg.DataDir, "backups-auto"), []byte("synthetic-blocker"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestAutomaticCLIOpenersRefuseMigrationWithoutBackup(t *testing.T) {
	for _, command := range []string{"backup", "owner", "token", "reconcile"} {
		t.Run(command, func(t *testing.T) {
			path := legacyCLIConfig(t, true)
			var err error
			switch command {
			case "backup":
				_, err = loadCLIEnv(path)
			case "owner":
				_, _, err = loadOwnerService(path)
			case "token":
				_, _, err = openForToken(path)
			case "reconcile":
				err = runReconcile([]string{"--config", path})
			}
			if err == nil || !strings.Contains(err.Error(), "pre-migration archive failed") {
				t.Fatal("CLI opener crossed the required backup gate", err)
			}
			cfg, _ := config.Load(path)
			db, err := database.Open(cfg.DatabasePath, database.Options{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var version string
			if err := db.QueryRowContext(context.Background(), `SELECT MAX(version) FROM migrations`).Scan(&version); err != nil || version != "0006_backup_schedules.sql" {
				t.Fatal("CLI failure left migrated data")
			}
		})
	}
}

func TestCLIMigrationRetainsOriginalArchive(t *testing.T) {
	path := legacyCLIConfig(t, false)
	env, err := loadCLIEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	files, err := os.ReadDir(filepath.Join(env.Cfg.DataDir, "backups-auto"))
	if err != nil || len(files) != 1 || !strings.HasSuffix(files[0].Name(), ".wgg") {
		t.Fatal("CLI migrated before retaining its recovery archive")
	}
}
