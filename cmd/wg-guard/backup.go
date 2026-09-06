package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/version"
)

// cliEnv is everything the ops commands (backup/restore/settings) share:
// boot config, open database, key ring, settings registry.
type cliEnv struct {
	Cfg        *config.Config
	ConfigPath string
	DB         *database.DB
	Reg        *settings.Registry
	Ring       *secrets.KeyRing
	lease      *backup.DataLease
}

// loadCLIEnv loads the boot config and opens the node state. It is the
// ops-command counterpart of runReconcile's setup.
func loadCLIEnv(configPath string) (*cliEnv, error) {
	return loadCLIEnvOwnership(configPath, false)
}

func loadCLIEnvOwnership(configPath string, exclusive bool) (*cliEnv, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	lease, err := (&backup.Service{Cfg: cfg}).OpenKeys(exclusive)
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			lease.Close()
		}
	}()
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := db.Migrate(context.Background(), quiet); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	ring, err := secrets.LoadKeyRing(cfg.MasterKeyFile)
	if err != nil {
		db.Close()
		return nil, err
	}
	if !exclusive {
		if err := lease.Share(); err != nil {
			db.Close()
			return nil, err
		}
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		db.Close()
		return nil, err
	}
	owned = true
	return &cliEnv{Cfg: cfg, ConfigPath: configPath, DB: db, Reg: reg, Ring: ring, lease: lease}, nil
}

func (e *cliEnv) Close() { e.DB.Close(); e.lease.Close() }

// newBackupService builds the archive engine over the CLI environment.
func (e *cliEnv) newBackupService() *backup.Service {
	return &backup.Service{
		DB: e.DB, Reg: e.Reg, Audit: audit.NewService(e.DB),
		Cfg: e.Cfg, ConfigPath: e.ConfigPath, Version: version.String(),
	}
}
