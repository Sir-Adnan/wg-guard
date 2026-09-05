package amneziawg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/network"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// PinnedToolsVersion is the amneziawg-tools version WG-Guard was verified
// against (docs/integrations/amneziawg.md). Drifting versions still work
// (the CLI surface is stable), but reconcile surfaces the mismatch.
const PinnedToolsVersion = "v3.1.20260812"

// Backend drives the pinned awg CLI (netlink kernel module or UAPI userspace
// daemon — transparent to the CLI) plus iproute2 for link state. All execs go
// through subprocess.Runner; no shell, explicit argv, timeouts.
type Backend struct {
	run   subprocess.Runner
	links *network.Links
	awg   string
}

// New returns a Backend using the `awg` binary from PATH.
func New(run subprocess.Runner) *Backend {
	return NewWithBinary(run, "awg")
}

// NewWithBinary overrides the awg binary (tests, pinned install path).
func NewWithBinary(run subprocess.Runner, awg string) *Backend {
	return &Backend{
		run:   run,
		links: &network.Links{Run: run},
		awg:   awg,
	}
}

// ToolsVersion returns the tools version string (e.g. "v3.1.20260812") from
// `awg --version`, or an error when awg is not usable.
func (b *Backend) ToolsVersion(ctx context.Context) (string, error) {
	res, err := b.run.Run(ctx, []string{b.awg, "--version"})
	if err != nil {
		return "", fmt.Errorf("amneziawg: probe version: %w", err)
	}
	// Output: "amneziawg-tools v3.1.20260812"
	for _, f := range strings.Fields(string(res.Stdout)) {
		if strings.HasPrefix(f, "v") && strings.ContainsRune(f, '.') {
			return f, nil
		}
	}
	return "", fmt.Errorf("amneziawg: unparseable --version output")
}

func (b *Backend) ListInterfaces(ctx context.Context) ([]string, error) {
	// `awg show` with no interfaces exits 0 with empty output (pinned fact).
	res, err := b.run.Run(ctx, []string{b.awg, "show", "interfaces"})
	if err != nil {
		return nil, fmt.Errorf("amneziawg: list interfaces: %w", err)
	}
	// Pinned tools print a space-separated line, not one name per line.
	// Fields also tolerates the trailing newline and empty no-interface result.
	return strings.Fields(string(res.Stdout)), nil
}

// CreateInterface brings up a link, applies the crypto config, verifies it,
// addresses the link, and marks it up — the wg-quick bring-up order, minus
// every global firewall mutation (ADR-0004: awg-quick is never used). Any
// failure after link creation (including a failed post-apply verify) rolls
// the link back so a failed create cannot leave half-state behind.
func (b *Backend) CreateInterface(ctx context.Context, spec tunnel.InterfaceSpec) error {
	if err := b.links.CreateAWG(ctx, spec.Name, spec.MTU); err != nil {
		return fmt.Errorf("amneziawg: create link %s: %w", spec.Name, err)
	}
	cfg := tunnel.InterfaceConfig{
		PrivateKey:  spec.PrivateKey,
		ListenPort:  spec.ListenPort,
		Fwmark:      spec.Fwmark,
		Obfuscation: spec.Obfuscation,
	}
	if err := b.applySetconf(ctx, spec.Name, cfg); err != nil {
		b.rollbackLink(ctx, spec.Name)
		return fmt.Errorf("amneziawg: configure %s: %w", spec.Name, err)
	}
	st, err := b.Dump(ctx, spec.Name)
	if err == nil {
		err = verifyApplied(spec.Name, cfg, st)
	}
	if err != nil {
		b.rollbackLink(ctx, spec.Name)
		return fmt.Errorf("amneziawg: verify %s: %w", spec.Name, err)
	}
	if spec.Address != "" {
		if err := b.links.AddAddress(ctx, spec.Name, spec.Address); err != nil {
			b.rollbackLink(ctx, spec.Name)
			return fmt.Errorf("amneziawg: address %s: %w", spec.Name, err)
		}
	}
	if err := b.links.SetUp(ctx, spec.Name); err != nil {
		b.rollbackLink(ctx, spec.Name)
		return fmt.Errorf("amneziawg: up %s: %w", spec.Name, err)
	}
	return nil
}

func (b *Backend) rollbackLink(ctx context.Context, name string) {
	// Best effort: the original error is what matters to the caller.
	_ = b.links.Delete(ctx, name)
}

func (b *Backend) RemoveInterface(ctx context.Context, name string) error {
	err := b.links.Delete(ctx, name)
	if errors.Is(err, network.ErrLinkNotFound) {
		return tunnel.ErrInterfaceNotFound
	}
	if err != nil {
		return fmt.Errorf("amneziawg: remove %s: %w", name, err)
	}
	return nil
}

