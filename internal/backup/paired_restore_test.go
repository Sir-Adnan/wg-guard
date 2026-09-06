package backup

import (
	"context"
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"os"
	"path/filepath"
	"testing"
)

type interruptApply struct {
	context.Context
	calls int
	crash bool
}

func (c *interruptApply) Err() error {
	c.calls++
	if c.calls == 3 {
		if c.crash {
			panic("synthetic process death after DB replacement")
		}
		return context.Canceled
	}
	return nil
}

func TestRestoreRecoversDatabaseKeyPairAfterReplacementInterruption(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "crash"}[crash], func(t *testing.T) {
			s, _ := newService(t)
			ctx := context.Background()
			arc, err := s.Create(ctx, CreateOpts{})
			if err != nil {
				t.Fatal(err)
			}
			p, _, err := s.Stage(ctx, arc.Path, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Approve(p.PreviewID()); err != nil {
				t.Fatal(err)
			}
			if err := s.Reg.SetRaw(ctx, "node.id", "after-archive"); err != nil {
				t.Fatal(err)
			}
			s.DB.Close()
			if err := os.WriteFile(s.Cfg.MasterKeyFile, []byte("different-current-key-32-bytes!!!"), 0600); err != nil {
				t.Fatal(err)
			}
			beforeDB := fileHash(s.Cfg.DatabasePath)
			beforeKey := fileHash(s.Cfg.MasterKeyFile)
			func() {
				defer func() {
					if v := recover(); v != nil && !crash {
						t.Fatal(v)
					}
				}()
				_, err = s.ApplyStaged(&interruptApply{Context: ctx, crash: crash})
				if !crash && !errors.Is(err, context.Canceled) {
					t.Fatal("expected interrupted replacement")
				}
			}()
			if crash {
				if _, err := s.ConsumePendingRestore(); err == nil {
					t.Fatal("boot continued after interrupted restore")
				}
			}
			if fileHash(s.Cfg.DatabasePath) != beforeDB || fileHash(s.Cfg.MasterKeyFile) != beforeKey {
				t.Fatal("old database/key pair was not recovered exactly")
			}
			if p, _ := s.Pending(); p != nil {
				t.Fatal("failed approved restore can replay at boot")
			}
		})
	}
}

func TestApplyRequiresCompleteMetadata(t *testing.T) {
	for _, kind := range []string{"absent", "empty", "missing-db", "traversal"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := newService(t)
			ctx := context.Background()
			a, err := s.Create(ctx, CreateOpts{})
			if err != nil {
				t.Fatal(err)
			}
			p, _, err := s.Stage(ctx, a.Path, "")
			if err != nil {
				t.Fatal(err)
			}
			p, err = s.Approve(p.PreviewID())
			if err != nil {
				t.Fatal(err)
			}
			s.DB.Close()
			before := fileHash(s.Cfg.DatabasePath)
			meta := filepath.Join(p.Dir, pendingMeta)
			switch kind {
			case "absent":
				os.Remove(meta)
			case "empty":
				os.WriteFile(meta, []byte(`{}`), 0600)
			case "missing-db":
				os.Remove(filepath.Join(p.Dir, DBMember))
			case "traversal":
				os.WriteFile(meta, []byte(`{"files":{"../outside":"bad"}}`), 0600)
			}
			if _, err := s.ApplyStaged(ctx); err == nil {
				t.Fatal("invalid metadata accepted")
			}
			if _, err := s.ConsumePendingRestore(); err == nil {
				t.Fatal("invalid pending metadata allowed boot")
			}
			if fileHash(s.Cfg.DatabasePath) != before {
				t.Fatal("active database changed before verification")
			}
		})
	}
}

func TestOriginalSchemaRecoveryDoesNotForwardMigrate(t *testing.T) {
	s, _ := newLegacyServiceThrough0006(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{Password: "synthetic-recovery-password"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()
	p, _, err := s.StageRecovery(ctx, a.Path, "synthetic-recovery-password", fileHash(a.Path), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(p.PreviewID()); err == nil {
		t.Fatal("original schema queued for automatic forward-migrating boot")
	}
	if _, err := s.ApplyOriginal(ctx, p.PreviewID()); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(s.Cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM migrations WHERE version='0007_awg_ranges.sql'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("recovery returned migrated data to old code")
	}
}
