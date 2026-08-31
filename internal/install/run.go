package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// longTimeout bounds package installs and image pulls; everything else runs
// well under a minute.
const longTimeout = 10 * time.Minute

// Health-check windows (package vars so tests can shrink them).
var (
	installHealthWindow = 60 * time.Second
	updateHealthWindow  = 90 * time.Second
)

// InstallOptions drives one install run. Plan is pre-filled from CLI flags;
// empty fields are prompted for unless Yes.
type InstallOptions struct {
	Plan Plan
	Yes  bool // non-interactive: flags + defaults only

	Version        string // recorded in the state file
	SkipModule     bool   // do not attempt the host kernel-module install
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// UpdateOptions drives one update.
type UpdateOptions struct {
	Image          string // docker: new image reference (default: keep compose value)
	BinaryPath     string // native: staged binary to install
	SkipBackup     bool
	Rollback       bool // re-deploy the state-recorded last-known-good artifact
	Stdout, Stderr io.Writer
}

// Install runs the full installation: preflight → config + artifacts →
// service up → health check → state file. On any failure the already-written
// files are left in place (they are inert without the service) and the error
// explains the step; rerunning install after fixing is safe (preflight
// refuses only a completed install, detected via the state file written last).
func Install(ctx context.Context, h Host, o InstallOptions) (*State, error) {
	out := o.Stdout
	prompt := newPrompt(o.Stdin, out, o.Yes)

	// Root: the installer writes /etc, binds ports and manages services.
	if !h.IsRoot() {
		return nil, fmt.Errorf("install: run as root (sudo)")
	}
	// Completed installs refuse (rerun after uninstall or use update).
	if st, err := LoadState(h); err == nil && st != nil {
		return nil, fmt.Errorf("install: WG-Guard is already installed (%s mode, %s); use 'wg-guard update' or 'wg-guard uninstall' first", st.Mode, StatePath)
	}

	// Interactive plan completion.
	if err := prompt.plan(&o.Plan, h); err != nil {
		return nil, err
	}
	p, err := o.Plan.Resolve()
	if err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}
	if err := prompt.confirm(p); err != nil {
		return nil, err
	}

	if err := preflight(ctx, h, p, out); err != nil {
		return nil, err
	}

	st := &State{
		Schema:     StateSchema,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Version:    o.Version,
		Mode:       p.Mode,
		ConfigPath: p.BootConfigPath(),
		DataDir:    p.DataDir,
	}

	if err := h.MkdirAll(p.EtcDir, 0o755); err != nil {
		return nil, fmt.Errorf("install: mkdir %s: %w", p.EtcDir, err)
	}
	if err := h.MkdirAll(p.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("install: mkdir %s: %w", p.DataDir, err)
	}
	cfgToml, err := renderBootConfig(p)
	if err != nil {
		return nil, fmt.Errorf("install: config: %w", err)
	}
	if err := h.WriteFile(p.BootConfigPath(), cfgToml, 0o600); err != nil {
		return nil, fmt.Errorf("install: write config: %w", err)
	}

	switch p.Mode {
	case ModeDocker:
		if err := installDocker(ctx, h, p, st, out); err != nil {
			return nil, err
		}
	case ModeNative:
		if err := installNative(ctx, h, p, st, out); err != nil {
			return nil, err
		}
	}

	fmt.Fprintln(out)
	step(out, "Health check")
	if err := waitHealthy(ctx, h, p, installHealthWindow); err != nil {
		return st, fmt.Errorf("install: %w", err)
	}
	fmt.Fprintf(out, "  node answers on %s\n", p.HealthProbeLabel())

	// The state file is written LAST: it marks the install as complete and
	// is the uninstall/update contract.
	stateJSON, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	if err := h.WriteFile(StatePath, append(stateJSON, '\n'), 0o644); err != nil {
		return st, fmt.Errorf("install: write state: %w", err)
	}

	printSummary(out, p, st)
	return st, nil
}

