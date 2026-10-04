// Package nodestate owns the live DB/key opening sequence. Request/CLI parsing
// supplies a resolved boot configuration; this package owns data admission,
// required pre-migration recovery, key initialization and settings composition.
// It performs no service-manager, network or host lifecycle operations.
package nodestate

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

type Options struct {
	Config     *config.Config
	ConfigPath string
	Version    string
	Log        *slog.Logger
}

// Session retains ownership until every DB/key consumer is drained. Close is
// retryable and serial: a failed database close does not release the data lease.
type Session struct {
	DB              *database.DB
	Ring            *secrets.KeyRing
	Settings        *settings.Registry
	Backup          *backup.Service
	Lease           *backup.DataLease
	RestoredArchive string
	mu              sync.Mutex
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DB != nil {
		if err := s.DB.Close(); err != nil {
			return err
		}
	}
	s.Lease.Close()
	return nil
}

type access uint8

const (
	databaseOnly access = iota
	sharedServices
	exclusiveServices
	runtimeStartup
)

// OpenDatabase never creates key material. Owner/token bootstrapping needs only DB access.
func OpenDatabase(ctx context.Context, o Options) (*Session, error) {
	return open(ctx, o, databaseOnly)
}

// OpenServices composes key/settings services. Exclusive is reserved for stopped-node
// operations such as rotation; callers retain that ownership through their operation.
func OpenServices(ctx context.Context, o Options, exclusive bool) (*Session, error) {
	mode := sharedServices
	if exclusive {
		mode = exclusiveServices
	}
	return open(ctx, o, mode)
}

// OpenRuntime consumes an approved pending restore before opening any live handle.
func OpenRuntime(ctx context.Context, o Options) (*Session, error) {
	return open(ctx, o, runtimeStartup)
}

func open(ctx context.Context, o Options, mode access) (*Session, error) {
	if o.Config == nil {
		return nil, fmt.Errorf("nodestate: boot configuration required")
	}
	s := &Session{Backup: &backup.Service{Cfg: o.Config, ConfigPath: o.ConfigPath, Version: o.Version, Log: o.Log}}
	var err error
	switch mode {
	case runtimeStartup:
		s.RestoredArchive, s.Lease, err = s.Backup.PrepareOpen()
	case databaseOnly:
		s.Lease, err = s.Backup.OpenData(false)
	default:
		s.Lease, err = s.Backup.OpenKeys(mode == exclusiveServices)
	}
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = s.Close()
		}
	}()
	s.DB, err = database.Open(o.Config.DatabasePath, database.Options{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s.Backup.DB = s.DB
	if err := s.Backup.MigrateNode(ctx, s.Lease); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if mode != databaseOnly {
		s.Ring, err = secrets.LoadNodeKeyRing(ctx, s.DB.DB, o.Config.MasterKeyFile)
		if err != nil {
			return nil, err
		}
		s.Settings, err = settings.New(s.DB, s.Ring, settings.Defaults())
		if err != nil {
			return nil, err
		}
		s.Backup.Reg = s.Settings
	}
	if mode != exclusiveServices {
		if err := s.Lease.Share(); err != nil {
			return nil, err
		}
	}
	ok = true
	return s, nil
}
