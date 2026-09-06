package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"io"
	"os"
	"os/signal"
	"strings"
)

func runRestore(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return runRestoreWith(ctx, args, os.Stdin, os.Stdout, install.NewRealHost())
}
func runRestoreWith(ctx context.Context, args []string, in io.Reader, out io.Writer, h install.Host) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lang := fs.String("lang", terminalLocale(), "")
	configPath := fs.String("config", install.ConfigPath, "")
	passwordFile := fs.String("password-file", "", "")
	pw := fs.Bool("password", false, "")
	yes := fs.Bool("yes", false, "")
	recover := fs.Bool("recover", false, "")
	retry := fs.Bool("retry", false, "")
	archive := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		archive = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *pw && *passwordFile != "" || *recover && archive != "" || !*recover && archive == "" {
		return fmt.Errorf("%s", backupText("restore_usage"))
	}
	if os.Getenv("WGG_IN_CONTAINER") == "1" {
		return fmt.Errorf("%s", backupText("host"))
	}
	if !i18n.Locale(*lang).Valid() {
		return lifecycleArgsError()
	}
	printer := backupPrinter{i18n.Locale(*lang)}
	u := terminal.New(os.Stdin, out, terminal.Detect(os.Stdin, out, printer.locale))
	u.Context = ctx
	var svc *backup.Service
	var preview *backup.PendingRestore
	defer func() {
		if svc != nil && preview != nil {
			_ = svc.DiscardPreview(preview.PreviewID())
		}
	}()
	return install.Restore(ctx, h, install.RestoreOptions{Recover: *recover, Retry: *retry, Prepare: func(ctx context.Context, id *install.BackupIdentity) (func(context.Context) error, error) {
		cfg, err := install.ReadBootConfig(h, *configPath)
		if err != nil {
			return nil, err
		}
		if *configPath != install.ConfigPath {
			return nil, fmt.Errorf("%s", printer.text("layout"))
		}
		svc = &backup.Service{Cfg: cfg, ConfigPath: *configPath}
		password := ""
		if *passwordFile != "" {
			password, err = install.ReadProtectedPassword(h, *passwordFile)
		} else if *pw {
			secretUI := terminal.New(in, out, terminal.Detect(in, out, u.Locale))
			secretUI.Context = ctx
			password, err = secretUI.Secret(printer.text("password"))
		}
		if err != nil {
			return nil, err
		}
		var report *backup.RestoreReport
		if id != nil {
			archive = id.Path
			preview, report, err = svc.StageRecovery(ctx, archive, password, id.SHA256, id.Encrypted)
		} else {
			preview, report, err = svc.Stage(ctx, archive, password)
		}
		if err != nil {
			return nil, err
		}
		u.Section(printer.text("review"))
		u.Field(printer.text("archive"), preview.Archive)
		u.Field(printer.text("source"), report.Hostname+" · "+report.AppVersion)
		u.Field(printer.text("node"), report.NodeID)
		u.Field(printer.text("endpoint"), report.Endpoint)
		u.Field("TLS", report.TLSMode+" · "+report.Listen)
		for _, f := range report.Interfaces {
			u.Text(fmt.Sprintf("%s · %d · %s", f.Name, f.Port, f.Subnet))
		}
		for _, w := range report.Warnings {
			u.Text(printer.text("warning", w))
		}
		u.Text(printer.text("config_review"))
		if id != nil {
			u.Text(printer.text("original"))
		}
		if !*yes {
			ok, err := u.Confirm(printer.text("apply"))
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, terminal.ErrCanceled
			}
		}
		return func(ctx context.Context) error {
			if err := svc.RecoverInterrupted(); err != nil {
				return err
			}
			var err error
			if id != nil {
				_, err = svc.ApplyOriginal(ctx, preview.PreviewID())
			} else {
				if _, err = svc.Approve(preview.PreviewID()); err == nil {
					_, err = svc.ApplyStaged(ctx)
				}
			}
			if err == nil {
				u.Text(printer.text("restored"))
			}
			return err
		}, nil
	}})
}
