package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
)

func installArchive(t *testing.T) (*Service, string) {
	t.Helper()
	s, _ := newService(t)
	seedMigrationData(t, s)
	if _, err := admin.NewService(s.DB, nil).BootstrapOwner(context.Background(), "source-owner", "synthetic-owner-password"); err != nil {
		t.Fatal(err)
	}
	if err := s.Reg.SetRaw(context.Background(), "node.endpoint", "vpn.source.example.test"); err != nil {
		t.Fatal(err)
	}
	arc, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	return s, arc.Path
}

func TestInstallArchiveKeepsOwnerKeySettingsAndTargetBoot(t *testing.T) {
	s, arc := installArchive(t)
	p, err := PrepareInstall(context.Background(), arc, "")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Complete()
	boot := filepath.Join(t.TempDir(), "target.toml")
	wantBoot := []byte("target listener remains private")
	if err := os.WriteFile(boot, wantBoot, 0600); err != nil {
		t.Fatal(err)
	}
	key := fileHash(s.Cfg.MasterKeyFile)
	var beforeDevices, beforeLink, beforeUsage string
	if err := s.DB.QueryRow(`SELECT group_concat(id||':'||ipv4_address||':'||public_key||':'||hex(private_key_encrypted)||':'||hex(preshared_key_encrypted)) FROM devices ORDER BY id`).Scan(&beforeDevices); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT token_hash||':'||hex(token_encrypted) FROM sub_links WHERE user_id='user-one'`).Scan(&beforeLink); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT traffic_limit_bytes||':'||traffic_used_rx||':'||traffic_used_tx||':'||expires_at FROM users WHERE id='user-one'`).Scan(&beforeUsage); err != nil {
		t.Fatal(err)
	}
	if err := p.ApplyInitialData(context.Background(), cfg, boot); err != nil {
		t.Fatal(err)
	}
	if fileHash(cfg.MasterKeyFile) != key {
		t.Fatal("source master key changed")
	}
	raw, err := os.ReadFile(boot)
	if err != nil || string(raw) != string(wantBoot) {
		t.Fatal("archived TLS replaced target boot")
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{ReadOnly: true, MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var afterDevices, afterLink, afterUsage string
	if err := db.QueryRow(`SELECT group_concat(id||':'||ipv4_address||':'||public_key||':'||hex(private_key_encrypted)||':'||hex(preshared_key_encrypted)) FROM devices ORDER BY id`).Scan(&afterDevices); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT token_hash||':'||hex(token_encrypted) FROM sub_links WHERE user_id='user-one'`).Scan(&afterLink); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT traffic_limit_bytes||':'||traffic_used_rx||':'||traffic_used_tx||':'||expires_at FROM users WHERE id='user-one'`).Scan(&afterUsage); err != nil {
		t.Fatal(err)
	}
	if beforeDevices != afterDevices || beforeLink != afterLink || beforeUsage != afterUsage {
		t.Fatal("device credentials, customer access or quota/usage changed")
	}
	var endpoint string
	var owners int
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='node.endpoint'`).Scan(&endpoint); err != nil || endpoint != "vpn.source.example.test" {
		t.Fatal("endpoint defaults replaced archive")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM admins WHERE role='owner' AND enabled=1`).Scan(&owners); err != nil || owners != 1 {
		t.Fatal("source owner not preserved")
	}
	if _, err := admin.NewService(db, nil).Authenticate(context.Background(), "source-owner", "synthetic-owner-password"); err != nil {
		t.Fatal("source credentials no longer work")
	}
	if err := p.ApplyInitialData(context.Background(), cfg, boot); err == nil {
		t.Fatal("archive overwrite of initialized data accepted")
	}
}

func TestInstallArchiveRefusesMissingOwnerAndChangedPreview(t *testing.T) {
	for _, kind := range []string{"no-owner", "disabled-owner", "changed-preview", "existing-key", "busy"} {
		t.Run(kind, func(t *testing.T) {
			s, arc := installArchive(t)
			if kind == "no-owner" || kind == "disabled-owner" {
				query := `DELETE FROM admins`
				if kind == "disabled-owner" {
					query = `UPDATE admins SET enabled=0`
				}
				if _, err := s.DB.Exec(query); err != nil {
					t.Fatal(err)
				}
				a, err := s.Create(context.Background(), CreateOpts{})
				if err != nil {
					t.Fatal(err)
				}
				if p, err := PrepareInstall(context.Background(), a.Path, ""); err == nil {
					p.Close()
					t.Fatal("archive without an enabled source owner accepted")
				}
				return
			}
			p, err := PrepareInstall(context.Background(), arc, "")
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			cfg := config.Defaults()
			cfg.DataDir = t.TempDir()
			cfg.Complete()
			if kind == "changed-preview" {
				if err := os.WriteFile(filepath.Join(p.preview.Dir, KeyMember), []byte("changed preview material"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "existing-key" {
				if err := os.WriteFile(cfg.MasterKeyFile, []byte("existing target material"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "busy" {
				lease, err := (&Service{Cfg: cfg}).OpenData(false)
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Close()
			}
			if err := p.ApplyInitialData(context.Background(), cfg, ""); err == nil {
				t.Fatal("unsafe target/source accepted")
			}
			if _, err := os.Stat(cfg.DatabasePath); !os.IsNotExist(err) {
				t.Fatal("failed initial restore published a database")
			}
		})
	}
}
