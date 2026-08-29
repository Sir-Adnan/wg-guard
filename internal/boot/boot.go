// Package boot brings the node to the state described by the database —
// the single orchestration used at service start (`serve`) and on demand
// from the CLI (`wg-guard reconcile`). Sequence per
// docs/architecture/networking.md: verify tooling, enable IPv4 forwarding,
// reconcile tunnels and peers, apply the namespaced firewall table, restore
// speed limits (tc), and handle firewall-manager coexistence. Failures on
// essential steps (tooling, forwarding, reconcile, firewall) abort bring-up;
// shaper and coexistence problems are surfaced as findings and never abort.
package boot

import (
	"context"
	"fmt"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/firewall"
	"github.com/Sir-Adnan/wg-guard/internal/network"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/amneziawg"
)

// Deps wires the collaborators boot needs (owned by the caller so tests can
// substitute fakes).
type Deps struct {
	DB       *database.DB
	Ring     *secrets.KeyRing
	Settings *settings.Registry
	Backend  tunnel.Backend
	Run      subprocess.Runner
	Audit    *audit.Service
	Shaper   *shaper.Manager // nil = skip shaping (tests, minimal runs)
}

// Result reports what bring-up observed and changed. It carries no secret
// material and is safe to print or serialize.
type Result struct {
	ToolsVersion           string
	ToolsVersionMatchesPin bool
	ForwardingChanged      bool // net.ipv4.ip_forward was off and is now on
	Reconcile              *reconcile.Report
	ManagedIfaces          int
	ShapedGroups           int      // (user, interface) pairs with speed limits
	UfwRoutes              []string // interfaces the ufw route rule was added for
	Findings               []firewall.Finding
}

// BringUp runs the full sequence and records an audit entry.
func BringUp(ctx context.Context, d Deps) (*Result, error) {
	res := &Result{}

	links := &network.Links{Run: d.Run}

	// 1. Tooling: the pinned awg CLI must be present. Version drift is
	// reported, not fatal (the CLI surface is stable).
	v, err := toolsVersion(ctx, d.Backend)
	if err != nil {
		return nil, fmt.Errorf("boot: probe awg: %w", err)
	}
	res.ToolsVersion = v
	res.ToolsVersionMatchesPin = v == amneziawg.PinnedToolsVersion

	// 2. Forwarding (idempotent).
	_, changed, err := links.EnsureIPForwarding(ctx)
	if err != nil {
		return nil, fmt.Errorf("boot: ip forwarding: %w", err)
	}
	res.ForwardingChanged = changed

	// 3. Tunnels + peers to DB state.
	policy, err := d.Settings.GetString(ctx, "drift.policy")
	if err != nil {
		return nil, fmt.Errorf("boot: drift policy: %w", err)
	}
	engine := &reconcile.Engine{
		DB:      d.DB,
		Backend: d.Backend,
		Ring:    d.Ring,
		Policy:  reconcile.Policy(strings.TrimSpace(policy)),
	}
	rep, err := engine.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("boot: reconcile: %w", err)
	}
	res.Reconcile = rep

	// 4. Firewall: the table content is rendered from enabled interfaces.
	ifaces, err := enabledInterfaces(ctx, d.DB)
	if err != nil {
		return nil, err
	}
	res.ManagedIfaces = len(ifaces)
	fw := &firewall.Manager{Run: d.Run}
	if err := fw.Apply(ctx, ifaces); err != nil {
		return nil, fmt.Errorf("boot: firewall: %w", err)
	}

	// 5. Speed limits (tc): restore shaping from DB state. Never fatal — a
	// broken tc must not take tunnels down; it is reported as a finding.
	// (The accounting cycle re-ensures with change detection afterwards.)
	if d.Shaper != nil {
		groups, err := shaper.LoadGroups(ctx, d.DB)
		if err != nil {
			res.Findings = append(res.Findings, shaperFinding(err.Error()))
		} else {
			res.ShapedGroups = len(groups)
			byIface := map[string][]shaper.Group{}
			for _, g := range groups {
				byIface[g.InterfaceName] = append(byIface[g.InterfaceName], g)
			}
			for _, ifc := range ifaces {
				if _, err := d.Shaper.Ensure(ctx, ifc.Name, byIface[ifc.Name]); err != nil {
					res.Findings = append(res.Findings, shaperFinding(err.Error()))
					break
				}
			}
		}
	}

	// 6. Coexistence: add the ufw forward-allow rule when ufw runs
	// (idempotent, scoped to our interfaces); gather findings for the
	// operator. Never fatal.
	routes, err := fw.EnsureUfwRoutes(ctx, ifaces)
	if err != nil {
		routes = nil // finding-quality issue; the table itself is applied
	} else {
		res.UfwRoutes = routes
	}
	findings, ferr := fw.Coexistence(ctx)
	if ferr == nil {
		res.Findings = append(res.Findings, findings...)
	}

	if d.Audit != nil {
		_ = d.Audit.Record(ctx, audit.Entry{
			ActorType: "system",
			Action:    "node.reconcile",
			Target:    "node",
			Metadata: map[string]any{
				"interfaces_created": rep.InterfacesCreated,
				"interfaces_updated": rep.InterfacesUpdated,
				"interfaces_removed": rep.InterfacesRemoved,
				"peers_added":        rep.PeersAdded,
				"peers_removed":      rep.PeersRemoved,
				"peers_updated":      rep.PeersUpdated,
				"drift_items":        len(rep.Drift),
				"forwarding_changed": changed,
				"fw_ifaces":          len(ifaces),
				"shaped_groups":      res.ShapedGroups,
			},
		})
	}
	return res, nil
}

// shaperFinding reports a speed-limit enforcement problem as an advisory
// finding: tunnels run unshaped rather than bring-up failing.
func shaperFinding(detail string) firewall.Finding {
	return firewall.Finding{
		Tool:     "tc",
		Active:   true,
		Blocking: true,
		Detail:   detail,
		Remedy:   "install iproute2 (tc) — or remove the configured speed limits",
	}
}

// toolsVersion probes through the backend when it supports it; fake backends
// (tests, dev without the CLI) report the pinned version so bring-up can run
// without the tooling installed.
func toolsVersion(ctx context.Context, b tunnel.Backend) (string, error) {
	if prober, ok := b.(interface {
		ToolsVersion(context.Context) (string, error)
	}); ok {
		return prober.ToolsVersion(ctx)
	}
	// Backends without a version probe (fake, dev without the CLI) report
	// the pinned version so bring-up can run without the tooling installed.
	return amneziawg.PinnedToolsVersion, nil
}

// enabledInterfaces lists the forwarding footprint for the firewall: every
// enabled interface's name and device pool.
func enabledInterfaces(ctx context.Context, db *database.DB) ([]firewall.Interface, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name, ipv4_subnet FROM tunnel_interfaces WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("boot: load enabled interfaces: %w", err)
	}
	defer rows.Close()
	var out []firewall.Interface
	for rows.Next() {
		var ifc firewall.Interface
		if err := rows.Scan(&ifc.Name, &ifc.Subnet); err != nil {
			return nil, fmt.Errorf("boot: scan enabled interfaces: %w", err)
		}
		out = append(out, ifc)
	}
	return out, rows.Err()
}
