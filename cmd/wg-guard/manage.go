package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"github.com/Sir-Adnan/wg-guard/internal/version"
)

type sourceCatalog interface {
	Releases(context.Context) ([]distribution.Release, error)
	Resolve(context.Context, distribution.Selection) (distribution.Build, error)
}
type manager struct {
	ui        *terminal.UI
	catalog   sourceCatalog
	run       func(context.Context, []string, io.Reader) error
	overview  func() error
	installed string
}

func runManage(args []string) error {
	fs := flag.NewFlagSet("manage", flag.ContinueOnError)
	lang := fs.String("lang", terminalLocale(), "fa | en")
	metadata := fs.String("build-metadata", "", "private bootstrap build identity")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !i18n.Locale(*lang).Valid() {
		return lifecycleArgsError()
	}
	if !terminal.IsTerminal(os.Stdin) {
		return fmt.Errorf("%s", i18n.T(i18n.Locale(*lang), "manage.tty"))
	}
	if os.Getenv("WGG_IN_CONTAINER") == "1" {
		return fmt.Errorf("%s", i18n.T(i18n.Locale(*lang), "manage.host"))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	u := terminal.New(os.Stdin, os.Stdout, terminal.Detect(os.Stdin, os.Stdout, i18n.Locale(*lang)))
	u.Context = ctx
	h := install.NewRealHost()
	st, err := install.LoadState(h)
	if err != nil {
		return err
	}
	if setup := managementSetup(st, *metadata, *lang); setup != nil {
		return runInstall(setup)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m := manager{ui: u, catalog: distribution.NewClient(nil, distribution.Options{})}
	m.run = func(ctx context.Context, args []string, in io.Reader) error {
		if args[0] == "backup" || args[0] == "restore" {
			args = append(append([]string{}, args...), "--lang", string(u.Locale))
		}
		u.Text(u.T("manage.working"))
		// Lifecycle commands stay in-process so SIGINT reaches the engine and its
		// independent recovery context; a supervising CommandContext must not kill it.
		switch args[0] {
		case "install":
			return runInstall(args[1:])
		case "update":
			return runUpdate(args[1:])
		case "uninstall":
			return runUninstall(args[1:])
		case "restart":
			return runRestart(args[1:])
		case "core":
			return runCoreWithHostContext(ctx, args[1:], h, os.Stdout)
		case "restore":
			if in == nil {
				in = os.Stdin
			}
			return runRestoreWith(ctx, args[1:], in, os.Stdout, h)
		}
		argv := append([]string{exe}, args...)
		if in != nil {
			return h.RunWithInput(ctx, argv, in, 30*time.Minute)
		}
		return h.Run(ctx, argv, 30*time.Minute)
	}
	m.overview = func() error {
		st, err := install.LoadState(h)
		if err != nil {
			return err
		}
		u.Section("WG-Guard")
		u.Text(u.T("manage.subtitle"))
		u.Field(u.T("manage.build"), version.String())
		if st == nil {
			u.Text(u.T("manage.absent"))
			return nil
		}
		m.installed = st.Version
		u.Field(u.T("manage.installed"), string(st.Mode)+" · "+st.Version)
		u.Field("TLS", st.TLSReadiness)
		u.Field(u.T("manage.core"), st.Core.Requested.ID)
		if st.Core.RebootRequired {
			u.Text(u.T("manage.reboot"))
		}
		if st.Recovery != "" {
			u.Field(u.T("manage.recovery"), st.Recovery)
		}
		cfg, err := install.ReadBootConfig(h, st.ConfigPath)
		if err != nil {
			return err
		}
		p := install.Defaults()
		p.TLSMode = cfg.TLS.Mode
		p.Domain = cfg.TLS.Domain
		p.PublicIP = st.PublicIP
		p.ACMEHTTPPort = cfg.TLS.ACMEHTTPPort
		_, p.PanelPort, err = splitListen(cfg.HTTPListen)
		if err != nil {
			return err
		}
		u.Field(u.T("manage.panel"), p.PanelURL())
		u.Field(u.T("manage.endpoint"), p.VPNEndpoint())
		url, skip, err := p.HealthProbeURL()
		if err == nil {
			err = install.ProbeHealth(ctx, url, skip)
		}
		health := u.T("manage.healthy")
		if err != nil {
			health = u.T("manage.unhealthy")
		}
		u.Field(u.T("manage.health"), health)
		j, err := install.LoadJournal(h)
		if err != nil {
			return err
		}
		if j != nil {
			u.Field(u.T("manage.journal"), j.Operation+" · "+j.Stage)
		}
		return nil
	}
	return m.loop(ctx)
}

// Only the acquisition entry auto-starts fresh setup. An installed node opens
// management without reinstalling or implicitly adopting the acquired build.
func managementSetup(st *install.State, metadata, locale string) []string {
	if st != nil || metadata == "" {
		return nil
	}
	return []string{"--build-metadata", metadata, "--lang", locale}
}
func terminalLocale() string {
	if v := os.Getenv("WGG_LANG"); v == "fa" || v == "en" {
		return v
	}
	if strings.HasPrefix(os.Getenv("LANG"), "fa") {
		return "fa"
	}
	return "en"
}
func (m *manager) menu(key string, items ...string) (int, error) {
	labels := make([]string, len(items))
	for i, v := range items {
		labels[i] = m.ui.T("manage." + v)
	}
	return m.ui.Choose(m.ui.T("manage."+key), labels, 0)
}
func (m *manager) loop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return terminal.ErrCanceled
		}
		if m.overview != nil {
			if err := m.overview(); err != nil {
				m.ui.Result(err)
			}
		}
		n, err := m.menu("menu", "lifecycle", "operations", "backups", "language")
		if errors.Is(err, terminal.ErrCanceled) || errors.Is(err, terminal.ErrBack) {
			return nil
		}
		if err != nil {
			return err
		}
		if n == 4 {
			if m.ui.Locale == i18n.En {
				m.ui.Locale = i18n.Fa
			} else {
				m.ui.Locale = i18n.En
			}
			continue
		}
		err = m.group(ctx, n)
		if errors.Is(err, terminal.ErrCanceled) {
			return err
		}
		if err != nil && !errors.Is(err, terminal.ErrBack) {
			m.ui.Result(err)
		}
	}
}
func (m *manager) group(ctx context.Context, group int) error {
	for {
		var n int
		var err error
		switch group {
		case 1:
			n, err = m.menu("lifecycle", "install", "update", "rollback", "recover", "uninstall")
		case 2:
			n, err = m.menu("operations", "status", "doctor", "tls", "core", "switch", "restart")
		case 3:
			n, err = m.ui.Choose(m.ui.T("manage.backups"), []string{m.ui.T("manage.backup_create"), m.ui.T("manage.backup_list"), m.ui.T("manage.restore"), m.ui.T("manage.schedules"), m.ui.T("manage.telegram"), m.ui.T("backup.cli.backup_password"), m.ui.T("backup.cli.recover")}, 0)
		}
		if err != nil {
			return err
		}
		var args []string
		var input io.Reader
		review := ""
		switch group {
		case 1:
			switch n {
			case 1, 2:
				selection, e := pickSource(ctx, m.ui, m.catalog, m.installed)
				if errors.Is(e, terminal.ErrBack) {
					continue
				}
				if e != nil {
					return e
				}
				cmd := "install"
				if n == 2 {
					cmd = "update"
					review = "update_review"
				}
				args = []string{cmd, "--" + selection.Channel, selection.Ref}
				if n == 1 {
					args = append(args, "--lang", string(m.ui.Locale))
				}
			case 3:
				args = []string{"update", "--rollback"}
				review = "rollback_review"
			case 4:
				args = []string{"update", "--recover"}
				review = "recover_review"
			case 5:
				args = []string{"uninstall", "--yes"}
				review = "uninstall_review"
			}
		case 2:
			switch n {
			case 1:
				args = []string{"status"}
			case 2:
				args = []string{"doctor"}
			case 3:
				args = []string{"tls-check"}
			case 4:
				choice, e := m.menu("core", "core_installed", "core_recommended", "core_latest")
				if e != nil {
					return e
				}
				args = []string{"core", []string{"installed", "recommended", "latest-compatible"}[choice-1]}
			case 5:
				if err := m.run(ctx, []string{"core", "recommended"}, nil); err != nil {
					return err
				}
				args = []string{"core", "switch", "recommended", "--confirm-impact"}
				review = "core_review"
			case 6:
				args = []string{"restart", "--yes"}
				review = "restart_review"
			}
		case 3:
			err = m.backupAction(ctx, n)
			if errors.Is(err, terminal.ErrCanceled) {
				return err
			}
			if err != nil && !errors.Is(err, terminal.ErrBack) {
				m.ui.Result(err)
			}
			continue
		}
		if review != "" {
			m.ui.Section(m.ui.T("manage.review"))
			ok, e := m.ui.Confirm(m.ui.T("manage." + review))
			if e != nil {
				return e
			}
			if !ok {
				continue
			}
		}
		err = m.run(ctx, args, input)
		m.ui.Result(err)
		if ctx.Err() != nil {
			return terminal.ErrCanceled
		}
		// Uninstall removes the host executable: don't present further actions.
		if group == 1 && n == 5 && err == nil {
			return terminal.ErrCanceled
		}
	}
}

