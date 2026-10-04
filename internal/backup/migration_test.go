package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Seed real envelopes and keys in multiple records so the audit must go beyond
// a first-row startup sample. All material is generated in a temporary fixture.
func seedMigrationData(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	ring, err := secrets.LoadKeyRing(s.Cfg.MasterKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, statement, args...); err != nil {
			t.Fatal("could not seed migration fixture")
		}
	}
	for index, name := range []string{"awg0", "awg1"} {
		pair, err := tunnel.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		private, err := ring.Encrypt([]byte(pair.Private))
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO tunnel_interfaces (id,name,listen_port,ipv4_subnet,mtu,public_key,private_key_encrypted,created_at,updated_at)
			VALUES (?, ?, ?, ?, 1380, ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			name, name, 39001+index, []string{"10.77.0.0/24", "10.78.0.0/24"}[index], pair.Public, private)
	}
	exec(`INSERT INTO templates (id,name,interface_id,traffic_limit_bytes,duration_seconds,device_limit,created_at,updated_at)
		VALUES ('template-one','migration-template','awg0',100000000000,2592000,2,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	exec(`INSERT INTO users (id,username,template_id,interface_id,traffic_limit_bytes,traffic_used_rx,traffic_used_tx,duration_seconds,expires_at,device_limit,created_at,updated_at)
		VALUES ('user-one','migration-user','template-one','awg0',100000000000,1234567,7654321,2592000,'2026-12-01T00:00:00Z',2,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	for index, id := range []string{"device-one", "device-two"} {
		pair, err := tunnel.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		psk, err := tunnel.GeneratePresharedKey()
		if err != nil {
			t.Fatal(err)
		}
		private, err := ring.Encrypt([]byte(pair.Private))
		if err != nil {
			t.Fatal(err)
		}
		shared, err := ring.Encrypt([]byte(psk))
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO devices (id,user_id,interface_id,name,ipv4_address,public_key,private_key_encrypted,preshared_key_encrypted,rx_bytes,tx_bytes,created_at,updated_at)
			VALUES (?, 'user-one', 'awg0', ?, ?, ?, ?, ?, 1234567, 7654321, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			id, id, []string{"10.77.0.2/32", "10.77.0.3/32"}[index], pair.Public, private, shared)
	}
	if _, err := subscription.NewService(s.DB, ring).Ensure(ctx, "user-one"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"hook-one", "hook-two"} {
		secret, err := ring.EncryptString("synthetic-migration-webhook-secret")
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO webhook_endpoints (id,url,secret_encrypted,events,created_at)
			VALUES (?, 'https://hooks.example/migration', ?, '[]', '2026-01-01T00:00:00Z')`, id, secret)
	}
	for key, value := range map[string]string{
		"node.endpoint": "vpn.example.com", "subscription.base_url": "https://sub.example.com",
		"backup.telegram_token": "synthetic-migration-telegram-token",
	} {
		if err := s.Reg.SetRaw(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
}

// Write a checksummed but deliberately unaudited archive to model older builds,
// mismatched DB/key pairs and corruption that checksums alone cannot detect.
func uncheckedMigrationArchive(t *testing.T, s *Service, key []byte) string {
	t.Helper()
	dir := t.TempDir()
	databasePath := filepath.Join(dir, DBMember)
	if err := s.snapshotDB(context.Background(), databasePath); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Schema: SchemaVersion, Files: map[string]string{DBMember: fileHash(databasePath)}}
	members := []member{{name: DBMember, path: databasePath}}
	if key != nil {
		manifest.Files[KeyMember] = hashBytes(key)
		members = append(members, member{name: KeyMember, data: key})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	members = append(members, member{name: ManifestName, data: raw})
	path := filepath.Join(dir, "wg-guard-migration.wgg")
	if err := s.writeArchive(path, "", members); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrationRejectsMissingWrongAndMixedArchiveKeys(t *testing.T) {
	for _, kind := range []string{"missing", "wrong", "interface", "device", "preshared", "subscription", "webhook", "settings", "malformed", "malformed-text", "oversized-text"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := newService(t)
			seedMigrationData(t, s)
			key, err := os.ReadFile(s.Cfg.MasterKeyFile)
			if err != nil {
				t.Fatal(err)
			}
			wrong := bytes.Repeat([]byte{0x42}, 32)
			cipher, err := secrets.NewCipher(wrong)
			if err != nil {
				t.Fatal(err)
			}
			bad, _ := cipher.Encrypt([]byte("synthetic-foreign-key-value"))
			badText, _ := cipher.EncryptString("synthetic-foreign-key-value")
			var query string
			var value any = bad
			switch kind {
			case "missing":
				key = nil
			case "wrong":
				key = wrong
			case "interface":
				query = `UPDATE tunnel_interfaces SET private_key_encrypted=? WHERE id='awg1'`
			case "device", "malformed", "oversized-text":
				query = `UPDATE devices SET private_key_encrypted=? WHERE id='device-two'`
				if kind == "malformed" {
					value = []byte("synthetic-malformed-envelope")
				}
				if kind == "oversized-text" {
					value = "\x00" + strings.Repeat("x", maxSecretEnvelopeBytes+1)
				}
			case "preshared":
				query = `UPDATE devices SET preshared_key_encrypted=? WHERE id='device-two'`
			case "subscription":
				query = `UPDATE sub_links SET token_encrypted=?`
			case "webhook":
				query, value = `UPDATE webhook_endpoints SET secret_encrypted=? WHERE id='hook-two'`, badText
			case "malformed-text":
				query, value = `UPDATE webhook_endpoints SET secret_encrypted=? WHERE id='hook-two'`, "enc:synthetic-invalid-base64"
			case "settings":
				query, value = `UPDATE settings SET value=? WHERE key='backup.telegram_token'`, badText
			}
			if query != "" {
				if _, err := s.DB.Exec(query, value); err != nil {
					t.Fatal(err)
				}
			}
			path := uncheckedMigrationArchive(t, s, key)
			beforeKey := fileHash(s.Cfg.MasterKeyFile)
			if _, _, err := s.Stage(context.Background(), path, ""); err == nil {
				t.Fatal("invalid portable database/key pair was accepted")
			} else if strings.Contains(err.Error(), "synthetic-") {
				t.Fatal("secret fixture value leaked in validation error")
			}
			if fileHash(s.Cfg.MasterKeyFile) != beforeKey {
				t.Fatal("failed preview replaced the active key")
			}
			if query != "" {
				if _, err := s.Create(context.Background(), CreateOpts{Password: "synthetic-archive-password"}); err == nil {
					t.Fatal("an archive containing undecryptable values was published")
				}
			}
		})
	}
}

func TestArchiveCannotDependOnAnUnarchivedRotationKey(t *testing.T) {
	s, _ := newService(t)
	seedMigrationData(t, s)
	original, err := os.ReadFile(s.Cfg.MasterKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Cfg.MasterKeyFile+secrets.KeyFileSuffixPrev, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Cfg.MasterKeyFile, bytes.Repeat([]byte{0x42}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.LoadNodeKeyRing(context.Background(), s.DB.DB, s.Cfg.MasterKeyFile); err != nil {
		t.Fatal("rotation fixture should remain readable with the previous key")
	}
	if _, err := s.Create(context.Background(), CreateOpts{}); err == nil {
		t.Fatal("archive silently depended on a key not included in its payload")
	}
	if archives, err := s.List(); err != nil || len(archives) != 0 {
		t.Fatal("invalid rotation-window archive was published")
	}
}

func TestArchiveRejectsEmptyExistingMasterKey(t *testing.T) {
	s, _ := newService(t)
	if err := os.WriteFile(s.Cfg.MasterKeyFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(context.Background(), CreateOpts{}); err == nil {
		t.Fatal("damaged existing key was treated as an intentionally keyless archive")
	}
}

func TestPreviouslyApprovedArchiveIsCheckedBeforeReplacement(t *testing.T) {
	s, _ := newService(t)
	seedMigrationData(t, s)
	path := uncheckedMigrationArchive(t, s, bytes.Repeat([]byte{0x42}, 32))
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dir, err := os.MkdirTemp(s.Cfg.DataDir, previewPrefix)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := extractArchive(context.Background(), f, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	// Model the old hash-only preview, which an operator could have approved
	// before updating the application. New apply must not trust that preview.
	pending := &PendingRestore{Dir: dir, Manifest: *manifest, Archive: filepath.Base(path), StagedAt: s.now()}
	if err := s.writeStagedMeta(pending, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(pending.PreviewID()); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()
	beforeDB, beforeKey := fileHash(s.Cfg.DatabasePath), fileHash(s.Cfg.MasterKeyFile)
	if _, err := s.ApplyStaged(context.Background()); err == nil {
		t.Fatal("older approved preview replaced the valid database/key pair")
	}
	if fileHash(s.Cfg.DatabasePath) != beforeDB || fileHash(s.Cfg.MasterKeyFile) != beforeKey {
		t.Fatal("failed portable-pair check changed live data")
	}
	if _, err := os.Stat(filepath.Join(s.Cfg.DataDir, transactionDir)); !os.IsNotExist(err) {
		t.Fatal("replacement began before archive data validation completed")
	}
}

func TestMigrationRejectsBrokenReferencesAndCancellation(t *testing.T) {
	s, _ := newService(t)
	seedMigrationData(t, s)
	conn, err := s.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`PRAGMA foreign_keys=OFF`, `UPDATE devices SET user_id='absent-user' WHERE id='device-two'`, `PRAGMA foreign_keys=ON`} {
		if _, err := conn.ExecContext(context.Background(), query); err != nil {
			conn.Close()
			t.Fatal(err)
		}
	}
	conn.Close()
	key, _ := os.ReadFile(s.Cfg.MasterKeyFile)
	path := uncheckedMigrationArchive(t, s, key)
	if _, err := VerifyArchive(context.Background(), path, ""); err == nil {
		t.Fatal("broken user/device relationship was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyArchive(ctx, path, ""); !errors.Is(err, context.Canceled) {
		t.Fatal("offline verification lost cancellation")
	}
}

func TestMigrationRestoreToFreshLayoutPreservesConfigsAndCustomerAccess(t *testing.T) {
	source, sourceDir := newService(t)
	seedMigrationData(t, source)
	writeBootConfig(t, sourceDir)
	ctx := context.Background()
	beforeConfig, beforeToken := migrationClientOutputs(t, source)
	archive, err := source.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyArchive(ctx, archive.Path, "")
	if err != nil || verified.Inventory.Users != 1 || verified.Inventory.Devices != 2 || verified.Inventory.EncryptedValues != 10 {
		t.Fatal("complete offline inventory was not verified", err)
	}
	target, targetDir := newService(t)
	if targetDir == sourceDir {
		t.Fatal("migration fixture reused the source layout")
	}
	activeConfig := []byte("http_listen = \"127.0.0.1:9090\"\n[tls]\nmode = \"proxy\"\n")
	if err := os.WriteFile(target.ConfigPath, activeConfig, 0600); err != nil {
		t.Fatal(err)
	}
	pending, _, err := target.Stage(ctx, archive.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	target.DB.Close()
	if _, err := target.Approve(pending.PreviewID()); err != nil {
		t.Fatal(err)
	}
	if _, err := target.ApplyStaged(ctx); err != nil {
		t.Fatal(err)
	}
	target.DB, err = database.Open(target.Cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer target.DB.Close()
	ring, err := secrets.LoadNodeKeyRing(ctx, target.DB.DB, target.Cfg.MasterKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	target.Reg, err = settings.New(target.DB, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	afterConfig, afterToken := migrationClientOutputs(t, target)
	if sha256.Sum256([]byte(beforeConfig)) != sha256.Sum256([]byte(afterConfig)) || beforeToken != afterToken {
		t.Fatal("migration changed client configuration or customer access")
	}
	var rx, tx int64
	if err := target.DB.QueryRow(`SELECT traffic_used_rx,traffic_used_tx FROM users WHERE id='user-one'`).Scan(&rx, &tx); err != nil || rx != 1234567 || tx != 7654321 {
		t.Fatal("migration lost charged traffic")
	}
	gotConfig, _ := os.ReadFile(target.ConfigPath)
	if !bytes.Equal(gotConfig, activeConfig) {
		t.Fatal("archived paths or listener replaced the target boot settings")
	}
}

func migrationClientOutputs(t *testing.T, s *Service) (string, string) {
	t.Helper()
	ring, err := secrets.LoadKeyRing(s.Cfg.MasterKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	renderer := &clientconf.Renderer{Devices: device.NewService(s.DB, ring), Ifaces: iface.NewService(s.DB, s.Reg, ring), Settings: s.Reg}
	config, err := renderer.Render(context.Background(), "device-one")
	if err != nil {
		t.Fatal("fixture client configuration could not be rendered")
	}
	link, err := subscription.NewService(s.DB, ring).Ensure(context.Background(), "user-one")
	if err != nil {
		t.Fatal(err)
	}
	return config, link.Token
}
