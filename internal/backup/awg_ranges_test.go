package backup

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/migrations"
)

type archivedAWGState struct {
	Ranges          [4]string
	LegacyLow       [4]int64
	Keepalive       string
	LegacyKeepalive string
	Padding         string
	Preset          string
	Endpoint        string
	NodeID          string
	Migration0007   int
}

func TestPre0007AWGRangeBackupRestore(t *testing.T) {
	ctx := context.Background()
	svc, dir := newLegacyServiceThrough0006(t)
	writeBootConfig(t, dir)

	if _, err := svc.DB.ExecContext(ctx, `INSERT INTO tunnel_interfaces
		(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted,
		 jc, jmin, jmax, s1, s2, h1, h2, h3, h4, preset_name, enabled, backend_mode,
		 endpoint_override, created_at, updated_at, s3, s4, content_padding_addition)
		VALUES ('legacy-iface', 'awg0', 39001, '10.77.0.0/24', 1380, 'synthetic-public', X'0102',
		        4, 40, 70, 15, 64, 101, 202, 303, 404, 'recommended', 1, 'kernel',
		        'legacy.example.com', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z',
		        0, 0, '10-100')`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `INSERT INTO plans
		(id, name, interface_id, created_at, updated_at)
		VALUES ('legacy-plan', 'kept-plan', 'legacy-iface', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES
		('network.client_keepalive_seconds', '37', '2026-01-03T00:00:00Z'),
		('node.id', 'legacy-node', '2026-01-03T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Create(ctx, CreateOpts{Reason: "phase8-pre-0007"})
	if err != nil {
		t.Fatal(err)
	}
	pending, report, err := svc.Stage(ctx, res.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	assertRestoreReportInterface(t, report, "awg0", 39001, "10.77.0.0/24")
	want := archivedAWGState{
		Ranges:    [4]string{"101", "202", "303", "404"},
		LegacyLow: [4]int64{101, 202, 303, 404},
		Keepalive: "37", LegacyKeepalive: "37", Padding: "10-100",
		Preset: "recommended", Endpoint: "legacy.example.com", NodeID: "legacy-node",
		Migration0007: 1,
	}
	assertArchivedAWGState(t, filepath.Join(pending.Dir, DBMember), "legacy-iface", want)

	if err := svc.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyStaged(ctx); err != nil {
		t.Fatal(err)
	}
	assertArchivedAWGState(t, svc.Cfg.DatabasePath, "legacy-iface", want)
	restored, err := database.Open(svc.Cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var planInterface string
	if err := restored.QueryRowContext(ctx, `SELECT interface_id FROM plans WHERE id='legacy-plan'`).Scan(&planInterface); err != nil || planInterface != "legacy-iface" {
		t.Fatalf("unrelated plan relationship = %q, %v", planInterface, err)
	}
}

func TestPost0007AWGRangeBackupRestore(t *testing.T) {
	ctx := context.Background()
	svc, dir := newService(t)
	writeBootConfig(t, dir)
	ring, err := secrets.LoadKeyRing(svc.Cfg.MasterKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	parseH := func(text string) awgparam.U32Range {
		value, err := awgparam.ParseU32Range(text)
		if err != nil {
			t.Fatalf("parse H range %q: %v", text, err)
		}
		return value
	}
	padding, err := awgparam.ParseU16Range("10-20")
	if err != nil {
		t.Fatal(err)
	}
	ifaces := iface.NewService(svc.DB, svc.Reg, ring)
	created, err := ifaces.Create(ctx, iface.CreateInput{
		Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24", MTU: 1380,
		EndpointOverride: "range.example.com", Preset: "custom",
		Obfuscation: iface.Obfuscation{
			Enabled: true, Jc: 4, Jmin: 40, Jmax: 70, S1: 15, S2: 64,
			H1: parseH("100-110"), H2: parseH("200-220"),
			H3: parseH("300-330"), H4: parseH("400-440"),
			ContentPaddingAddition: padding,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Reg.Set(ctx, "network.client_persistent_keepalive", "40-50"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reg.Set(ctx, "node.id", "range-node"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Create(ctx, CreateOpts{Reason: "phase8-post-0007"})
	if err != nil {
		t.Fatal(err)
	}
	pending, report, err := svc.Stage(ctx, res.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	assertRestoreReportInterface(t, report, "awg0", 39001, "10.77.0.0/24")
	want := archivedAWGState{
		Ranges:    [4]string{"100-110", "200-220", "300-330", "400-440"},
		LegacyLow: [4]int64{100, 200, 300, 400}, Keepalive: "40-50",
		Padding: "10-20", Preset: "custom", Endpoint: "range.example.com",
		NodeID: "range-node", Migration0007: 1,
	}
	assertArchivedAWGState(t, filepath.Join(pending.Dir, DBMember), created.ID, want)

	if err := svc.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyStaged(ctx); err != nil {
		t.Fatal(err)
	}
	assertArchivedAWGState(t, svc.Cfg.DatabasePath, created.ID, want)
}

func newLegacyServiceThrough0006(t *testing.T) (*Service, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wg-guard.db")
	keyFile := filepath.Join(dir, "master.key")
	db, err := database.Open(dbPath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TABLE migrations (
		version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"0001_init.sql", "0002_speed_limits.sql", "0003_admin_locale.sql",
		"0004_sub_links.sql", "0005_iface_advanced.sql", "0006_backup_schedules.sql",
	} {
		body, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO migrations (version, applied_at) VALUES (?, 'test')`, name); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}
	ring, err := secrets.LoadKeyRing(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir, cfg.DatabasePath, cfg.MasterKeyFile = dir, dbPath, keyFile
	cfg.Complete()
	return &Service{
		DB: db, Reg: reg, Audit: audit.NewService(db), Cfg: cfg,
		ConfigPath: filepath.Join(dir, "wg-guard.toml"), Version: "test",
		Now: func() time.Time { return time.Now().UTC() },
	}, dir
}

