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
	ui                *terminal.UI
	catalog           sourceCatalog
	run               func(context.Context, []string, io.Reader) error
	overview          func() error
	installed         string
	bootstrapMetadata string
	lifecycleReady    func() error
	view              managerView
	journalOperation  string
	recoveryLineage   string
}

type managerView uint8

const (
	managerFresh managerView = iota
	managerInstalled
	managerInstallRecovery
	managerRecovery
)

type managerMenu struct {
	key         string
	items       []string
	defaultItem int
}

func managerRootMenu(view managerView) managerMenu {
	switch view {
	case managerInstalled:
		return managerMenu{key: "menu", items: []string{"lifecycle", "access", "backups", "operations", "uninstall"}, defaultItem: 1}
	case managerInstallRecovery:
		return managerMenu{key: "setup_recovery_menu", items: []string{"cleanup_install", "readiness"}, defaultItem: 1}
	case managerRecovery:
		return managerMenu{key: "recovery_menu", items: []string{"recover_now", "lifecycle", "access", "backups", "operations"}, defaultItem: 1}
	default:
		return managerMenu{key: "fresh_menu", items: []string{"install_cached", "install_choose", "readiness", "help_short"}, defaultItem: 1}
	}
}

func classifyManagerView(st *install.State, j *install.Journal) managerView {
	pending := j != nil && j.Stage != "complete" && j.Stage != "rolled-back" && j.Stage != "aborted"
	if pending {
		if j.Operation == "install" && j.Before == nil && j.After != nil && !j.DataMayHaveChanged && !j.PrerequisitesComplete && (j.After.Recovery == "" || j.After.Recovery == "install-incomplete") {
			return managerInstallRecovery
		}
		return managerRecovery
	}
	if st != nil {
		return managerInstalled
	}
	return managerFresh
}

