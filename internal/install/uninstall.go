package install

import (
	"context"
	"errors"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"io"
	"io/fs"
	"time"
)

// UninstallReport lists what uninstall did or would do (--dry-run).
type UninstallReport struct {
	Mode       Mode
	Stopped    bool
	Artifacts  []string // removed files
	KeptData   string   // non-empty when the data dir was preserved
	PurgedData bool
	PurgedPkgs []string
}

// UninstallOptions selects what uninstall removes. Data and the packages the
// installer installed are kept unless explicitly purged — uninstall is
// difficult to misuse by default.
type UninstallOptions struct {
	DryRun         bool
	PurgeData      bool
	PurgePackages  bool
	Yes            bool
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Uninstall stops the node and removes only the artifacts recorded in the
// install state (ADR-0006: "removes only WG-Guard-owned resources"). Data is
// preserved unless PurgeData; installer-installed packages are preserved
// unless PurgePackages. With DryRun nothing is mutated.
func Uninstall(ctx context.Context, h Host, o UninstallOptions) (*UninstallReport, error) {
	if !h.IsRoot() {
		return nil, terminalError("install.error.root")
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return nil, err
	}
	defer unlock()
	out := o.Stdout
	if out == nil {
		out = io.Discard
	}
	st, err := LoadState(h)
	if err != nil {
		return nil, err
	}
	pending, err := LoadJournal(h)
	if err != nil {
		return nil, err
	}
	if st == nil && pending != nil && !pending.terminal() {
		if pending.Operation == "uninstall" {
			st = pending.Before
		} else if pending.Operation == "install" {
			st = pending.After
		}
	}
	if st == nil {
		return nil, terminalError("install.error.no_state")
	}
	rep := &UninstallReport{Mode: st.Mode}
	if pending != nil && !pending.terminal() && pending.Operation != "install" && pending.Operation != "uninstall" {
		return nil, terminalError("install.error.pending")
	}

	// What will be removed, computed up front (dry-run prints the same list).
	var artifacts []string
	if st.ComposePath != "" {
		artifacts = append(artifacts, st.ComposePath)
	}
	if st.UnitPath != "" {
		artifacts = append(artifacts, st.UnitPath)
	}
	if st.BinPath != "" {
		artifacts = append(artifacts, st.BinPath)
	}
	artifacts = append(artifacts, st.ExtraFiles...)
	artifacts = append(artifacts, st.ConfigPath)
	for _, a := range []*Artifact{st.Current, st.Previous} {
		if a != nil {
			artifacts = append(artifacts, a.Binary)
			if a.Compose != "" {
				artifacts = append(artifacts, a.Compose)
			}
		}
	}
	rep.Artifacts = append(append([]string{}, artifacts...), StatePath)

	if o.DryRun {
		fmt.Fprintln(out, "Uninstall plan (--dry-run, nothing changed):")
		printPlan(out, st, rep, o)
		return rep, nil
	}

	if !o.Yes {
		fmt.Fprintf(out, "Stop and remove WG-Guard (%s mode)? Data %s.\nType 'uninstall' to confirm: ",
			st.Mode, dataFate(o.PurgeData, st.DataDir))
		u := terminal.New(o.Stdin, out, terminal.Detect(o.Stdin, out, i18n.En))
		u.Context = ctx
		answer, err := u.Ask("uninstall", "")
		if err != nil {
			return nil, err
		}
		if answer != "uninstall" {
			return nil, terminal.ErrCanceled
		}
	}

	// Stop first: a still-running service could reopen files being removed.
	j := &Journal{Schema: 1, ID: transactionID(), Operation: "uninstall", Before: st}
	if err := j.save(h, "prepared"); err != nil {
		return rep, err
	}
	step(out, "Stopping the node")
	if st.Mode == ModeNative {
		absent, err := stopNativeService(ctx, h)
		if err != nil {
			return rep, err
		}
		if !absent {
			if err := h.Run(ctx, []string{"systemctl", "disable", "wg-guard"}, 30*time.Second); err != nil {
				return rep, err
			}
		}
	} else if err := stopService(ctx, h, st); err != nil {
		return rep, err
	}
	rep.Stopped = true
	if err := j.save(h, "swap-pending"); err != nil {
		return rep, err
	}

	step(out, "Removing artifacts")
	for _, path := range artifacts {
		if err := h.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return rep, err
		} else {
			fmt.Fprintf(out, "  removed %s\n", path)
		}
	}

	if o.PurgeData {
		if err := h.RemoveAll(st.DataDir); err != nil {
			return rep, fmt.Errorf("uninstall: purge data: %w", err)
		}
		rep.PurgedData = true
		fmt.Fprintf(out, "  purged %s\n", st.DataDir)
	} else {
		rep.KeptData = st.DataDir
	}

	if o.PurgePackages && len(st.PackagesInstalled) > 0 {
		step(out, "Removing installer-installed packages")
		pkgs := append([]string{"apt-get", "remove", "-y"}, st.PackagesInstalled...)
		if err := h.Run(ctx, pkgs, longTimeout); err != nil {
			return rep, err
		}
		rep.PurgedPkgs = st.PackagesInstalled
	}

	if o.PurgeData {
		fmt.Fprintf(out, "\nWG-Guard uninstalled. Data purged (%s).\n", st.DataDir)
	} else {
		fmt.Fprintf(out, "\nWG-Guard uninstalled. Data kept at %s — delete manually when sure.\n", st.DataDir)
	}
	if err := h.Remove(StatePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return rep, err
	}
	return rep, j.save(h, "complete")
}

func dataFate(purge bool, dir string) string {
	if purge {
		return "will be purged"
	}
	return "stays on disk (" + dir + " — delete manually when sure)"
}

func printPlan(out io.Writer, st *State, rep *UninstallReport, o UninstallOptions) {
	fmt.Fprintf(out, "  mode: %s\n", st.Mode)
	for _, a := range rep.Artifacts {
		fmt.Fprintf(out, "  remove:   %s\n", a)
	}
	if o.PurgeData {
		fmt.Fprintf(out, "  purge:    %s\n", st.DataDir)
	} else {
		fmt.Fprintf(out, "  keep:     %s (data + backups)\n", st.DataDir)
	}
	if o.PurgePackages && len(st.PackagesInstalled) > 0 {
		for _, p := range st.PackagesInstalled {
			fmt.Fprintf(out, "  purge:    package %s\n", p)
		}
	}
}
