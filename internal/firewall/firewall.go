// Package firewall owns the namespaced nftables table `table inet wgguard`
// (ADR-0004): one forward-accept chain (priority 10, after standard filter
// chains) and one srcnat masquerade chain. The table is applied as rendered
// state — its full content is a pure function of the enabled interfaces — so
// re-applying is idempotent and the uninstaller deletes exactly this table.
//
// Hard rules (docs/architecture/networking.md): WG-Guard never flushes or
// edits foreign tables, never sets global policies, and no shell
// interpolation is used — `nft -f <file>` receives a generated script, which
// the kernel applies as a single transaction.
package firewall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// TableName is the single namespaced table WG-Guard owns.
const TableName = "inet wgguard"

// Interface is one managed tunnel's forwarding footprint.
type Interface struct {
	Name   string
	Subnet string // device pool CIDR, e.g. "10.8.0.0/24"
}

// Manager drives the `nft` CLI through subprocess.Runner.
type Manager struct {
	Run subprocess.Runner
}

// Present probes for the wgguard table.
func (m *Manager) Present(ctx context.Context) (bool, error) {
	if err := m.nftAvailable(); err != nil {
		return false, err
	}
	_, err := m.Run.Run(ctx, []string{"nft", "list", "table", TableName})
	if err == nil {
		return true, nil
	}
	var ee *subprocess.ExitError
	if errors.As(err, &ee) {
		// nft exits non-zero for "no such table"; any other failure surfaces
		// on Apply anyway.
		return false, nil
	}
	return false, fmt.Errorf("firewall: probe %s: %w", TableName, err)
}

// Apply renders the desired table and applies it atomically. With zero
// interfaces the desired state is "no table" (Remove).
func (m *Manager) Apply(ctx context.Context, ifaces []Interface) error {
	if len(ifaces) == 0 {
		return m.Remove(ctx)
	}
	present, err := m.Present(ctx) // includes the nft availability check
	if err != nil {
		return err
	}
	script := renderTable(ifaces, present)
	f, err := os.CreateTemp("", "wg-guard-nft-*.nft")
	if err != nil {
		return fmt.Errorf("firewall: temp script: %w", err)
	}
	path := f.Name()
	defer func() {
		f.Close()
		os.Remove(path)
	}()
	if _, err := f.Write(script); err != nil {
		return fmt.Errorf("firewall: temp script: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("firewall: temp script: %w", err)
	}
	if _, err := m.Run.Run(ctx, []string{"nft", "-f", path}); err != nil {
		return fmt.Errorf("firewall: apply %s: %w", TableName, err)
	}
	return nil
}

// Remove deletes the wgguard table; removing an absent table is success
// (idempotent uninstall).
func (m *Manager) Remove(ctx context.Context) error {
	if err := m.nftAvailable(); err != nil {
		return err
	}
	_, err := m.Run.Run(ctx, []string{"nft", "delete", "table", TableName})
	if err != nil {
		var ee *subprocess.ExitError
		if errors.As(err, &ee) {
			return nil // already absent — goal state reached
		}
		return fmt.Errorf("firewall: remove %s: %w", TableName, err)
	}
	return nil
}

func (m *Manager) nftAvailable() error {
	if _, err := m.Run.Run(context.Background(), []string{"nft", "--version"}); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("firewall: nftables (nft) is not installed")
		}
		return fmt.Errorf("firewall: nft probe: %w", err)
	}
	return nil
}

// renderTable renders the complete `nft -f` script. dropExisting is set when
// the probe found the table (delete + recreate in one atomic transaction —
// plain re-application would merge rules and duplicate them).
//
// Priorities are numeric: forward chain 10 (== "filter + 10", so host
// firewall chains evaluate first), postrouting 100 (== srcnat). Chain
// policies are accept: a base chain requires one, and drop here would be a
// global policy — exactly what ADR-0004 forbids.
func renderTable(ifaces []Interface, dropExisting bool) []byte {
	var sb strings.Builder
	if dropExisting {
		sb.WriteString("delete table " + TableName + "\n")
	}
	sb.WriteString("table " + TableName + " {\n")
	sb.WriteString("\tchain forward {\n")
	sb.WriteString("\t\ttype filter hook forward priority 10; policy accept;\n")
	for _, ifc := range ifaces {
		sb.WriteString(fmt.Sprintf("\t\tiifname %q accept comment %q\n", ifc.Name, managedComment(ifc.Name)))
		sb.WriteString(fmt.Sprintf("\t\toifname %q accept comment %q\n", ifc.Name, managedComment(ifc.Name)))
	}
	sb.WriteString("\t}\n")
	sb.WriteString("\tchain postrouting {\n")
	sb.WriteString("\t\ttype nat hook postrouting priority 100; policy accept;\n")
	// Masquerade tunnel egress on any non-tunnel interface — the wg-quick
	// pattern, so multi-WAN hosts and interface renames need no re-config.
	// Tunnel-to-tunnel traffic is masqueraded too (same as wg-quick);
	// per-profile isolation stays a possible future setting.
	for _, ifc := range ifaces {
		sb.WriteString(fmt.Sprintf("\t\toifname != %q ip saddr %s masquerade comment %q\n",
			ifc.Name, ifc.Subnet, managedComment(ifc.Name)))
	}
	sb.WriteString("\t}\n")
	sb.WriteString("}\n")
	return []byte(sb.String())
}

func managedComment(iface string) string { return "wgguard:managed:" + iface }
