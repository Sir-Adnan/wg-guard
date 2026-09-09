package main

import (
	"context"
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
)

type managerUpdateOptions struct {
	Selection distribution.Selection
	Refresh   bool
}

type coreUpdateOptions struct {
	Bundle string
	Yes    bool
}

type allUpdateOptions struct {
	Selection distribution.Selection
	Bundle    string
	Yes       bool
}

func classifyUpdateCommand(args []string, interactive bool) (string, []string, error) {
	if len(args) == 0 {
		if interactive {
			return "menu", nil, nil
		}
		return "panel", nil, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return "panel", args, nil
	}
	switch args[0] {
	case "menu", "manager", "panel", "core", "all", "status":
		var rest []string
		if len(args) > 1 {
			rest = args[1:]
		}
		return args[0], rest, nil
	default:
		return "", nil, lifecycleArgsError()
	}
}

func updateSourceFlags(name string, args []string, extra func(*flag.FlagSet)) (distribution.Selection, *flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	release := fs.String("release", "", "stable release tag or latest")
	commit := fs.String("commit", "", "main or an immutable full commit")
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return distribution.Selection{}, fs, lifecycleArgsError()
	}
	selection, err := sourceSelection(*release, *commit)
	if err != nil {
		return distribution.Selection{}, fs, err
	}
	if selection.Channel == "" {
		selection = distribution.Selection{Channel: "release", Ref: "latest"}
	}
	return selection, fs, nil
}

func parseManagerUpdateOptions(args []string) (managerUpdateOptions, error) {
	var refresh *bool
	selection, _, err := updateSourceFlags("update manager", args, func(fs *flag.FlagSet) {
		refresh = fs.Bool("refresh", false, "reacquire even when the selected manager is current")
	})
	if err != nil {
		return managerUpdateOptions{}, err
	}
	return managerUpdateOptions{Selection: selection, Refresh: *refresh}, nil
}

func parseCoreUpdateOptions(args []string) (coreUpdateOptions, error) {
	fs := flag.NewFlagSet("update core", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bundle := fs.String("bundle", "recommended", "reviewed compatible core bundle")
	yes := fs.Bool("yes", false, "confirm maintenance impact")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return coreUpdateOptions{}, lifecycleArgsError()
	}
	if _, err := install.SelectCore(*bundle); err != nil {
		return coreUpdateOptions{}, err
	}
	return coreUpdateOptions{Bundle: *bundle, Yes: *yes}, nil
}

func parseAllUpdateOptions(args []string) (allUpdateOptions, error) {
	var bundle *string
	var yes *bool
	selection, _, err := updateSourceFlags("update all", args, func(fs *flag.FlagSet) {
		bundle = fs.String("bundle", "recommended", "reviewed compatible core bundle")
		yes = fs.Bool("yes", false, "confirm panel and core maintenance impact")
	})
	if err != nil {
		return allUpdateOptions{}, err
	}
	if _, err := install.SelectCore(*bundle); err != nil {
		return allUpdateOptions{}, err
	}
	return allUpdateOptions{Selection: selection, Bundle: *bundle, Yes: *yes}, nil
}

func runUpdate(args []string) error {
	kind, rest, err := classifyUpdateCommand(args, terminal.IsTerminal(os.Stdin))
	if err != nil {
		return err
	}
	switch kind {
	case "menu":
		if len(rest) != 0 || !terminal.IsTerminal(os.Stdin) {
			return lifecycleArgsError()
		}
		return runUpdateMenu()
	case "manager":
		return runManagerUpdate(rest)
	case "panel":
		return runPanelUpdate(rest)
	case "core":
		return runCoreUpdate(rest)
	case "all":
		return runAllUpdate(rest)
	case "status":
		return runUpdateStatus(rest)
	}
	return lifecycleArgsError()
}

func runManagerUpdate(args []string) error {
	o, err := parseManagerUpdateOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return updateManagerSelection(ctx, install.NewRealHost(), o, os.Stdout)
}

func updateManagerSelection(ctx context.Context, h install.Host, o managerUpdateOptions, out io.Writer) error {
	client := distribution.NewClient(nil, distribution.Options{})
	checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	resolved, err := client.Resolve(checkCtx, o.Selection)
	cancel()
	if err != nil {
		return err
	}
	if !o.Refresh {
		if current, loadErr := install.LoadManagerBuild(ctx, h); loadErr == nil &&
			current.Channel == resolved.Channel && current.Ref == resolved.Ref && current.Commit == resolved.Commit && current.Version == resolved.Version {
			fmt.Fprintf(out, "Manager is current: %s (%s)\n", current.Version, current.Commit[:12])
			return nil
		}
	}
	selection := distribution.Selection{Channel: resolved.Channel, Ref: resolved.Ref}
	stopHeartbeat := startUpdateHeartbeat(ctx, out, "Acquiring verified manager build…")
	build, _, cleanup, err := prepareBuild(ctx, selection, "")
	stopHeartbeat()
	if err != nil {
		return err
	}
	defer cleanup()
	return install.UpdateManager(ctx, h, install.ManagerUpdateOptions{Build: build, Stdout: out})
}

