package nodestate

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
)

func fixtureOptions(t *testing.T) Options {
	t.Helper()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Complete()
	return Options{Config: cfg, Version: "fixture"}
}

func TestDatabaseOnlyDoesNotCreateKeysAndExcludesInitialization(t *testing.T) {
	opts := fixtureOptions(t)
	db, err := OpenDatabase(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Stat(opts.Config.MasterKeyFile); !os.IsNotExist(err) {
		t.Fatal("database-only operation created a master key")
	}
	if session, err := OpenServices(context.Background(), opts, false); err == nil {
		session.Close()
		t.Fatal("key initialization crossed an existing database lifetime owner")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	services, err := OpenServices(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	defer services.Close()
	if services.Ring == nil || services.Settings == nil || services.Backup.Reg != services.Settings {
		t.Fatal("shared service composition is incomplete")
	}
	if exclusive, err := OpenServices(context.Background(), opts, true); err == nil {
		exclusive.Close()
		t.Fatal("rotation crossed shared lifetime ownership")
	}
}

func TestCanceledOpenReleasesOwnershipWithoutInitializingKey(t *testing.T) {
	opts := fixtureOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenRuntime(ctx, opts); !errors.Is(err, context.Canceled) {
		t.Fatal("opening lost cancellation")
	}
	if _, err := os.Stat(opts.Config.MasterKeyFile); !os.IsNotExist(err) {
		t.Fatal("canceled opener initialized a key")
	}
	owner, err := (&backup.Service{Cfg: opts.Config}).OpenData(true)
	if err != nil {
		t.Fatal("failed open leaked lifetime ownership", err)
	}
	owner.Close()
}
