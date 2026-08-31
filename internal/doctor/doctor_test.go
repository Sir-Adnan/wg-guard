package doctor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/boot"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

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
