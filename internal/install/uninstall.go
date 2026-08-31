package install

import (
	"context"
	"fmt"
	"io"
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
	out := o.Stdout
	st, err := LoadState(h)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("uninstall: no install state at %s — nothing to uninstall (remove files manually if this host was set up differently)", StatePath)
	}
	rep := &UninstallReport{Mode: st.Mode}

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
	artifacts = append(artifacts, st.ConfigPath, StatePath)
	rep.Artifacts = artifacts

	if o.DryRun {
		fmt.Fprintln(out, "Uninstall plan (--dry-run, nothing changed):")
		printPlan(out, st, rep, o)
		return rep, nil
	}

	if !o.Yes {
		fmt.Fprintf(out, "Stop and remove WG-Guard (%s mode)? Data %s.\nType 'uninstall' to confirm: ",
			st.Mode, dataFate(o.PurgeData, st.DataDir))
		var answer string
		if o.Stdin != nil {
			if _, err := fmt.Fscanln(o.Stdin, &answer); err != nil && answer != "uninstall" {
				return nil, fmt.Errorf("uninstall: cancelled")
			}
		}
		if answer != "uninstall" {
			return nil, fmt.Errorf("uninstall: cancelled")
		}
	}

	// Stop first: a still-running service could reopen files being removed.
	step(out, "Stopping the node")
	switch st.Mode {
	case ModeDocker:
		if err := h.Run(ctx, []string{"docker", "compose", "-f", st.ComposePath, "down"}, longTimeout); err != nil {
			fmt.Fprintf(out, "  WARNING: compose down failed (%v) — continuing with removal\n", err)
		} else {
			rep.Stopped = true
		}
	case ModeNative:
		_ = h.Run(ctx, []string{"systemctl", "stop", "wg-guard"}, 60*time.Second)
		_ = h.Run(ctx, []string{"systemctl", "disable", "wg-guard"}, 30*time.Second)
		rep.Stopped = true
	}

	step(out, "Removing artifacts")
	for _, path := range artifacts {
		if err := h.Remove(path); err != nil {
			fmt.Fprintf(out, "  WARNING: could not remove %s: %v\n", path, err)
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
		_ = h.Run(ctx, pkgs, longTimeout)
		rep.PurgedPkgs = st.PackagesInstalled
	}

	if o.PurgeData {
		fmt.Fprintf(out, "\nWG-Guard uninstalled. Data purged (%s).\n", st.DataDir)
	} else {
		fmt.Fprintf(out, "\nWG-Guard uninstalled. Data kept at %s — delete manually when sure.\n", st.DataDir)
	}
	return rep, nil
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