// installDocker writes the compose project and brings the container up. It
// also verifies docker + the compose plugin exist before mutating anything.
func installDocker(ctx context.Context, h Host, p Plan, st *State, out io.Writer) error {
	step(out, "Docker preflight")
	if _, err := h.LookPath("docker"); err != nil {
		return fmt.Errorf("install: docker not found — install docker first (https://docs.docker.com/engine/install/) or use --mode native")
	}
	if err := h.Run(ctx, []string{"docker", "compose", "version"}, 30*time.Second); err != nil {
		return fmt.Errorf("install: docker compose plugin missing (%v) — install docker-compose-plugin", err)
	}

	step(out, "Host CLI (shim)")
	self, err := h.SelfExe()
	if err != nil {
		return fmt.Errorf("install: locate running binary: %w", err)
	}
	if err := h.CopyFile(self, BinPath, 0o755); err != nil {
		return fmt.Errorf("install: install host CLI to %s: %w", BinPath, err)
	}
	st.BinPath = BinPath
	fmt.Fprintf(out, "  %s → %s (mode-aware: panel commands exec into the container)\n", self, BinPath)

	step(out, "Compose project")
	compose := RenderCompose(p)
	if err := h.WriteFile(ComposePth, []byte(compose), 0o644); err != nil {
		return fmt.Errorf("install: write compose: %w", err)
	}
	st.ComposePath = ComposePth
	st.Image = p.Image
	fmt.Fprintf(out, "  wrote %s (image %s)\n", ComposePth, p.Image)

	// Host kernel module: the data plane (ADR-0006). Best effort with a loud
	// warning — userspace fallback exists and the panel reports tooling drift
	// at boot.
	if err := ensureKernelModule(ctx, h, st, out); err != nil {
		fmt.Fprintf(out, "  WARNING: %v\n", err)
		fmt.Fprintf(out, "  the panel will still run; tunnels need the module or the userspace daemon\n")
		fmt.Fprintf(out, "  (docs/operations/deployment.md → Host requirements)\n")
	}

	step(out, "Starting container")
	if err := h.Run(ctx, []string{"docker", "compose", "-f", ComposePth, "up", "-d"}, longTimeout); err != nil {
		return fmt.Errorf("install: docker compose up: %w", err)
	}
	return nil
}

// installNative installs the binary + unit and starts the service.
func installNative(ctx context.Context, h Host, p Plan, st *State, out io.Writer) error {
	step(out, "systemd preflight")
	if _, err := h.LookPath("systemctl"); err != nil {
		return fmt.Errorf("install: systemctl not found — native mode needs systemd")
	}

	step(out, "Binary")
	self, err := h.SelfExe()
	if err != nil {
		return fmt.Errorf("install: locate running binary: %w", err)
	}
	if err := h.CopyFile(self, BinPath, 0o755); err != nil {
		return fmt.Errorf("install: install binary to %s: %w", BinPath, err)
	}
	st.BinPath = BinPath
	fmt.Fprintf(out, "  %s → %s\n", self, BinPath)

	step(out, "Systemd unit")
	if err := h.WriteFile(UnitPath, []byte(RenderUnit(p)), 0o644); err != nil {
		return fmt.Errorf("install: write unit: %w", err)
	}
	st.UnitPath = UnitPath
	if err := h.Run(ctx, []string{"systemctl", "daemon-reload"}, 30*time.Second); err != nil {
		return fmt.Errorf("install: daemon-reload: %w", err)
	}
	if err := h.Run(ctx, []string{"systemctl", "enable", "--now", "wg-guard"}, 60*time.Second); err != nil {
		return fmt.Errorf("install: enable service: %w", err)
	}
	fmt.Fprintln(out, "  wg-guard.service enabled and started")
	return nil
}

