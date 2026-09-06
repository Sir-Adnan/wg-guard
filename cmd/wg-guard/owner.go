package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

// runOwnerBootstrap is host-local even for Docker: the installer uses the same
// bind-mounted data and shared admin service before the container is started.
func runOwnerBootstrap(args []string) error {
	fs := flag.NewFlagSet("owner-bootstrap", flag.ContinueOnError)
	cfg := fs.String("config", install.ConfigPath, "boot config")
	check := fs.Bool("check", false, "report present or absent")
	stdin := fs.Bool("stdin", false, "read bounded owner JSON from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *check == *stdin {
		return lifecycleArgsError()
	}
	if !install.NewRealHost().IsRoot() {
		return fmt.Errorf("%s", i18n.T(i18n.En, "manage.root"))
	}
	svc, close, err := loadOwnerService(*cfg)
	if err != nil {
		return fmt.Errorf("%s", i18n.T(i18n.En, "owner.check_failed"))
	}
	defer close()
	if *check {
		has, err := svc.HasOwner(context.Background())
		if err != nil {
			return fmt.Errorf("%s", i18n.T(i18n.En, "owner.check_failed"))
		}
		if has {
			fmt.Println("present")
		} else {
			fmt.Println("absent")
		}
		return nil
	}
	return bootstrapOwnerInput(context.Background(), svc, os.Stdin, os.Stdout)
}

// Owner preparation must target exactly the generated service config. Shell
// WGG_DATABASE_PATH / WGG_DATA_DIR overrides may differ from Docker/systemd's
// environment and must never redirect the first-owner guarantee to another DB.
func loadOwnerService(path string) (*admin.Service, func(), error) {
	cfg, err := install.ReadBootConfig(install.NewRealHost(), path)
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if err := (&backup.Service{Cfg: cfg}).CheckOpen(); err != nil {
		return nil, nil, err
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		return nil, nil, err
	}
	if err := db.Migrate(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		db.Close()
		return nil, nil, err
	}
	return admin.NewService(db, nil), func() { db.Close() }, nil
}
func bootstrapOwnerInput(ctx context.Context, svc *admin.Service, in io.Reader, out io.Writer) error {
	fail := func() error { return fmt.Errorf("%s", i18n.T(i18n.En, "owner.failed")) }
	b, err := io.ReadAll(io.LimitReader(in, 16385))
	if err != nil || len(b) > 16384 {
		return fail()
	}
	defer clear(b)
	var input install.OwnerInput
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || len(input.Password) > 4096 || dec.Decode(new(any)) != io.EOF {
		return fail()
	}
	if _, err := svc.BootstrapOwner(ctx, input.Username, input.Password); err != nil {
		return fail()
	}
	fmt.Fprintln(out, "ready")
	return nil
}
