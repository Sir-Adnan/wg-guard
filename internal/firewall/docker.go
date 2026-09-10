package firewall

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

const (
	dockerUserChain       = "DOCKER-USER"
	dockerForwardingChain = "WGGUARD-FORWARD"
	dockerJumpComment     = "wgguard:managed:docker-forward"
)

// ForwardingStatus is the effective legacy-filter policy observed after
// manager coexistence has been applied.
type ForwardingStatus struct {
	Policy  string // ACCEPT, DROP, or unknown when iptables is unavailable
	Managed bool   // true when no bypass is needed or a supported manager owns it
	Source  string // policy, docker, ufw, or unavailable
}

// ForwardingInspection is the read-only state used by doctor.
type ForwardingInspection struct {
	Policy        string
	DockerUser    bool
	DockerManaged bool
}

// EnsureDockerForwarding installs a narrow accept path before Docker's
// default FORWARD drop. Docker documents DOCKER-USER as the extension point
// for host-routing policy; WG-Guard owns only its child chain and one tagged
// jump. Hosts without that chain keep using the independent nftables table.
func (m *Manager) EnsureDockerForwarding(ctx context.Context, ifaces []Interface) (bool, error) {
	if _, err := m.Run.Run(ctx, []string{"iptables", "--version"}); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("firewall: probe iptables: %w", err)
	}

	res, err := m.Run.Run(ctx, iptablesArgs("-S", dockerUserChain))
	if err != nil {
		if missingIPTablesObject(res, err) {
			return false, nil
		}
		return false, fmt.Errorf("firewall: probe %s: %w", dockerUserChain, err)
	}
	if len(ifaces) == 0 {
		return true, m.RemoveDockerForwarding(ctx)
	}

	res, err = m.Run.Run(ctx, iptablesArgs("-S", dockerForwardingChain))
	var current [][]string
	if err != nil {
		if !missingIPTablesObject(res, err) {
			return false, fmt.Errorf("firewall: probe %s: %w", dockerForwardingChain, err)
		}
		if _, err := m.Run.Run(ctx, iptablesArgs("-N", dockerForwardingChain)); err != nil {
			return false, fmt.Errorf("firewall: create %s: %w", dockerForwardingChain, err)
		}
	} else if current, err = parseDockerForwardingRules(res.Stdout); err != nil {
		return false, err
	}

	desired := dockerForwardingRules(ifaces)
	remaining := make(map[string]int, len(current))
	for _, rule := range current {
		remaining[strings.Join(rule, " ")]++
	}
	// Add replacements before removing stale rules so a live update never
	// opens a forwarding blackout window.
	for _, rule := range desired {
		key := strings.Join(rule, " ")
		if remaining[key] > 0 {
			remaining[key]--
			continue
		}
		if _, err := m.Run.Run(ctx, iptablesArgs(rule...)); err != nil {
			return false, fmt.Errorf("firewall: add Docker forwarding rule: %w", err)
		}
	}
	for _, rule := range current {
		key := strings.Join(rule, " ")
		if remaining[key] == 0 {
			continue
		}
		remaining[key]--
		remove := append([]string(nil), rule...)
		remove[0] = "-D"
		if _, err := m.Run.Run(ctx, iptablesArgs(remove...)); err != nil {
			return false, fmt.Errorf("firewall: remove stale Docker forwarding rule: %w", err)
		}
	}

	jump := []string{"-m", "comment", "--comment", dockerJumpComment, "-j", dockerForwardingChain}
	if _, err := m.Run.Run(ctx, iptablesArgs(append([]string{"-C", dockerUserChain}, jump...)...)); err != nil {
		if _, err := m.Run.Run(ctx, iptablesArgs(append([]string{"-I", dockerUserChain, "1"}, jump...)...)); err != nil {
			return false, fmt.Errorf("firewall: attach %s to %s: %w", dockerForwardingChain, dockerUserChain, err)
		}
	}
	return true, nil
}

// CheckForwarding prevents a healthy-looking node when an earlier legacy
// FORWARD policy drops packets before WG-Guard's nftables base chain. A DROP
// is accepted only when the boot pass just installed a complete supported
// Docker or UFW route for every enabled interface.
func (m *Manager) CheckForwarding(ctx context.Context, ifaces []Interface, dockerManaged bool, ufwRoutes []string) (ForwardingStatus, error) {
	status := ForwardingStatus{Policy: "unknown", Source: "unavailable"}
	if len(ifaces) == 0 {
		status.Managed = true
		return status, nil
	}
	policy, available, err := m.forwardPolicy(ctx)
	if err != nil {
		return status, err
	}
	if !available {
		return status, nil
	}
	status.Policy = policy
	if status.Policy != "DROP" {
		status.Managed = true
		status.Source = "policy"
		return status, nil
	}
	if dockerManaged {
		status.Managed = true
		status.Source = "docker"
		return status, nil
	}
	if routesCoverInterfaces(ifaces, ufwRoutes) {
		status.Managed = true
		status.Source = "ufw"
		return status, nil
	}
	return status, fmt.Errorf("firewall: host FORWARD policy is DROP and no supported scoped allow path covers every WG-Guard interface")
}

