package doctor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/boot"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

type doctorRunner struct {
	responses map[string]subprocess.Result
}

func (r *doctorRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	if result, ok := r.responses[strings.Join(argv, " ")]; ok {
		return result, nil
	}
	return subprocess.Result{}, nil
}

func newDoctorEnv(t *testing.T) (Deps, *database.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "wg-guard.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.DatabasePath = filepath.Join(dir, "wg-guard.db")
	cfg.MasterKeyFile = filepath.Join(dir, "master.key")
	cfg.Complete()
	return Deps{
		Cfg: cfg, DB: db, Reg: reg, Ring: ring,
		Backend: fake.New(),
	}, db
}

func statusOf(r *Report, name string) Check {
	t := r
	for _, c := range t.Checks {
		if c.Name == name {
			return c
		}
	}
	return Check{Status: "absent"}
}

func TestDoctorReportOverTempNode(t *testing.T) {
	deps, _ := newDoctorEnv(t)
	report, err := Run(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(report, "platform"); got.Status != StatusPass {
		t.Fatalf("platform = %s (%s)", got.Status, got.Detail)
	}
	if got := statusOf(report, "database"); got.Status != StatusPass {
		t.Fatalf("database = %s (%s)", got.Status, got.Detail)
	}
	if got := statusOf(report, "interfaces"); got.Status != StatusPass {
		t.Fatalf("interfaces = %s (%s)", got.Status, got.Detail)
	}
	if got := statusOf(report, "endpoint"); got.Status != StatusWarn {
		t.Fatalf("endpoint (unset) = %s", got.Status)
	}
	if got := statusOf(report, "backups"); got.Status != StatusWarn {
		t.Fatalf("backups (none configured) = %s (%s)", got.Status, got.Detail)
	}
	if report.Failures() != 0 {
		t.Fatalf("temp node has failures: %+v", report.Checks)
	}
}

func TestKernelModuleWarningDoesNotPromiseUnimplementedFallback(t *testing.T) {
	d := &doctor{}
	d.checkKernelModule()
	got := statusOf(&d.report, "kernel-module")
	if got.Status != StatusWarn {
		t.Fatalf("kernel module status = %s", got.Status)
	}
	if strings.Contains(got.Remedy, "will use") {
		t.Fatalf("remedy promises an unimplemented automatic fallback: %q", got.Remedy)
	}
	if !strings.Contains(got.Remedy, "not automatic") {
		t.Fatalf("remedy does not disclose the fallback limitation: %q", got.Remedy)
	}
}

func TestDoctorFixRefusesWhileServiceUp(t *testing.T) {
	deps, _ := newDoctorEnv(t)
	deps.Fix = true
	deps.ServiceUp = true
	_, err := Run(context.Background(), deps)
	if err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Fatalf("fix while service up: %v", err)
	}
}

func TestDoctorFixRunsRepairsAndRechecks(t *testing.T) {
	deps, _ := newDoctorEnv(t)
	deps.Fix = true
	// Stub the boot orchestration: the repairs themselves are covered by the
	// boot package tests — here we verify doctor wires the re-check pass.
	orig := bringUp
	bringUp = func(ctx context.Context, d Deps) (fixResult, error) {
		return &boot.Result{Reconcile: &reconcile.Report{}}, nil
	}
	defer func() { bringUp = orig }()
	report, err := Run(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Fixes) == 0 {
		t.Fatal("fix pass recorded no repairs")
	}
	// The checks list must contain a second pass (duplicate names allowed).
	var passes int
	for _, c := range report.Checks {
		if c.Name == "database" {
			passes++
		}
	}
	if passes < 2 {
		t.Fatalf("expected re-check after fixes, database checked %d times", passes)
	}
}

func TestDoctorReportsIncompleteDockerForwardingPath(t *testing.T) {
	deps, db := newDoctorEnv(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO tunnel_interfaces
		(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted,
		 preset_name, enabled, backend_mode, created_at, updated_at)
		VALUES ('iface-0', 'awg0', 39001, '10.8.0.0/24', 1420, 'pub', x'00',
		 'plain', 1, 'kernel', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	deps.Run = &doctorRunner{responses: map[string]subprocess.Result{
		"nft list table inet wgguard":      {Stdout: []byte("table inet wgguard {}\n")},
		"iptables --version":               {Stdout: []byte("iptables v1.8.10 (nf_tables)\n")},
		"iptables -w 5 -S FORWARD":         {Stdout: []byte("-P FORWARD DROP\n-A FORWARD -j DOCKER-USER\n")},
		"iptables -w 5 -S DOCKER-USER":     {Stdout: []byte("-N DOCKER-USER\n-A DOCKER-USER -j RETURN\n")},
		"iptables -w 5 -S WGGUARD-FORWARD": {Stdout: []byte("-N WGGUARD-FORWARD\n-A WGGUARD-FORWARD -s 10.8.0.0/24 -i awg0 -j ACCEPT\n")},
	}}
	doc := &doctor{d: deps}
	doc.checkFirewall(context.Background())
	got := statusOf(&doc.report, "forwarding")
	if got.Status != StatusFail || !strings.Contains(got.Detail, "DROP") {
		t.Fatalf("forwarding check=%+v", got)
	}
	if !strings.Contains(got.Remedy, "doctor --fix") {
		t.Fatalf("forwarding remedy=%q", got.Remedy)
	}
}