func pickSource(ctx context.Context, u *terminal.UI, c sourceCatalog, installed string) (distribution.Selection, error) {
	u.Field(u.T("manage.installed"), installed)
	networkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	releases, err := c.Releases(networkCtx)
	cancel()
	if err != nil {
		return distribution.Selection{}, err
	}
	for {
		labels := []string{u.T("source.latest"), u.T("source.list"), u.T("source.main"), u.T("source.sha")}
		if len(releases) > 0 {
			labels[0] += " · " + releases[0].Tag + " · " + releases[0].PublishedAt
		} else {
			u.Text(u.T("source.empty"))
		}
		n, err := u.Choose(u.T("source.title"), labels, 0)
		if err != nil {
			return distribution.Selection{}, err
		}
		var selection distribution.Selection
		switch n {
		case 1:
			if len(releases) == 0 {
				continue
			}
			selection = distribution.Selection{Channel: "release", Ref: releases[0].Tag}
		case 2:
			if len(releases) == 0 {
				continue
			}
			labels = labels[:0]
			for _, r := range releases {
				labels = append(labels, r.Tag+" · "+r.PublishedAt)
			}
			n, err = u.Choose(u.T("source.list"), labels, 0)
			if errors.Is(err, terminal.ErrBack) {
				continue
			}
			if err != nil {
				return selection, err
			}
			selection = distribution.Selection{Channel: "release", Ref: releases[n-1].Tag}
		case 3:
			selection = distribution.Selection{Channel: "commit", Ref: "main"}
		case 4:
			ref, e := u.Ask(u.T("source.sha"), "")
			if e != nil {
				return selection, e
			}
			b, e := hex.DecodeString(ref)
			if e != nil || len(b) != 20 || ref != strings.ToLower(ref) {
				u.Text(u.T("source.sha"))
				continue
			}
			selection = distribution.Selection{Channel: "commit", Ref: ref}
		}
		networkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		build, err := c.Resolve(networkCtx, selection)
		cancel()
		if err != nil {
			return selection, err
		}
		u.Section(u.T("manage.review"))
		u.Field(u.T("manage.build"), build.Version)
		u.Field("Commit", build.Commit)
		if selection.Channel == "commit" {
			selection.Ref = build.Commit
			u.Text(u.T("source.development"))
		}
		ok, err := u.Confirm(u.T("source.confirm"))
		if err != nil {
			return selection, err
		}
		if ok {
			return selection, nil
		}
	}
}

func runRestart(args []string) error {
	fs := flag.NewFlagSet("restart", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm restart")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return lifecycleArgsError()
	}
	if !*yes {
		u := terminal.New(os.Stdin, os.Stdout, terminal.Detect(os.Stdin, os.Stdout, i18n.Locale(terminalLocale())))
		ok, err := u.Confirm(u.T("manage.restart_review"))
		if err != nil {
			return err
		}
		if !ok {
			return terminal.ErrCanceled
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return install.Restart(ctx, install.NewRealHost())
}