func runCoreUpdate(args []string) error {
	o, err := parseCoreUpdateOptions(args)
	if err != nil {
		return err
	}
	if !o.Yes {
		if !terminal.IsTerminal(os.Stdin) {
			return fmt.Errorf("update core: --yes is required without an interactive terminal")
		}
		u := terminal.New(os.Stdin, os.Stdout, terminal.Detect(os.Stdin, os.Stdout, i18n.En))
		ok, confirmErr := u.Confirm(u.T("update.core_review"))
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			return terminal.ErrCanceled
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_, err = install.SwitchCore(ctx, install.NewRealHost(), install.CoreSwitchOptions{Selector: o.Bundle, ConfirmImpact: true, Stdout: os.Stdout})
	return err
}

func runAllUpdate(args []string) error {
	o, err := parseAllUpdateOptions(args)
	if err != nil {
		return err
	}
	if !o.Yes {
		if !terminal.IsTerminal(os.Stdin) {
			return fmt.Errorf("update all: --yes is required without an interactive terminal")
		}
		u := terminal.New(os.Stdin, os.Stdout, terminal.Detect(os.Stdin, os.Stdout, i18n.En))
		ok, confirmErr := u.Confirm(u.T("update.all_review"))
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			return terminal.ErrCanceled
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	h := install.NewRealHost()
	if err := install.CheckLifecycleReady(h); err != nil {
		return err
	}
	stopHeartbeat := startUpdateHeartbeat(ctx, os.Stdout, "Acquiring verified update build…")
	build, parent, cleanup, err := prepareBuild(ctx, o.Selection, "")
	stopHeartbeat()
	if err != nil {
		return err
	}
	defer cleanup()
	return install.UpdateAll(ctx, h, install.FullUpdateOptions{Build: build, StageParent: parent, Core: o.Bundle, Stdout: os.Stdout})
}

func runUpdateStatus(args []string) error {
	if len(args) != 0 {
		return lifecycleArgsError()
	}
	return printUpdateStatus(context.Background(), install.NewRealHost(), os.Stdout)
}

func printUpdateStatus(ctx context.Context, h install.Host, out io.Writer) error {
	manager := "not cached"
	if b, err := install.LoadManagerBuild(ctx, h); err == nil {
		manager = b.Version + " · " + b.Commit[:12]
	}
	st, err := install.LoadState(h)
	if err != nil {
		return err
	}
	panel, core := "not installed", "not installed"
	if st != nil {
		panel = st.Version
		r, inspectErr := install.InspectInstalledCore(ctx, h)
		if inspectErr != nil {
			return inspectErr
		}
		core = r.Requested.ID + " · " + r.ModuleIdentity
		if r.RebootRequired {
			core += " · reboot required"
		}
	}
	fmt.Fprintf(out, "Manager: %s\nPanel:   %s\nCore:    %s\n", manager, panel, core)
	return nil
}

func selectionArgs(s distribution.Selection) []string {
	return []string{"--" + s.Channel, s.Ref}
}

func runUpdateMenu() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	u := terminal.New(os.Stdin, os.Stdout, terminal.Detect(os.Stdin, os.Stdout, i18n.En))
	u.Context = ctx
	client := distribution.NewClient(nil, distribution.Options{})
	for {
		u.Header("WG-GUARD", u.T("update.subtitle"))
		n, err := u.ChooseRoot(u.T("update.title"), []string{
			u.T("update.all"), u.T("update.panel"), u.T("update.manager"), u.T("update.core"), u.T("update.status"),
		}, 1)
		if errors.Is(err, terminal.ErrCanceled) || errors.Is(err, terminal.ErrBack) {
			return nil
		}
		if err != nil {
			return err
		}
		if n == 5 {
			err = printUpdateStatus(ctx, install.NewRealHost(), os.Stdout)
			u.Result(err)
			continue
		}
		if n == 4 {
			ok, confirmErr := u.Confirm(u.T("update.core_review"))
			if confirmErr != nil {
				return confirmErr
			}
			if !ok {
				continue
			}
			err = runCoreUpdate([]string{"--bundle", "recommended", "--yes"})
			u.Result(err)
			continue
		}
		st, stateErr := install.LoadState(install.NewRealHost())
		if stateErr != nil {
			return stateErr
		}
		installed := "manager only"
		if st != nil {
			installed = st.Version
		}
		selection, pickErr := pickSource(ctx, u, client, installed)
		if errors.Is(pickErr, terminal.ErrBack) || errors.Is(pickErr, terminal.ErrCanceled) {
			continue
		}
		if pickErr != nil {
			u.Result(pickErr)
			continue
		}
		confirmKey := "update.manager_review"
		if n == 1 {
			confirmKey = "update.all_review"
		} else if n == 2 {
			confirmKey = "manage.update_review"
		}
		ok, confirmErr := u.ConfirmDefault(u.T(confirmKey), n == 3)
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			continue
		}
		args := selectionArgs(selection)
		switch n {
		case 1:
			args = append(args, "--bundle", "recommended", "--yes")
			err = runAllUpdate(args)
		case 2:
			err = runPanelUpdate(args)
		case 3:
			err = runManagerUpdate(args)
		}
		u.Result(err)
	}
}