func runManage(args []string) error {
	fs := flag.NewFlagSet("manage", flag.ContinueOnError)
	lang := fs.String("lang", terminalLocale(), "terminal UI language (English; fa is a legacy alias)")
	metadata := fs.String("build-metadata", "", "private bootstrap build identity")
	if err := fs.Parse(args); err != nil {
		return err
	}
	locale, ok := terminalLanguage(*lang)
	if fs.NArg() != 0 || !ok {
		return lifecycleArgsError()
	}
	if !terminal.IsTerminal(os.Stdin) {
		return fmt.Errorf("%s", i18n.T(locale, "manage.tty"))
	}
	if os.Getenv("WGG_IN_CONTAINER") == "1" {
		return fmt.Errorf("%s", i18n.T(locale, "manage.host"))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	u := terminal.New(os.Stdin, os.Stdout, terminal.Detect(os.Stdin, os.Stdout, locale))
	u.Context = ctx
	h := install.NewRealHost()
	if *metadata == "" {
		if _, statErr := os.Stat(install.ManagerBuildPath); statErr == nil {
			*metadata = install.ManagerBuildPath
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m := manager{
		ui: u, catalog: distribution.NewClient(nil, distribution.Options{}),
		bootstrapMetadata: *metadata,
		lifecycleReady:    func() error { return install.CheckLifecycleReady(h) },
	}
	m.run = func(ctx context.Context, args []string, in io.Reader) error {
		if args[0] == "backup" || args[0] == "restore" {
			args = append(append([]string{}, args...), "--lang", string(u.Locale))
		}
		if args[0] != "update" || len(args) != 1 {
			u.Text(u.T("manage.working"))
		}
		// Lifecycle commands stay in-process so SIGINT reaches the engine and its
		// independent recovery context; a supervising CommandContext must not kill it.
		switch args[0] {
		case "install":
			return runInstall(args[1:])
		case "recover-install":
			return runRecoverInstallWith(ctx, args[1:], h, os.Stdout)
		case "update":
			return runUpdate(args[1:])
		case "uninstall":
			return runUninstall(args[1:])
		case "restart":
			return runRestart(args[1:])
		case "core":
			return runCoreWithHostContext(ctx, args[1:], h, os.Stdout)
		case "certificate-sync":
			return runCertificateSync(args[1:])
		case "exposure":
			return runExposureWith(ctx, args[1:], h, os.Stdin, os.Stdout)
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
		j, err := install.LoadJournal(h)
		if err != nil {
			return err
		}
		m.view = classifyManagerView(st, j)
		m.installed = ""
		m.journalOperation = ""
		m.recoveryLineage = ""
		if st != nil {
			m.installed = st.Version
			if st.Exposure.Lineage != "" {
				m.recoveryLineage = install.CertbotLivePath(st.Exposure.Lineage)
			}
		}
		if j != nil && j.Stage != "complete" && j.Stage != "rolled-back" && j.Stage != "aborted" {
			m.journalOperation = j.Operation
		}
		u.Header("WG-GUARD", u.T("manage.subtitle"))
		if m.view == managerInstallRecovery {
			mode := ""
			if j != nil && j.After != nil {
				mode = string(j.After.Mode)
			}
			fields := []terminal.StatusField{
				{Label: u.T("manage.build"), Value: version.String()},
				{Label: u.T("manage.installed"), Value: mode + " · " + u.T("manage.setup_incomplete")},
				{Label: u.T("manage.journal"), Value: "install · recovery-required"},
				{Label: u.T("manage.next"), Value: u.T("manage.cleanup_next")},
			}
			u.StatusCard(u.T("manage.action_required"), u.T("manage.setup_recovery_title"), fields)
			return nil
		}
		if st == nil {
			status := u.T("manage.ready")
			title := u.T("manage.fresh_title")
			if m.view == managerRecovery {
				status = u.T("manage.action_required")
				title = u.T("manage.recovery_title")
			}
			fields := []terminal.StatusField{
				{Label: u.T("manage.build"), Value: version.String()},
				{Label: u.T("manage.next"), Value: u.T("manage.fresh_next")},
			}
			if j != nil {
				fields = append(fields, terminal.StatusField{Label: u.T("manage.journal"), Value: j.Operation + " · " + j.Stage})
			}
			u.StatusCard(status, title, fields)
			return nil
		}
		p, err := install.InstalledPlan(h, st)
		if err != nil {
			return err
		}
		url, skip, err := p.HealthProbeURL()
		if err == nil {
			err = install.ProbeHealth(ctx, url, skip)
		}
		status := u.T("manage.healthy")
		health := u.T("manage.healthy")
		if err != nil {
			status = u.T("manage.attention")
			health = u.T("manage.unhealthy")
		}
		if m.view == managerRecovery {
			status = u.T("manage.action_required")
		}
		fields := []terminal.StatusField{
			{Label: u.T("manage.installed"), Value: string(st.Mode) + " · " + st.Version},
			{Label: u.T("manage.panel"), Value: p.PanelURL()},
			{Label: u.T("manage.access"), Value: string(p.Exposure) + " · " + firstNonemptyString(st.TLSReadiness, "unknown")},
			{Label: u.T("manage.health"), Value: health},
			{Label: u.T("manage.core"), Value: st.Core.Requested.ID},
		}
		if j != nil {
			fields = append(fields, terminal.StatusField{Label: u.T("manage.journal"), Value: j.Operation + " · " + j.Stage})
		}
		u.StatusCard(status, u.T("manage.node_title"), fields)
		if st.Core.RebootRequired {
			u.Text(u.T("manage.reboot"))
		}
		if st.Recovery != "" {
			u.Field(u.T("manage.recovery"), st.Recovery)
		}
		return nil
	}
	return m.loop(ctx)
}

func showRecordedReadiness(u *terminal.UI, st *install.State) {
	tls, core := st.TLSReadiness, st.Core.Requested.ID
	if tls == "" {
		tls = u.T("manage.not_recorded")
	}
	if core == "" {
		core = u.T("manage.not_recorded")
	}
	u.Field("TLS", tls)
	u.Field(u.T("manage.core"), core)
}

func terminalLocale() string {
	return "en"
}

// terminalLanguage keeps the old --lang fa spelling non-breaking while the
// terminal product surface is English-only. The web panel remains bilingual.
func terminalLanguage(value string) (i18n.Locale, bool) {
	if value != string(i18n.En) && value != string(i18n.Fa) {
		return i18n.En, false
	}
	return i18n.En, true
}
func (m *manager) menu(key string, items ...string) (int, error) {
	labels := make([]string, len(items))
	for i, v := range items {
		labels[i] = m.ui.T("manage." + v)
	}
	return m.ui.Choose(m.ui.T("manage."+key), labels, 0)
}
func (m *manager) rootMenu(key string, def int, items ...string) (int, error) {
	labels := make([]string, len(items))
	for i, v := range items {
		labels[i] = m.ui.T("manage." + v)
	}
	return m.ui.ChooseRoot(m.ui.T("manage."+key), labels, def)
}
func (m *manager) loop(ctx context.Context) error {
	// Terminal presentation is intentionally English-only. Locale selection
	// remains a web-panel preference, not a host-terminal capability.
	m.ui.Locale = i18n.En
	for {
		if ctx.Err() != nil {
			return terminal.ErrCanceled
		}
		if m.overview != nil {
			if err := m.overview(); err != nil {
				m.ui.Result(err)
			}
		}
		root := managerRootMenu(m.view)
		n, err := m.rootMenu(root.key, root.defaultItem, root.items...)
		if errors.Is(err, terminal.ErrCanceled) || errors.Is(err, terminal.ErrBack) {
			return nil
		}
		if err != nil {
			return err
		}
		err = m.rootAction(ctx, n)
		if errors.Is(err, terminal.ErrCanceled) {
			return err
		}
		if err != nil && !errors.Is(err, terminal.ErrBack) {
			m.ui.Result(err)
		}
	}
}

func (m *manager) rootAction(ctx context.Context, n int) error {
	switch m.view {
	case managerFresh:
		return m.freshAction(ctx, n)
	case managerInstallRecovery:
		switch n {
		case 1:
			_, err := m.reviewedAction(ctx, "cleanup_install_review", []string{"recover-install", "--yes"}, nil)
			return err
		case 2:
			err := m.run(ctx, []string{"doctor"}, nil)
			m.ui.Result(err)
			return nil
		}
	case managerRecovery:
		switch n {
		case 1:
			args := []string{"update", "--recover"}
			if m.journalOperation == "restore" {
				args = []string{"restore", "--recover"}
			} else if m.journalOperation == "certificate" && m.recoveryLineage != "" {
				args = []string{"certificate-sync", "--lineage", m.recoveryLineage}
			} else if m.journalOperation == "exposure" {
				args = []string{"exposure", "recover"}
			}
			_, err := m.reviewedAction(ctx, "recover_review", args, nil)
			return err
		case 2:
			return m.group(ctx, 1)
		case 3:
			return m.group(ctx, 4)
		case 4:
			return m.group(ctx, 3)
		case 5:
			return m.group(ctx, 2)
		}
	case managerInstalled:
		switch n {
		case 1:
			return m.group(ctx, 1)
		case 2:
			return m.group(ctx, 4)
		case 3:
			return m.group(ctx, 3)
		case 4:
			return m.group(ctx, 2)
		case 5:
			executed, err := m.reviewedAction(ctx, "uninstall_review", []string{"uninstall", "--yes"}, nil)
			if err != nil {
				return err
			}
			if executed {
				return terminal.ErrCanceled
			}
			return nil
		}
	}
	return nil
}

func (m *manager) freshAction(ctx context.Context, n int) error {
	switch n {
	case 1, 2:
		var args []string
		if n == 1 && m.bootstrapMetadata != "" {
			args = []string{"install", "--build-metadata", m.bootstrapMetadata, "--lang", string(m.ui.Locale)}
		} else {
			selection, err := pickSource(ctx, m.ui, m.catalog, "")
			if err != nil {
				return err
			}
			args = []string{"install", "--" + selection.Channel, selection.Ref, "--lang", string(m.ui.Locale)}
		}
		err := m.run(ctx, args, nil)
		m.ui.Result(err)
		if ctx.Err() != nil {
			return terminal.ErrCanceled
		}
		return nil
	case 3:
		err := m.run(ctx, []string{"doctor"}, nil)
		m.ui.Result(err)
		return nil
	case 4:
		m.ui.Section(m.ui.T("manage.help_short"))
		m.ui.Text(m.ui.T("manage.help_body"))
	}
	return nil
}

func (m *manager) reviewedAction(ctx context.Context, review string, args []string, input io.Reader) (bool, error) {
	m.ui.Section(m.ui.T("manage.review"))
	ok, err := m.ui.Confirm(m.ui.T("manage." + review))
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	err = m.run(ctx, args, input)
	m.ui.Result(err)
	if ctx.Err() != nil {
		return true, terminal.ErrCanceled
	}
	return err == nil, nil
}

func (m *manager) group(ctx context.Context, group int) error {
	for {
		var n int
		var err error
		switch group {
		case 1:
			n, err = m.menu("lifecycle", "update", "rollback", "recover", "restart")
		case 2:
			n, err = m.menu("operations", "status", "doctor", "tls", "core", "switch")
		case 3:
			n, err = m.ui.Choose(m.ui.T("manage.backups"), []string{m.ui.T("manage.backup_create"), m.ui.T("manage.backup_list"), m.ui.T("manage.restore"), m.ui.T("manage.schedules"), m.ui.T("manage.telegram"), m.ui.T("backup.cli.backup_password"), m.ui.T("backup.cli.recover")}, 0)
		case 4:
			n, err = m.menu("access", "access_status", "access_configure", "access_renew", "access_private", "tls")
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
			case 1:
				if m.lifecycleReady != nil {
					if e := m.lifecycleReady(); e != nil {
						return e
					}
				}
				args = []string{"update"}
			case 2:
				args = []string{"update", "--rollback"}
				review = "rollback_review"
			case 3:
				args = []string{"update", "--recover"}
				review = "recover_review"
			case 4:
				args = []string{"restart", "--yes"}
				review = "restart_review"
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
		case 4:
			switch n {
			case 1:
				args = []string{"exposure", "status"}
			case 2:
				args = []string{"exposure", "configure"}
			case 3:
				args = []string{"exposure", "renew"}
				review = "access_renew_review"
			case 4:
				args = []string{"exposure", "private", "--yes"}
				review = "access_private_review"
			case 5:
				args = []string{"tls-check"}
			}
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
		defaultSource := 0
		if len(releases) > 0 {
			defaultSource = 1
		}
		n, err := u.Choose(u.T("source.title"), labels, defaultSource)
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
			n, err = u.Choose(u.T("source.list"), labels, 1)
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
		ok, err := u.ConfirmDefault(u.T("source.confirm"), true)
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