func assertRestoreReportInterface(t *testing.T, report *RestoreReport, name string, port int, subnet string) {
	t.Helper()
	if len(report.Interfaces) != 1 || report.Interfaces[0] != (IfaceSummary{Name: name, Port: port, Subnet: subnet}) {
		t.Fatalf("restore interface report = %+v", report.Interfaces)
	}
}

func assertArchivedAWGState(t *testing.T, dbPath, interfaceID string, want archivedAWGState) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(dbPath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var (
		got                                    archivedAWGState
		legacyH1, legacyH2, legacyH3, legacyH4 sql.NullInt64
		padding, endpoint                      sql.NullString
	)
	if err := db.QueryRowContext(ctx, `SELECT h1_range, h2_range, h3_range, h4_range,
		h1, h2, h3, h4, content_padding_addition, preset_name, endpoint_override
		FROM tunnel_interfaces WHERE id = ?`, interfaceID).Scan(
		&got.Ranges[0], &got.Ranges[1], &got.Ranges[2], &got.Ranges[3],
		&legacyH1, &legacyH2, &legacyH3, &legacyH4, &padding, &got.Preset, &endpoint,
	); err != nil {
		t.Fatal(err)
	}
	for i, value := range []sql.NullInt64{legacyH1, legacyH2, legacyH3, legacyH4} {
		if value.Valid {
			got.LegacyLow[i] = value.Int64
		}
	}
	got.Padding, got.Endpoint = padding.String, endpoint.String
	got.Keepalive = settingValue(t, db, "network.client_persistent_keepalive")
	got.LegacyKeepalive = settingValue(t, db, "network.client_keepalive_seconds")
	got.NodeID = settingValue(t, db, "node.id")
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM migrations WHERE version='0007_awg_ranges.sql'`).Scan(&got.Migration0007); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("restored AWG state = %+v, want %+v", got, want)
	}
}

func settingValue(t *testing.T, db *database.DB, key string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return value
}
