package main

import (
	"context"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRotationProcessCannotOutliveRestoredPair(t *testing.T) {
	if path := os.Getenv("WGG_TEST_ROTATION_CONFIG"); path != "" {
		if err := runSecrets([]string{"rotate", "--config", path}); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, kill := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirm", true: "process-death"}[kill], func(t *testing.T) {
			sourcePath := testTokenConfig(t)
			source, err := loadCLIEnv(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			if err := source.Reg.SetRaw(context.Background(), "backup.telegram_token", "synthetic-restored-token"); err != nil {
				t.Fatal(err)
			}
			arc, err := source.newBackupService().Create(context.Background(), backup.CreateOpts{})
			if err != nil {
				t.Fatal(err)
			}
			path := testTokenConfig(t)
			env, err := loadCLIEnv(path)
			if err != nil {
				t.Fatal(err)
			}
			env.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRotationProcessCannotOutliveRestoredPair$")
			cmd.Env = append(os.Environ(), "WGG_TEST_ROTATION_CONFIG="+path)
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr strings.Builder
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			var prompt strings.Builder
			for !strings.Contains(prompt.String(), "Type YES to confirm: ") && prompt.Len() < 8192 {
				var b [1]byte
				if _, err := io.ReadFull(output, b[:]); err != nil {
					t.Fatalf("rotation prompt: %v %s", err, stderr.String())
				}
				prompt.WriteByte(b[0])
			}
			if !strings.Contains(prompt.String(), "Type YES to confirm: ") {
				t.Fatal("rotation never reached confirmation")
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			svc := &backup.Service{Cfg: cfg}
			preview, _, err := svc.Stage(ctx, arc.Path, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Approve(preview.PreviewID()); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ApplyStaged(ctx); err == nil {
				t.Fatal("restore accepted a paused, admitted rotation")
			}
			if _, err := loadCLIEnv(path); err == nil {
				t.Fatal("exclusive rotation admitted another DB/key reader")
			}
			if kill {
				cmd.Process.Kill()
			} else {
				io.WriteString(input, "YES\n")
			}
			input.Close()
			io.Copy(io.Discard, output)
			if err := cmd.Wait(); err != nil && !kill {
				t.Fatalf("rotation: %v %s", err, stderr.String())
			}
			if _, err := svc.ApplyStaged(ctx); err != nil {
				t.Fatal("restore after rotation exit", err)
			}
			restored, err := loadCLIEnv(path)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			got, err := restored.Reg.GetSecret(ctx, "backup.telegram_token")
			if err != nil || got != "synthetic-restored-token" {
				t.Fatal("restored DB/key correspondence lost", err)
			}
		})
	}
}

func TestManualOpenersHoldAndReleaseDataOwnership(t *testing.T) {
	for _, command := range []string{"backup", "owner", "token"} {
		t.Run(command, func(t *testing.T) {
			path := testTokenConfig(t)
			var closeData func()
			switch command {
			case "backup":
				e, err := loadCLIEnv(path)
				if err != nil {
					t.Fatal(err)
				}
				closeData = e.Close
			case "owner":
				_, close, err := loadOwnerService(path)
				if err != nil {
					t.Fatal(err)
				}
				closeData = close
			case "token":
				_, close, err := openForToken(path)
				if err != nil {
					t.Fatal(err)
				}
				closeData = close
			}
			defer closeData()
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			s := &backup.Service{Cfg: cfg}
			if lease, err := s.OpenData(true); err == nil {
				lease.Close()
				t.Fatal("opener released lifetime ownership early")
			}
			closeData()
			lease, err := s.OpenData(true)
			if err != nil {
				t.Fatal("opener leaked ownership", err)
			}
			lease.Close()
		})
	}
}
