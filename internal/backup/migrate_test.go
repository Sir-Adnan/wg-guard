package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeMigrationBacksUpOriginalSchemaBeforeApplying(t *testing.T) {
	s, _ := newLegacyServiceThrough0006(t)
	ctx := context.Background()
	if err := s.Reg.SetRaw(ctx, "backup.telegram_token", "synthetic-original-secret"); err != nil {
		t.Fatal(err)
	}
	lease, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	// The gate does not depend on a post-migration registry or audit service.
	s.Reg, s.Audit = nil, nil
	if err := s.MigrateNode(ctx, lease); err != nil {
		t.Fatal(err)
	}
	status, err := s.DB.MigrationStatus(ctx)
	if err != nil || status.Pending != 0 {
		t.Fatal("migration did not finish", err)
	}
	files, err := os.ReadDir(filepath.Join(s.Cfg.DataDir, "backups-auto"))
	if err != nil || len(files) != 1 || !strings.HasSuffix(files[0].Name(), ".wgg") {
		t.Fatal("pre-migration archive was not retained")
	}
	pending, _, err := s.StageOriginal(ctx, filepath.Join(s.Cfg.DataDir, "backups-auto", files[0].Name()), "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(pending.Dir, DBMember))+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&version); err != nil || version != "0006_backup_schedules.sql" {
		t.Fatal("recovery archive was captured after schema modification")
	}
	if err := s.MigrateNode(ctx, lease); err != nil {
		t.Fatal(err)
	}
	files, _ = os.ReadDir(filepath.Join(s.Cfg.DataDir, "backups-auto"))
	if len(files) != 1 {
		t.Fatal("current schema produced a redundant automatic archive")
	}
}

func TestNodeMigrationFailureKeepsSchemaAndPair(t *testing.T) {
	for _, kind := range []string{"destination", "key", "reader"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := newLegacyServiceThrough0006(t)
			ctx := context.Background()
			if err := s.Reg.SetRaw(ctx, "backup.telegram_token", "synthetic-original-secret"); err != nil {
				t.Fatal(err)
			}
			if kind == "destination" {
				if err := os.WriteFile(filepath.Join(s.Cfg.DataDir, "backups-auto"), []byte("synthetic-blocker"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "key" {
				if err := os.WriteFile(s.Cfg.MasterKeyFile, []byte("different-current-key-32-bytes!!!"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			lease, err := s.OpenData(false)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			if kind == "reader" {
				other, err := s.OpenData(false)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
			}
			beforeKey := fileHash(s.Cfg.MasterKeyFile)
			if err := s.MigrateNode(ctx, lease); err == nil {
				t.Fatal("migration continued without an exclusive verified recovery archive")
			}
			var version string
			if err := s.DB.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&version); err != nil || version != "0006_backup_schedules.sql" {
				t.Fatal("failed gate changed the original schema")
			}
			if fileHash(s.Cfg.MasterKeyFile) != beforeKey {
				t.Fatal("failed gate replaced the source key")
			}
		})
	}
}