// ensureKernelModule verifies the AmneziaWG kernel module is loadable and,
// if not, walks a recovery ladder before warning: modprobe → DKMS rebuild
// for the RUNNING kernel (an apt kernel upgrade leaves a DKMS module built
// for the old series — the rebuild needs the matching headers) → fresh PPA
// install. When the module loads, an /etc/modules-load.d entry makes it
// boot-persistent. The returned error is a warning for the caller — module
// absence is not fatal (ADR-0003 userspace fallback), but the operator must
// see it.
func ensureKernelModule(ctx context.Context, h Host, st *State, out io.Writer) error {
	// Loaded, or loadable right now?
	if data, err := h.ReadFile("/proc/modules"); err == nil && strings.Contains(string(data), "amneziawg") {
		fmt.Fprintln(out, "  kernel module amneziawg: loaded")
		return markModuleBootPersistence(h, st, out)
	}
	if err := h.Run(ctx, []string{"modprobe", "amneziawg"}, 30*time.Second); err == nil {
		fmt.Fprintln(out, "  kernel module amneziawg: loaded via modprobe")
		return markModuleBootPersistence(h, st, out)
	}
	// DKMS rebuild path: the package is installed but built for a different
	// kernel series (typical after an unattended kernel upgrade).
	if rebuilt := rebuildDKMSModule(ctx, h, out); rebuilt {
		if err := h.Run(ctx, []string{"modprobe", "amneziawg"}, 30*time.Second); err == nil {
			fmt.Fprintln(out, "  kernel module rebuilt for the running kernel and loaded")
			return markModuleBootPersistence(h, st, out)
		}
	}
	// Fresh-install path (Ubuntu + PPA per docs/integrations/amneziawg.md).
	fmt.Fprintln(out, "  kernel module not present — attempting amneziawg-dkms install (may take minutes)…")
	apt := func(argv ...string) error { return h.Run(ctx, argv, longTimeout) }
	if err := apt("apt-get", "update"); err == nil {
		err = apt("apt-get", "install", "-y", "software-properties-common")
		if err == nil {
			err = apt("add-apt-repository", "-y", "ppa:amnezia/ppa")
		}
		if err == nil {
			err = apt("apt-get", "update")
		}
		if err == nil {
			// Headers for the RUNNING kernel are what DKMS builds against.
			if kr, e := h.Output(ctx, []string{"uname", "-r"}, 10*time.Second); e == nil {
				_ = apt("apt-get", "install", "-y", "linux-headers-"+strings.TrimSpace(kr))
			}
			err = apt("apt-get", "install", "-y", "amneziawg-dkms")
		}
	}
	if err := h.Run(ctx, []string{"modprobe", "amneziawg"}, 30*time.Second); err == nil {
		st.PackagesInstalled = append(st.PackagesInstalled, "amneziawg-dkms", "software-properties-common")
		fmt.Fprintln(out, "  kernel module installed and loaded")
		return markModuleBootPersistence(h, st, out)
	}
	return fmt.Errorf("kernel module could not be installed automatically")
}

// rebuildDKMSModule rebuilds an already-registered DKMS module for the
// running kernel: install matching headers, run dkms autoinstall, refresh
// module dependencies. Reports whether every step succeeded.
func rebuildDKMSModule(ctx context.Context, h Host, out io.Writer) bool {
	dkmsStatus, err := h.Output(ctx, []string{"dkms", "status"}, 30*time.Second)
	if err != nil || !strings.Contains(dkmsStatus, "amneziawg") {
		return false
	}
	kr, err := h.Output(ctx, []string{"uname", "-r"}, 10*time.Second)
	if err != nil {
		return false
	}
	kr = strings.TrimSpace(kr)
	if strings.Contains(dkmsStatus, kr) {
		// Registered (and likely built) for the running kernel already; a
		// plain modprobe failure means something else — skip to the warning.
		return false
	}
	fmt.Fprintf(out, "  DKMS module registered for another kernel (running %s) — rebuilding…\n", kr)
	apt := func(argv ...string) error { return h.Run(ctx, argv, longTimeout) }
	if err := apt("apt-get", "update"); err != nil {
		return false
	}
	if err := apt("apt-get", "install", "-y", "linux-headers-"+kr); err != nil {
		fmt.Fprintf(out, "  WARNING: headers for %s could not be installed: %v\n", kr, err)
		return false
	}
	if err := h.Run(ctx, []string{"dkms", "autoinstall"}, longTimeout); err != nil {
		fmt.Fprintf(out, "  WARNING: dkms autoinstall failed: %v\n", err)
		return false
	}
	return h.Run(ctx, []string{"depmod", "-a"}, 5*time.Minute) == nil
}

func addUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// ModuleAutoLoadPath is the systemd-modules-load entry the installer writes
// so the AmneziaWG module survives reboots (uninstall removes it).
const ModuleAutoLoadPath = "/etc/modules-load.d/wg-guard.conf"

func markModuleBootPersistence(h Host, st *State, out io.Writer) error {
	if err := h.WriteFile(ModuleAutoLoadPath, []byte("# Written by wg-guard install: load the AmneziaWG module at boot\namneziawg\n"), 0o644); err != nil {
		fmt.Fprintf(out, "  WARNING: boot persistence not written: %v\n", err)
		return nil
	}
	st.ExtraFiles = addUnique(st.ExtraFiles, ModuleAutoLoadPath)
	return nil
}

// preflight fails on a busy panel/challenge port (difficult to misuse) and
// warns on DNS problems the operator must fix for ACME.
func preflight(ctx context.Context, h Host, p Plan, out io.Writer) error {
	step(out, "Preflight")
	panelAddr := fmt.Sprintf(":%d", p.PanelPort)
	if !h.PortFree(panelAddr) {
		return fmt.Errorf("install: panel port %d is already in use — free it or choose another with --panel-port", p.PanelPort)
	}
	if p.TLSMode == "acme" && !h.PortFree(fmt.Sprintf(":%d", p.ACMEHTTPPort)) {
		return fmt.Errorf("install: acme challenge port %d is already in use (HTTP-01 requires it)", p.ACMEHTTPPort)
	}
	fmt.Fprintf(out, "  ports %d free\n", p.PanelPort)
	if p.TLSMode == "acme" {
		addrs, err := h.LookupHost(p.Domain)
		switch {
		case err != nil || len(addrs) == 0:
			fmt.Fprintf(out, "  WARNING: %s does not resolve yet — certificate issuance will fail until DNS points here\n", p.Domain)
		default:
			fmt.Fprintf(out, "  %s resolves (%s)\n", p.Domain, strings.Join(addrs, ", "))
		}
	}
	return nil
}

// waitHealthy polls the health probe until it answers 200 (or, on the ACME
// sidecar, 302 — the redirect IS the healthy answer).
func waitHealthy(ctx context.Context, h Host, p Plan, within time.Duration) error {
	url, skipVerify, err := p.HealthProbeURL()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(within)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ProbeHealth(ctx, url, skipVerify); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("health check aborted: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("node did not become healthy within %s: %v", within, lastErr)
}

func printSummary(out io.Writer, p Plan, st *State) {
	step(out, "Done")
	fmt.Fprintf(out, "\n  Panel:        %s\n", p.PanelURL())
	fmt.Fprintf(out, "  Mode:         %s\n", p.Mode)
	fmt.Fprintf(out, "  Config:       %s\n", p.BootConfigPath())
	fmt.Fprintf(out, "  Data:         %s\n", p.DataDir)
	if p.Mode == ModeDocker {
		fmt.Fprintf(out, "  Compose:      %s\n", ComposePth)
		fmt.Fprintf(out, "  Service:      docker compose -f %s <up -d|down|pull>\n", ComposePth)
	} else {
		fmt.Fprintf(out, "  Service:      systemctl <status|restart|stop> wg-guard\n")
	}
	fmt.Fprintf(out, "  Diagnostics:  wg-guard doctor\n\n")
	fmt.Fprintf(out, "  Next steps:\n")
	fmt.Fprintf(out, "   1. Open %s and create the owner account (first-run wizard).\n", p.PanelURL())
	if p.TLSMode == "acme" {
		fmt.Fprintf(out, "   2. The TLS certificate is issued on first browser visit (needs port %d reachable).\n", p.ACMEHTTPPort)
	} else {
		fmt.Fprintf(out, "   2. Create the first interface (awg0) — ports and subnet are auto-assigned.\n")
	}
	fmt.Fprintf(out, "   3. wg-guard status · wg-guard doctor · wg-guard backup create\n\n")
}

func step(out io.Writer, name string) { fmt.Fprintf(out, "==> %s\n", name) }