// InspectForwarding reports whether the complete owned Docker path exists;
// it never mutates firewall state.
func (m *Manager) InspectForwarding(ctx context.Context, ifaces []Interface) (ForwardingInspection, error) {
	inspection := ForwardingInspection{Policy: "unknown"}
	policy, available, err := m.forwardPolicy(ctx)
	if err != nil || !available {
		return inspection, err
	}
	inspection.Policy = policy
	res, err := m.Run.Run(ctx, iptablesArgs("-S", dockerUserChain))
	if err != nil {
		if missingIPTablesObject(res, err) {
			return inspection, nil
		}
		return inspection, fmt.Errorf("firewall: inspect %s: %w", dockerUserChain, err)
	}
	inspection.DockerUser = true
	jump := []string{"-m", "comment", "--comment", dockerJumpComment, "-j", dockerForwardingChain}
	if _, err := m.Run.Run(ctx, iptablesArgs(append([]string{"-C", dockerUserChain}, jump...)...)); err != nil {
		var exitErr *subprocess.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode == 1 {
			return inspection, nil
		}
		return inspection, fmt.Errorf("firewall: inspect Docker forwarding jump: %w", err)
	}
	res, err = m.Run.Run(ctx, iptablesArgs("-S", dockerForwardingChain))
	if err != nil {
		if missingIPTablesObject(res, err) {
			return inspection, nil
		}
		return inspection, fmt.Errorf("firewall: inspect %s: %w", dockerForwardingChain, err)
	}
	current, err := parseDockerForwardingRules(res.Stdout)
	if err != nil {
		return inspection, err
	}
	inspection.DockerManaged = sameDockerRules(current, dockerForwardingRules(ifaces))
	return inspection, nil
}

func (m *Manager) forwardPolicy(ctx context.Context) (string, bool, error) {
	if _, err := m.Run.Run(ctx, []string{"iptables", "--version"}); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "unknown", false, nil
		}
		return "unknown", false, fmt.Errorf("firewall: probe iptables policy: %w", err)
	}
	res, err := m.Run.Run(ctx, iptablesArgs("-S", "FORWARD"))
	if err != nil {
		return "unknown", true, fmt.Errorf("firewall: read FORWARD policy: %w", err)
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "-P" && fields[1] == "FORWARD" {
			return strings.ToUpper(fields[2]), true, nil
		}
	}
	return "unknown", true, nil
}

func sameDockerRules(current, desired [][]string) bool {
	counts := make(map[string]int, len(current))
	for _, rule := range current {
		counts[strings.Join(rule, " ")]++
	}
	for _, rule := range desired {
		key := strings.Join(rule, " ")
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func routesCoverInterfaces(ifaces []Interface, routes []string) bool {
	covered := make(map[string]bool, len(routes))
	for _, name := range routes {
		covered[name] = true
	}
	for _, ifc := range ifaces {
		if !covered[ifc.Name] {
			return false
		}
	}
	return len(ifaces) > 0
}

func dockerForwardingRules(ifaces []Interface) [][]string {
	rules := make([][]string, 0, len(ifaces)*2)
	for _, ifc := range ifaces {
		rules = append(rules,
			[]string{"-A", dockerForwardingChain, "-s", ifc.Subnet, "-i", ifc.Name, "-j", "ACCEPT"},
			[]string{"-A", dockerForwardingChain, "-d", ifc.Subnet, "-o", ifc.Name,
				"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		)
	}
	return rules
}

func parseDockerForwardingRules(output []byte) ([][]string, error) {
	var rules [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "-N "+dockerForwardingChain {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "-A" || fields[1] != dockerForwardingChain {
			return nil, fmt.Errorf("firewall: unexpected rule in owned %s chain", dockerForwardingChain)
		}
		rules = append(rules, fields)
	}
	return rules, nil
}

// RemoveDockerForwarding removes only WG-Guard's tagged jump and owned child
// chain. It is safe when Docker, iptables, the parent chain, or the child
// chain is absent.
func (m *Manager) RemoveDockerForwarding(ctx context.Context) error {
	if _, err := m.Run.Run(ctx, []string{"iptables", "--version"}); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("firewall: probe iptables cleanup: %w", err)
	}
	jump := []string{"-m", "comment", "--comment", dockerJumpComment, "-j", dockerForwardingChain}
	if _, err := m.Run.Run(ctx, iptablesArgs(append([]string{"-C", dockerUserChain}, jump...)...)); err == nil {
		if _, err := m.Run.Run(ctx, iptablesArgs(append([]string{"-D", dockerUserChain}, jump...)...)); err != nil {
			return fmt.Errorf("firewall: detach %s: %w", dockerForwardingChain, err)
		}
	}
	res, err := m.Run.Run(ctx, iptablesArgs("-S", dockerForwardingChain))
	if err != nil {
		if missingIPTablesObject(res, err) {
			return nil
		}
		return fmt.Errorf("firewall: probe %s cleanup: %w", dockerForwardingChain, err)
	}
	if _, err := m.Run.Run(ctx, iptablesArgs("-F", dockerForwardingChain)); err != nil {
		return fmt.Errorf("firewall: clear %s: %w", dockerForwardingChain, err)
	}
	if _, err := m.Run.Run(ctx, iptablesArgs("-X", dockerForwardingChain)); err != nil {
		return fmt.Errorf("firewall: remove %s: %w", dockerForwardingChain, err)
	}
	return nil
}

func iptablesArgs(args ...string) []string {
	return append([]string{"iptables", "-w", "5"}, args...)
}

func missingIPTablesObject(res subprocess.Result, err error) bool {
	var exitErr *subprocess.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	message := strings.ToLower(string(res.Stderr) + " " + exitErr.Stderr)
	return strings.Contains(message, "no chain/target/match") ||
		strings.Contains(message, "does not exist")
}
