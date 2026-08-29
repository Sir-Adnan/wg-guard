package firewall

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Finding reports one third-party firewall manager that interacts with
// forwarded tunnel traffic (docs/architecture/networking.md: ufw's default
// forward DROP is the most common "installed fine, no traffic" failure).
type Finding struct {
	Tool     string // ufw | firewalld
	Active   bool
	Blocking bool // forward policy likely drops tunnel traffic before our rules
	Detail   string
	Remedy   string // exact, copy-pasteable remedy
}

// Coexistence probes ufw and firewalld and reports findings only for
// *running* managers (an installed-but-inactive one cannot block forwarding).
// A missing binary means the tool is not installed — no finding, not an error.
func (m *Manager) Coexistence(ctx context.Context) ([]Finding, error) {
	var out []Finding

	if active, detail, _, err := ufwStatus(m.Run, ctx); err != nil {
		return nil, err
	} else if active {
		out = append(out, Finding{
			Tool: "ufw", Active: true, Blocking: detail != "",
			Detail: detail,
			Remedy: "ufw route allow in on <tunnel-iface>   # or run: wg-guard reconcile (applies it)",
		})
	}

	if active, ok, err := firewalldStatus(m.Run, ctx); err != nil {
		return nil, err
	} else if ok {
		out = append(out, Finding{
			Tool: "firewalld", Active: active,
			Detail: "firewalld manages the host zones",
			Remedy: "add tunnel interfaces to a zone with masquerade, e.g. firewall-cmd --zone=public --add-masquerade --permanent",
		})
	}
	return out, nil
}

// EnsureUfwRoutes adds the ufw forward-allow rule for each managed interface
// when ufw is active (idempotent: `ufw route allow` on an existing rule
// exits 0 with "Skipping adding existing rule"). The rule is additive and
// scoped to WG-Guard's own interfaces, so applying it unconditionally when
// ufw runs is safe and keeps forwarding alive regardless of the routed
// policy (networking.md sanctions adding it through the framework).
func (m *Manager) EnsureUfwRoutes(ctx context.Context, ifaces []Interface) (applied []string, err error) {
	active, _, ok, err := ufwStatus(m.Run, ctx)
	if err != nil || !ok || !active {
		return nil, err
	}
	for _, ifc := range ifaces {
		if _, err := m.Run.Run(ctx, []string{"ufw", "route", "allow", "in", "on", ifc.Name}); err != nil {
			return applied, fmt.Errorf("firewall: ufw route allow for %s: %w", ifc.Name, err)
		}
		applied = append(applied, ifc.Name)
	}
	return applied, nil
}

// ufwStatus reports (active, blockingPolicyDetail, installed, err).
// blockingPolicyDetail is non-empty when the routed (forward) default policy
// parses as deny/reject/drop.
func ufwStatus(run subprocess.Runner, ctx context.Context) (active bool, blockingDetail string, installed bool, err error) {
	res, rerr := run.Run(ctx, []string{"ufw", "status", "verbose"})
	if rerr != nil {
		var ee *subprocess.ExitError
		if errors.As(rerr, &ee) {
			return false, "", false, nil // not installed or not runnable
		}
		return false, "", false, fmt.Errorf("firewall: ufw status: %w", rerr)
	}
	out := string(res.Stdout)
	if !strings.Contains(out, "Status: active") {
		return false, "", true, nil
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "Default:") && routedPolicyBlocked(ln) {
			return true, strings.TrimSpace(ln), true, nil
		}
	}
	return true, "", true, nil
}

// routedPolicyBlocked inspects ufw's "Default: … (routed)" segment. Values
// seen upstream: deny / reject / allow / disabled. deny|reject|drop forward
// tunnel traffic before the wgguard table sees it. Unparseable output stays
// non-blocking here — the ensure-route call runs regardless.
func routedPolicyBlocked(defaultLine string) bool {
	seg := defaultLine[strings.Index(defaultLine, "Default:")+len("Default:"):]
	for _, part := range strings.Split(seg, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasSuffix(part, "(routed)") && part != "(routed)" {
			continue
		}
		policy := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(part), "(routed)"))
		switch policy {
		case "deny", "reject", "drop":
			return true
		}
	}
	return false
}

// firewalldStatus reports (running, installed, err).
func firewalldStatus(run subprocess.Runner, ctx context.Context) (running bool, installed bool, err error) {
	_, rerr := run.Run(ctx, []string{"firewall-cmd", "--state"})
	if rerr != nil {
		var ee *subprocess.ExitError
		if errors.As(rerr, &ee) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("firewall: firewalld state: %w", rerr)
	}
	return true, true, nil
}
