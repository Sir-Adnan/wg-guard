package main

import (
	"bytes"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryManualOpenerRefusesRecoveryMarkersBeforeDatabaseAccess(t *testing.T) {
	for _, marker := range []string{backup.RestoreGuardName, "restore.transaction"} {
		for _, command := range []string{"backup", "token", "owner", "reconcile"} {
			t.Run(marker+"/"+command, func(t *testing.T) {
				path := testTokenConfig(t)
				cfg, err := config.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				original := []byte("not sqlite: opening this file must not be attempted")
				if err := os.WriteFile(cfg.DatabasePath, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cfg.DataDir, marker), []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
				switch command {
				case "backup":
					_, err = loadCLIEnv(path)
				case "token":
					_, _, err = openForToken(path)
				case "owner":
					_, _, err = loadOwnerService(path)
				case "reconcile":
					err = runReconcile([]string{"--config", path})
				}
				if err == nil || !strings.Contains(err.Error(), "offline restore") {
					t.Fatalf("opener did not refuse recovery marker: %v", err)
				}
				got, err := os.ReadFile(cfg.DatabasePath)
				if err != nil || !bytes.Equal(got, original) {
					t.Fatal("active DB mutated")
				}
				for _, suffix := range []string{"-wal", "-shm"} {
					if _, err := os.Stat(cfg.DatabasePath + suffix); !os.IsNotExist(err) {
						t.Fatal("SQLite sidecar created")
					}
				}
			})
		}
	}
}
