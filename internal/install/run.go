package install

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"io"
	"io/fs"
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
	// BeforeStart is M4's local-owner setup point, after configuration/settings
	// exist and before any public listener starts. Nil advertises no owner guarantee.
	BeforeStart   func(context.Context, Host, Plan, *State) error
	Selection     distribution.Selection
	BuildMetadata string
	Build         distribution.Build
	StageParent   string
	LocalImage    bool
	Plan          Plan
	Yes           bool // non-interactive: flags + defaults only

	Version        string // recorded in the state file
	SkipModule     bool   // do not attempt the host kernel-module install
	Prerequisites  PrerequisitePolicy
	Core           string // recommended, latest-compatible or exact catalog bundle ID
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// UpdateOptions drives one update.
type UpdateOptions struct {
	Selection      distribution.Selection
	Build          distribution.Build
	LocalImage     bool // explicitly local: never pull; remote must pull successfully
	Recover        bool
	Image          string // docker: new image reference (default: keep compose value)
	BinaryPath     string // native: staged binary to install
	SkipBackup     bool
	Rollback       bool // re-deploy the state-recorded last-known-good artifact
	Stdout, Stderr io.Writer
}

// Install locks the lifecycle, records prerequisite ownership/intents, stages
// the selected artifact and starts the service only after settings and the
// optional local-owner hook succeed. Failures retain a recovery journal.
func Install(ctx context.Context, h Host, o InstallOptions) (result *State, resultErr error) {
	out := o.Stdout
	if out == nil {
		out = io.Discard
	}
	prompt := newPrompt(o.Stdin, out, o.Yes)

	// Root: the installer writes /etc, binds ports and manages services.
	if !h.IsRoot() {
		return nil, fmt.Errorf("install: run as root (sudo)")
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := noPending(h); err != nil {
		return nil, err
	}
	if o.Build.BinaryPath == "" {
		o.Build.BinaryPath, err = h.SelfExe()
		if err != nil {
			return nil, err
		}
		o.Build.Channel = "local"
		o.Build.Version = o.Version
		o.Build.SHA256, _, err = fileDigest(ctx, h, o.Build.BinaryPath, 256<<20)
		if err != nil {
			return nil, err
		}
	}
	if _, err := inspectContract(ctx, h, []string{o.Build.BinaryPath}); err != nil {
		return nil, err
	}
	platform, err := InspectPlatform(ctx, h)
	if err != nil {
		return nil, err
	}
	// Completed installs refuse (rerun after uninstall or use update).
	if st, err := LoadState(h); err != nil {
		return nil, err
	} else if st != nil {
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
	if p.Mode == ModeNative && platform.Init != "systemd" {
		return nil, terminalError("install.error.systemd")
	}
	if err := resolveEndpoint(ctx, h, &p); err != nil {
		return nil, err
	}
	if err := prompt.confirm(p); err != nil {
		return nil, err
	}

	if err := preflight(ctx, h, p, out); err != nil {
		return nil, err
	}
	for _, target := range []string{ConfigPath, ComposePth, UnitPath} {
		if _, err := h.Stat(target); err == nil {
			return nil, terminalError("install.error.state")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if _, err := h.Stat(BinPath); err == nil {
		self, e := h.SelfExe()
		if e != nil || self != BinPath {
			return nil, terminalError("install.error.state")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	st := &State{
		Schema:     StateSchema,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Version:    o.Version,
		Mode:       p.Mode,
		ConfigPath: p.BootConfigPath(),
		DataDir:    p.DataDir,
		PublicIP:   p.PublicIP,
	}
	st.BinPath = BinPath
	if p.Mode == ModeDocker {
		st.ComposePath = ComposePth
		st.Image = p.Image
	} else {
		st.UnitPath = UnitPath
	}
	if err := h.MkdirAll(EtcDir, 0700); err != nil {
		return st, err
	}
	j := &Journal{Schema: 1, ID: transactionID(), Operation: "install", After: st}
	if err := j.save(h, "prepared"); err != nil {
		return st, err
	}
	defer func() {
		if resultErr != nil && !j.terminal() {
			st.Recovery = "install-incomplete"
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if j.DataMayHaveChanged {
				resultErr = errors.Join(resultErr, stopService(ctx, h, st))
			}
			resultErr = errors.Join(resultErr, saveState(h, st), j.save(h, "recovery-required"))
			result = st
		}
	}()
	bundle, err := SelectCore(o.Core)
	if err != nil {
		return nil, err
	}
	st.Platform = platform
	st.Core, err = EnsurePrerequisites(ctx, journalHost{Host: h, j: j}, p, platform, bundle, o.Prerequisites, o.SkipModule, st, out)
	if err != nil {
		return st, err
	}
	if err := j.save(h, "prepared"); err != nil {
		return st, err
	}
	if o.StageParent != "" && p.Mode == ModeDocker && p.Image == DefaultImage {
		p.Image, err = BuildRuntimeImage(ctx, h, o.Build, bundle, o.StageParent)
		if err != nil {
			return st, err
		}
		o.LocalImage = true
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

	if o.Build.BinaryPath != "" {
		if p.Mode == ModeDocker {
			if err := atomicWrite(h, ComposePth, []byte(RenderCompose(p)), 0644); err != nil {
				return st, err
			}
		}
		a, err := stageCandidate(ctx, h, st, UpdateOptions{Build: o.Build, Image: p.Image, LocalImage: o.LocalImage})
		if err != nil {
			return st, err
		}
		st.Current = a
		p.Image = a.Image
		st.Image = a.Image
	}
	if err := j.save(h, "swap-pending"); err != nil {
		return st, err
	}
	j.DataMayHaveChanged = true
	if err := j.save(h, "started"); err != nil {
		return st, err
	}
	switch p.Mode {
	case ModeDocker:
		if err := installDocker(ctx, h, p, st, out, o.BeforeStart); err != nil {
			return nil, err
		}
	case ModeNative:
		if err := installNative(ctx, h, p, st, out, o.BeforeStart); err != nil {
			return nil, err
		}
	}

	fmt.Fprintln(out)
	step(out, "Health check")
	if err := waitHealthy(ctx, h, p, installHealthWindow); err != nil {
		return st, fmt.Errorf("install: %w", err)
	}
	fmt.Fprintf(out, "  node answers on %s\n", p.HealthProbeLabel())

	st.TLSReadiness = "not-applicable"
	if p.TLSMode == "acme" || p.TLSMode == "manual" {
		st.TLSReadiness = "pending"
	}
	if err := saveState(h, st); err != nil {
		return st, fmt.Errorf("install: write state: %w", err)
	}
	if st.TLSReadiness == "pending" {
		if err := WaitCertificate(ctx, p, 90*time.Second); err != nil {
			// The installed service must remain available for ACME retries.
			if journalErr := j.save(h, "complete"); journalErr != nil {
				return st, errors.Join(err, journalErr)
			}
			j.DataMayHaveChanged = false
			return st, err
		}
		st.TLSReadiness = "verified"
		if err := saveState(h, st); err != nil {
			return st, err
		}
	}

	printSummary(out, p, st)
	if err := j.save(h, "complete"); err != nil {
		return st, err
	}
	return st, nil
}

// installDocker writes the compose project and brings the container up. It
// also verifies docker + the compose plugin exist before mutating anything.
func installDocker(ctx context.Context, h Host, p Plan, st *State, out io.Writer, beforeStart func(context.Context, Host, Plan, *State) error) error {
	step(out, "Docker preflight")
	if _, err := h.LookPath("docker"); err != nil {
		return fmt.Errorf("install: docker not found — install docker first (https://docs.docker.com/engine/install/) or use --mode native")
	}
	if err := h.Run(ctx, []string{"docker", "compose", "version"}, 30*time.Second); err != nil {
		return fmt.Errorf("install: docker compose plugin missing (%v) — install docker-compose-plugin", err)
	}

	step(out, "Host CLI (shim)")
	self, err := h.SelfExe()
	if st.Current != nil {
		self = st.Current.Binary
		err = nil
	}
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

	// Runtime settings (wizard choices) seed through the installed CLI while
	// the state file still doesn't exist — see seedSettings for why.
	if err := seedSettings(ctx, h, p, out); err != nil {
		return err
	}

	step(out, "Starting container")
	if beforeStart != nil {
		if err := beforeStart(ctx, h, p, st); err != nil {
			return err
		}
	}
	if err := h.Run(ctx, []string{"docker", "compose", "-f", ComposePth, "up", "-d"}, longTimeout); err != nil {
		return fmt.Errorf("install: docker compose up: %w", err)
	}
	return nil
}

// installNative installs the binary + unit and starts the service.
func installNative(ctx context.Context, h Host, p Plan, st *State, out io.Writer, beforeStart func(context.Context, Host, Plan, *State) error) error {
	step(out, "systemd preflight")
	if _, err := h.LookPath("systemctl"); err != nil {
		return fmt.Errorf("install: systemctl not found — native mode needs systemd")
	}

	step(out, "Binary")
	self, err := h.SelfExe()
	if st.Current != nil {
		self = st.Current.Binary
		err = nil
	}
	if err != nil {
		return fmt.Errorf("install: locate running binary: %w", err)
	}
	if err := h.CopyFile(self, BinPath, 0o755); err != nil {
		return fmt.Errorf("install: install binary to %s: %w", BinPath, err)
	}
	st.BinPath = BinPath
	fmt.Fprintf(out, "  %s → %s\n", self, BinPath)

	// Runtime settings (wizard choices) seed through the just-installed
	// binary before the service starts — see seedSettings for why.
	if err := seedSettings(ctx, h, p, out); err != nil {
		return err
	}

	step(out, "Systemd unit")
	if err := h.WriteFile(UnitPath, []byte(RenderUnit(p)), 0o644); err != nil {
		return fmt.Errorf("install: write unit: %w", err)
	}
	st.UnitPath = UnitPath
	if err := h.Run(ctx, []string{"systemctl", "daemon-reload"}, 30*time.Second); err != nil {
		return fmt.Errorf("install: daemon-reload: %w", err)
	}
	if beforeStart != nil {
		if err := beforeStart(ctx, h, p, st); err != nil {
			return err
		}
	}
	if err := h.Run(ctx, []string{"systemctl", "enable", "--now", "wg-guard"}, 60*time.Second); err != nil {
		return fmt.Errorf("install: enable service: %w", err)
	}
	fmt.Fprintln(out, "  wg-guard.service enabled and started")
	return nil
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
	if p.TLSMode == "manual" {
		cert, certErr := h.ReadFile(p.CertFile)
		key, keyErr := h.ReadFile(p.KeyFile)
		if certErr != nil || keyErr != nil {
			return terminalError("install.error.manual_files")
		}
		if _, err := tls.X509KeyPair(cert, key); err != nil {
			return terminalError("install.error.manual_pair")
		}
	}
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
	if p.TLSMode == "proxy" || p.TLSMode == "dev" {
		fmt.Fprintf(out, "  SSH:          %s\n", p.SSHTunnel())
	}
	fmt.Fprintf(out, "  Mode:         %s\n", p.Mode)
	fmt.Fprint(out, i18n.T(i18n.En, "install.summary.core", st.Core.Requested.ID, st.Core.Requested.ToolsVersion, st.Core.Requested.KernelVersion, st.Core.ToolsPackage, st.Core.KernelPackage, st.Core.LoadedVersion, st.Core.ModuleIdentity, st.Core.RebootRequired))
	fmt.Fprintf(out, "  Config:       %s\n", p.BootConfigPath())
	fmt.Fprintf(out, "  Data:         %s\n", p.DataDir)
	if p.Mode == ModeDocker {
		fmt.Fprintf(out, "  Compose:      %s\n", ComposePth)
		fmt.Fprintf(out, "  Service:      docker compose -f %s <up -d|down|pull>\n", ComposePth)
	} else {
		fmt.Fprintf(out, "  Service:      systemctl <status|restart|stop> wg-guard\n")
	}
	fmt.Fprintf(out, "  Diagnostics:  wg-guard doctor\n")
	if p.TelegramToken != "" {
		fmt.Fprintf(out, "  Telegram:     daily %s UTC → chat %s (test: wg-guard backup telegram-test)\n", p.TelegramTime, p.TelegramChat)
	}
	fmt.Fprintf(out, "\n  Next steps:\n")
	fmt.Fprintf(out, "   1. Open %s and create the owner account (first-run wizard).\n", p.PanelURL())
	if p.TLSMode == "acme" {
		fmt.Fprint(out, i18n.T(i18n.En, "install.summary.certificate", st.TLSReadiness))
	} else {
		fmt.Fprintf(out, "   2. Create the first interface (awg0) — ports and subnet are auto-assigned.\n")
	}
	fmt.Fprintf(out, "   3. wg-guard status · wg-guard doctor · wg-guard backup create\n\n")
}

func step(out io.Writer, name string) { fmt.Fprintf(out, "==> %s\n", name) }
