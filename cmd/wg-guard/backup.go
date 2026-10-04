package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/logsafe"
	"github.com/Sir-Adnan/wg-guard/internal/nodestate"
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
	state      *nodestate.Session
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
	quiet := logsafe.WithComponent(
		slog.New(logsafe.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))),
		logsafe.ComponentBackup,
	)
	state, err := nodestate.OpenServices(context.Background(), nodestate.Options{Config: cfg, ConfigPath: configPath, Version: version.String(), Log: quiet}, exclusive)
	if err != nil {
		return nil, err
	}
	return &cliEnv{Cfg: cfg, ConfigPath: configPath, DB: state.DB, Reg: state.Settings, Ring: state.Ring, state: state}, nil
}

func (e *cliEnv) Close() { _ = e.state.Close() }

// newBackupService builds the archive engine over the CLI environment.
func (e *cliEnv) newBackupService() *backup.Service {
	return &backup.Service{
		DB: e.DB, Reg: e.Reg, Audit: audit.NewService(e.DB),
		Cfg: e.Cfg, ConfigPath: e.ConfigPath, Version: version.String(),
	}
}
