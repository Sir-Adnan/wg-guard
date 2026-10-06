//go:build ignore

// This acceptance helper verifies/stages an archive in a separate private work
// directory. It never approves/applies a payload or opens the live node pair.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
)

func main() {
	archive := flag.String("archive", "", "archive path")
	passwordFile := flag.String("password-file", "", "private password file")
	work := flag.String("work", "", "separate root-private staging directory")
	flag.Parse()
	if err := stage(*archive, *passwordFile, *work); err != nil {
		fmt.Fprintln(os.Stderr, "phase20-stage: archive staging failed")
		os.Exit(1)
	}
}

func stage(archive, passwordFile, work string) error {
	work, err := filepath.Abs(work)
	if err != nil || filepath.Dir(work) != "/root" || !strings.HasPrefix(filepath.Base(work), "wgg-phase20-stage-") {
		return fmt.Errorf("work directory refused")
	}
	if err := os.Mkdir(work, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(passwordFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return fmt.Errorf("password file refused")
	}
	password, err := os.ReadFile(passwordFile)
	if err != nil {
		return err
	}
	defer clear(password)
	cfg := config.Defaults()
	cfg.DataDir, cfg.DatabasePath, cfg.MasterKeyFile = work, filepath.Join(work, "wg-guard.db"), filepath.Join(work, "master.key")
	svc := &backup.Service{Cfg: cfg}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	preview, _, err := svc.StageOriginal(ctx, archive, strings.TrimSpace(string(password)))
	if err != nil {
		return err
	}
	fmt.Println(filepath.Join(preview.Dir, backup.DBMember))
	return nil
}
