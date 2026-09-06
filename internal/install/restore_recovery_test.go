package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/migrations"
	"os"
	"strings"
	"testing"
	"time"
)

type recoveringHost struct {
	*memHost
	beforeStart func()
}

func (h *recoveringHost) Run(ctx context.Context, args []string, timeout time.Duration) error {
	joined := strings.Join(args, " ")
	if strings.Contains(joined, " restart ") || strings.Contains(joined, " up -d") {
		h.beforeStart()
	}
	return h.memHost.Run(ctx, args, timeout)
}

func TestRestoreRecoveryVerifiesOriginalArchiveBeforeStartingPreviousArtifact(t *testing.T) {
	for _, mode := range []Mode{ModeNative, ModeDocker} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			cfg := config.Defaults()
			cfg.DataDir = dir
			cfg.Complete()
			db, err := database.Open(cfg.DatabasePath, database.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"0001_init.sql", "0002_speed_limits.sql", "0003_admin_locale.sql", "0004_sub_links.sql", "0005_iface_advanced.sql", "0006_backup_schedules.sql"} {
				raw, err := migrations.Read(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(string(raw)); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO migrations VALUES(?, 'test')`, name); err != nil {
					t.Fatal(err)
				}
			}
			ring, err := secrets.LoadKeyRing(cfg.MasterKeyFile)
			if err != nil {
				t.Fatal(err)
			}
			reg, err := settings.New(db, ring, settings.Defaults())
			if err != nil {
				t.Fatal(err)
			}
			if err := reg.SetRaw(ctx, "backup.telegram_token", "synthetic-original-token"); err != nil {
				t.Fatal(err)
			}
			svc := &backup.Service{Cfg: cfg, DB: db, Reg: reg, Version: "legacy"}
			archive, err := svc.Create(ctx, backup.CreateOpts{Password: "synthetic-recovery-password"})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Migrate(ctx, nil); err != nil {
				t.Fatal(err)
			}
			db.Close()
			if err := os.WriteFile(cfg.MasterKeyFile, bytes.Repeat([]byte{1}, 32), 0600); err != nil {
				t.Fatal(err)
			}
			m := installedFixture(t, mode)
			contractFixture(m)
			st, _ := LoadState(m)
			previous, err := retainCurrent(ctx, m, st)
			if err != nil {
				t.Fatal(err)
			}
			previous.Contract = Contract{}
			raw, err := os.ReadFile(archive.Path)
			if err != nil {
				t.Fatal(err)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(raw))
			recorded := DataDir + "/backups/lifecycle-" + strings.Repeat("e", 32) + "/" + archive.Name
			m.files[recorded] = memFile{data: raw, perm: 0600}
			previous.Backup = &BackupIdentity{Path: recorded, SHA256: digest, Encrypted: true, RestoreRequired: true}
			j := &Journal{Schema: 1, ID: strings.Repeat("d", 32), Operation: "update", Before: st, Previous: previous, Candidate: &Artifact{Binary: ArtifactDir + "/" + strings.Repeat("f", 32) + "/binary", BinarySHA256: strings.Repeat("c", 64), Contract: CurrentContract()}, DataMayHaveChanged: true}
			if err := j.save(m, "restore-required"); err != nil {
				t.Fatal(err)
			}
			m.files[BinPath] = memFile{data: []byte("candidate")}
			if mode == ModeDocker {
				m.output["docker run --rm --network none --entrypoint sha256sum "+previous.Image+" "+BinPath] = previous.BinarySHA256 + "  " + BinPath
			}
			h := &recoveringHost{memHost: m, beforeStart: func() {
				restored, err := database.Open(cfg.DatabasePath, database.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer restored.Close()
				var count int
				if err := restored.QueryRow(`SELECT COUNT(*) FROM migrations WHERE version='0007_awg_ranges.sql'`).Scan(&count); err != nil || count != 0 {
					t.Fatal("old code started on migrated schema")
				}
				key, err := secrets.LoadKeyRing(cfg.MasterKeyFile)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := settings.New(restored, key, settings.Defaults())
				if err != nil {
					t.Fatal(err)
				}
				token, err := registry.GetSecret(ctx, "backup.telegram_token")
				if err != nil || token != "synthetic-original-token" {
					t.Fatal("database/key pair does not decrypt original settings")
				}
				if string(m.files[BinPath].data) != "/src/wg-guard" {
					t.Fatal("previous artifact not restored before start")
				}
			}}
			prepareCalls := 0
			opts := RestoreOptions{Recover: true, Prepare: func(ctx context.Context, id *BackupIdentity) (func(context.Context) error, error) {
				prepareCalls++
				p, _, err := svc.StageRecovery(ctx, archive.Path, "synthetic-recovery-password", id.SHA256, id.Encrypted)
				if err != nil {
					return nil, err
				}
				return func(ctx context.Context) error { _, err := svc.ApplyOriginal(ctx, p.PreviewID()); return err }, nil
			}}
			// Identity failures must not even enter decrypt/stage, stop or start.
			good := m.files[recorded]
			m.files[recorded] = memFile{data: []byte("tampered")}
			if err := Restore(ctx, h, opts); err == nil || prepareCalls != 0 {
				t.Fatal("unverified archive allowed into recovery")
			}
			m.files[recorded] = good
			if err := Restore(ctx, h, opts); err != nil {
				t.Fatal(err)
			}
			final, err := LoadJournal(h)
			if err != nil || final.Stage != "rolled-back" {
				t.Fatal("recovery journal not synchronized")
			}
		})
	}
}