// ApplyInterfaceConfig replaces the interface configuration (setconf
// semantics) and verifies the result. See the package comment for why the
// verification exists.
func (b *Backend) ApplyInterfaceConfig(ctx context.Context, name string, cfg tunnel.InterfaceConfig) error {
	if err := b.applySetconf(ctx, name, cfg); err != nil {
		return err
	}
	st, err := b.Dump(ctx, name)
	if err != nil {
		return fmt.Errorf("amneziawg: verify %s: %w", name, err)
	}
	if err := verifyApplied(name, cfg, st); err != nil {
		return err
	}
	return nil
}

// verifyApplied compares the freshly-dumped state against what was just
// applied. It checks the properties that must survive every apply: key pair,
// explicit listen port, and the full obfuscation parameter set.
func verifyApplied(name string, cfg tunnel.InterfaceConfig, st tunnel.InterfaceState) error {
	wantPub, err := tunnel.PublicKeyFromPrivate(cfg.PrivateKey)
	if err != nil {
		return fmt.Errorf("amneziawg: verify %s: %w", name, err)
	}
	if st.PublicKey != wantPub {
		return fmt.Errorf("amneziawg: verify %s: backend public key did not take effect", name)
	}
	if cfg.ListenPort > 0 && st.ListenPort != cfg.ListenPort {
		return fmt.Errorf("amneziawg: verify %s: listen port is %d, want %d", name, st.ListenPort, cfg.ListenPort)
	}
	if st.Obfuscation != cfg.Obfuscation {
		return fmt.Errorf("amneziawg: verify %s: obfuscation parameters did not take effect (backend shows a different set)", name)
	}
	return nil
}

// SyncPeers diff-applies the peer list (syncconf semantics): peers absent
// from the rendered file are removed, listed peers are upserted, and the
// interface's own configuration is untouched.
func (b *Backend) SyncPeers(ctx context.Context, name string, peers []tunnel.PeerConfig) error {
	beforeResult, err := b.run.Run(ctx, []string{b.awg, "showconf", name})
	if err != nil {
		return fmt.Errorf("amneziawg: read current interface configuration %s: %w", name, err)
	}
	beforeSection, err := currentInterfaceSection(beforeResult.Stdout)
	if err != nil {
		return fmt.Errorf("amneziawg: current interface configuration %s: %w", name, err)
	}
	desired, err := composeSyncconf(beforeResult.Stdout, peers)
	if err != nil {
		return fmt.Errorf("amneziawg: current interface configuration %s: %w", name, err)
	}
	path, cleanup, err := writeTempConfig(desired)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := b.run.Run(ctx, []string{b.awg, "syncconf", name, path}); err != nil {
		return fmt.Errorf("amneziawg: syncconf %s: %w", name, err)
	}
	afterResult, err := b.run.Run(ctx, []string{b.awg, "showconf", name})
	if err != nil {
		return fmt.Errorf("amneziawg: verify syncconf %s: %w", name, err)
	}
	afterSection, err := currentInterfaceSection(afterResult.Stdout)
	if err != nil {
		return fmt.Errorf("amneziawg: verify syncconf interface configuration %s: %w", name, err)
	}
	if !bytes.Equal(beforeSection, afterSection) {
		return fmt.Errorf("amneziawg: syncconf %s changed interface configuration", name)
	}
	return nil
}

func (b *Backend) Dump(ctx context.Context, name string) (tunnel.InterfaceState, error) {
	res, err := b.run.Run(ctx, []string{b.awg, "show", name, "dump"})
	if err != nil {
		if ifaceMissing(err, string(res.Stderr)) {
			return tunnel.InterfaceState{}, tunnel.ErrInterfaceNotFound
		}
		return tunnel.InterfaceState{}, fmt.Errorf("amneziawg: dump %s: %w", name, err)
	}
	return parseDump(name, res.Stdout)
}

// ifaceMissing classifies `awg show <name>` failures on absent interfaces.
// The CLI exits 1 with "Unable to access interface: …" on stderr (wg-tools
// behavior, amneziawg-tools is a direct fork); matching by substring is
// necessarily fragile and is documented as such.
func ifaceMissing(err error, stderr string) bool {
	var ee *subprocess.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	return strings.Contains(stderr, "Unable to access interface")
}

func (b *Backend) applySetconf(ctx context.Context, name string, cfg tunnel.InterfaceConfig) error {
	path, cleanup, err := writeTempConfig(renderSetconf(cfg))
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := b.run.Run(ctx, []string{b.awg, "setconf", name, path}); err != nil {
		return fmt.Errorf("amneziawg: setconf %s: %w", name, err)
	}
	return nil
}

// writeTempConfig writes one 0600 config file (private keys inside) next to
// nothing persistent — os.TempDir — and returns its cleanup. The file exists
// only for the duration of the single CLI call that reads it.
func writeTempConfig(data []byte) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "wg-guard-awg-*.conf")
	if err != nil {
		return "", nil, fmt.Errorf("amneziawg: temp config: %w", err)
	}
	path = f.Name()
	cleanup = func() {
		f.Close()
		os.Remove(path)
	}
	if err := f.Chmod(0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("amneziawg: temp config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("amneziawg: temp config: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", nil, fmt.Errorf("amneziawg: temp config: %w", err)
	}
	return path, cleanup, nil
}
