package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

func TestReadableArchiveRefusesInvalidDomainData(t *testing.T) {
	ctx := context.Background()
	mutations := map[string]string{
		"profile-key-mismatch":     `UPDATE tunnel_interfaces SET public_key=? WHERE id='awg0'`,
		"private-key-format":       `UPDATE devices SET private_key_encrypted=? WHERE id='device-two'`,
		"psk-format":               `UPDATE devices SET preshared_key_encrypted=? WHERE id='device-two'`,
		"listen-port":              `UPDATE tunnel_interfaces SET listen_port=80 WHERE id='awg0'`,
		"profile-enabled":          `UPDATE tunnel_interfaces SET enabled=3 WHERE id='awg0'`,
		"overlapping-profiles":     `UPDATE tunnel_interfaces SET ipv4_subnet='10.77.0.0/23' WHERE id='awg1'`,
		"overlapping-extra-pool":   `UPDATE tunnel_interfaces SET ipv4_extra_pools='["10.77.0.0/25"]' WHERE id='awg0'`,
		"obfuscation-order":        `UPDATE tunnel_interfaces SET jc=4,jmin=80,jmax=40,s1=15,s2=64,h1_range='101',h2_range='202',h3_range='303',h4_range='404' WHERE id='awg0'`,
		"range-bounds":             `UPDATE tunnel_interfaces SET h1_range='4294967296' WHERE id='awg0'`,
		"oversized-optional-range": `UPDATE tunnel_interfaces SET h1_range=? WHERE id='awg0'`,
		"foreign-device-pool":      `UPDATE devices SET ipv4_address='10.78.0.2/32' WHERE id='device-one'`,
		"gateway-address":          `UPDATE devices SET ipv4_address='10.77.0.1/32' WHERE id='device-one'`,
		"broadcast-address":        `UPDATE devices SET ipv4_address='10.77.0.255/32' WHERE id='device-one'`,
		"device-prefix":            `UPDATE devices SET ipv4_address='10.77.0.2/24' WHERE id='device-one'`,
		"negative-device-counter":  `UPDATE devices SET last_rx=-1 WHERE id='device-one'`,
		"negative-account-counter": `UPDATE users SET traffic_used_rx=-1`,
		"total-counter-overflow":   `UPDATE users SET traffic_used_rx=9223372036854775807,traffic_used_tx=1`,
		"zero-device-limit":        `UPDATE users SET device_limit=0`,
		"text-speed":               `UPDATE users SET speed_limit_down_kbps='synthetic-invalid'`,
		"zero-duration":            `UPDATE templates SET duration_seconds=0`,
		"unknown-status":           `UPDATE users SET status='synthetic-unknown-status'`,
		"invalid-date":             `UPDATE users SET expires_at='synthetic-invalid-date'`,
		"oversized-optional-date":  `UPDATE users SET expires_at=?`,
		"customer-hash":            `UPDATE sub_links SET token_hash='synthetic-mismatched-hash'`,
		"customer-revocation-date": `UPDATE sub_links SET revoked_at='synthetic-invalid-revocation-date'`,
		"customer-token-format":    `UPDATE sub_links SET token_encrypted=?`,
		"invalid-setting":          `INSERT INTO settings(key,value,updated_at) VALUES ('accounting.interval_seconds','1','2026-01-01T00:00:00Z')`,
		"invalid-secret-setting":   `INSERT INTO settings(key,value,updated_at) VALUES ('backup.password',?,'2026-01-01T00:00:00Z')`,
		"migration-hole":           `DELETE FROM migrations WHERE version='0006_backup_schedules.sql'`,
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			s, _ := newService(t)
			seedMigrationData(t, s)
			ring, err := secrets.LoadKeyRing(s.Cfg.MasterKeyFile)
			if err != nil {
				t.Fatal(err)
			}
			var args []any
			switch name {
			case "profile-key-mismatch":
				pair, err := tunnel.GenerateKeyPair()
				if err != nil {
					t.Fatal(err)
				}
				args = []any{pair.Public}
			case "private-key-format", "psk-format", "customer-token-format":
				sealed, err := ring.Encrypt([]byte("synthetic-readable-invalid-key"))
				if err != nil {
					t.Fatal(err)
				}
				args = []any{sealed}
			case "invalid-secret-setting":
				sealed, err := ring.EncryptString("short")
				if err != nil {
					t.Fatal(err)
				}
				args = []any{sealed}
			case "oversized-optional-range", "oversized-optional-date":
				args = []any{strings.Repeat("1", 256)}
			}
			if _, err := s.DB.Exec(mutation, args...); err != nil {
				t.Fatal("could not seed invalid domain fixture")
			}
			if _, err := s.Create(ctx, CreateOpts{}); err == nil || strings.Contains(err.Error(), "synthetic-") {
				t.Fatal("archive creation accepted invalid domain data or disclosed it")
			}
			key, err := os.ReadFile(s.Cfg.MasterKeyFile)
			if err != nil {
				t.Fatal(err)
			}
			archive := uncheckedMigrationArchive(t, s, key)
			for _, original := range []bool{false, true} {
				var preview *PendingRestore
				if original {
					preview, _, err = s.StageOriginal(ctx, archive, "")
				} else {
					preview, _, err = s.Stage(ctx, archive, "")
				}
				if err == nil || preview != nil || strings.Contains(err.Error(), "synthetic-") {
					t.Fatal("readable invalid archive published a preview or disclosed stored values")
				}
			}
		})
	}
}

func TestSemanticGateRechecksPreviouslyApprovedPreviewBeforeReplacement(t *testing.T) {
	source, _ := newService(t)
	seedMigrationData(t, source)
	archive, err := source.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := newService(t)
	preview, _, err := target.Stage(context.Background(), archive.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(preview.Dir, DBMember), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE devices SET ipv4_address='10.77.0.1/32' WHERE id='device-one'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// Model an older build's valid-hash preview that did no domain validation.
	if err := os.Remove(filepath.Join(preview.Dir, pendingMeta)); err != nil {
		t.Fatal(err)
	}
	if err := target.writeStagedMeta(preview, archive.Size); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Approve(preview.PreviewID()); err != nil {
		t.Fatal(err)
	}
	target.DB.Close()
	beforeDB, beforeKey := fileHash(target.Cfg.DatabasePath), fileHash(target.Cfg.MasterKeyFile)
	_, err = target.ApplyStaged(context.Background())
	var message Message
	if err == nil || !errors.As(err, &message) || message.Key != "data_semantics" {
		t.Fatal("older approved preview bypassed domain validation")
	}
	if fileHash(target.Cfg.DatabasePath) != beforeDB || fileHash(target.Cfg.MasterKeyFile) != beforeKey {
		t.Fatal("failed semantic validation replaced active data")
	}
}

func TestSemanticGatePreservesHistoricalAndOverLimitRecords(t *testing.T) {
	s, _ := newService(t)
	seedMigrationData(t, s)
	if _, err := s.DB.Exec(`UPDATE users SET enabled=0,status='expired',device_limit=1,traffic_limit_bytes=0,deleted_at='2026-01-03T00:00:00Z',expires_at='2026-01-02T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('historical.unknown-setting','preserve','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	archive, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := VerifyArchive(context.Background(), archive.Path, "")
	if err != nil || report.Inventory.Users != 1 || report.Inventory.Devices != 2 {
		t.Fatal("semantic validation rejected valid historical/over-limit data", err)
	}
}
