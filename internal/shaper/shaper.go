// Package shaper applies speed limits with Linux tc (HTB) on the tunnel
// interface egress (docs/architecture/networking.md §Shaping). One class per
// user per interface — the limit is a *user* field, so all of that user's
// device IPs on the interface share the class (aggregate enforcement) — with
// one u32 filter per device IP. Unclassified traffic (users without limits)
// flows through HTB's direct service unshaped (default 0), so shaping one
// user never degrades another.
//
// Applies are rendered-state: the command list is a pure function of the
// desired groups; a rebuild deletes the root qdisc (wiping classes and
// filters) and recreates it in one `tc -b` batch, so re-applying never
// duplicates rules. The manager re-applies only when the rendered script
// changes; the first Ensure for an interface in this process lifetime
// always applies, which is the restart-recovery path (a crashed previous
// run may have left rules behind). Upload (ingress) shaping is deliberately
// deferred per the same doc — Phase 3 shapes egress only.
//
// Ensure is not safe for concurrent calls on the same interface; the
// composition (boot bring-up, then the single accounting cycle) serializes
// it naturally.
package shaper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Group is one shaped user: their device IPs on one interface share one HTB
// class at the given rate.
type Group struct {
	InterfaceName string
	UserID        string // class owner identity (also the render ordering key)
	IPs           []string
	Kbps          int
}

// Manager renders and applies tc state.
type Manager struct {
	Run subprocess.Runner

	mu      sync.Mutex
	applied map[string]string // interface → last applied script
	ensured map[string]bool   // interfaces ensured at least once this process
}

// New returns a Manager.
func New(run subprocess.Runner) *Manager {
	return &Manager{Run: run, applied: map[string]string{}, ensured: map[string]bool{}}
}

// Ensure reconciles shaping on one interface to the desired groups. It
// returns true when kernel state was touched. An empty desired state removes
// the shaper (best-effort: this tc reports an error for deleting an absent
// qdisc, which is indistinguishable from a real failure at acceptable cost —
// a leftover qdisc is inert and visible in `tc qdisc show`). When no limits
// are desired and tc is not installed, cleanup is skipped silently; when
// limits ARE desired, a missing tc is a hard error — an unenforced limit
// would be a lie.
func (m *Manager) Ensure(ctx context.Context, iface string, groups []Group) (bool, error) {
	if len(groups) == 0 {
		m.mu.Lock()
		rec, ok := m.applied[iface]
		m.mu.Unlock()
		if ok && rec == "" {
			return false, nil // cleaned up already in this process
		}
		// Either limits existed and were removed (must clean now), or this
		// is the first pass in this process — restart recovery: a crashed
		// previous run may have left a qdisc (see doc comment on errors).
		_, err := m.Run.Run(ctx, []string{"tc", "qdisc", "del", "dev", iface, "root"})
		if err != nil && errors.Is(err, exec.ErrNotFound) {
			return false, nil // tc-less host: cleanup impossible, nothing to enforce
		}
		m.mu.Lock()
		m.applied[iface] = ""
		m.mu.Unlock()
		return true, nil
	}

	script, err := RenderScript(iface, groups)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	first := !m.ensured[iface]
	m.ensured[iface] = true
	changed := m.applied[iface] != script
	m.mu.Unlock()
	if !changed && !first {
		return false, nil
	}

	if err := m.applyBatch(ctx, iface, script); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, fmt.Errorf("shaper: tc (iproute2) is not installed; speed limits cannot be enforced")
		}
		return false, fmt.Errorf("shaper: apply on %s: %w", iface, err)
	}

	m.mu.Lock()
	m.applied[iface] = script
	m.mu.Unlock()
	return true, nil
}

// applyBatch rebuilds the shaper for one interface. The root qdisc is
// deleted first (a second del / absent qdisc is fine — the error is
// ignored), then the tree is built in one `tc -b` batch. `qdisc replace`
// cannot be used: HTB does not support the change operation behind it
// ("Change operation not supported by specified qdisc", iproute2). Between
// del and add a rebuild window is briefly unshaped — rebuilds are rare
// (configuration changes), so that is acceptable.
func (m *Manager) applyBatch(ctx context.Context, iface, script string) error {
	// Reset: remove any previous root qdisc (absent → error, ignored).
	_, _ = m.Run.Run(ctx, []string{"tc", "qdisc", "del", "dev", iface, "root"})

	f, err := os.CreateTemp("", "wg-guard-tc-*.batch")
	if err != nil {
		return fmt.Errorf("shaper: temp file: %w", err)
	}
	path := f.Name()
	defer func() {
		f.Close()
		os.Remove(path)
	}()
	if _, err := f.WriteString(script); err != nil {
		return fmt.Errorf("shaper: write batch: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("shaper: chmod batch: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("shaper: write batch: %w", err)
	}

	// Same choke-point contract as every other exec: explicit argv, bounded
	// by the Runner's timeout; only stderr (operational errors) can surface
	// in an error. The batch content has no secret material, but 0600 is the
	// house temp-file hygiene anyway.
	_, err = m.Run.Run(ctx, []string{"tc", "-b", path})
	return err
}

// RenderScript produces the tc batch for one interface: build the root HTB
// qdisc (the previous one is deleted by the caller), then one class per user
// and one filter per device IP. Class/filter IDs are assigned over the
// sorted group list, so identical desired state always renders identically.
// An empty desired state renders "" — Ensure handles cleanup itself.
func RenderScript(iface string, groups []Group) (string, error) {
	groups = append([]Group(nil), groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].UserID < groups[j].UserID
	})

	if len(groups) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("qdisc add dev %s root handle 1: htb default 0\n", iface))
	classID := 10
	pref := 100
	for _, g := range groups {
		if g.Kbps <= 0 {
			return "", fmt.Errorf("shaper: user %s on %s has no positive rate", g.UserID, iface)
		}
		ips := append([]string(nil), g.IPs...)
		sort.Strings(ips)
		sb.WriteString(fmt.Sprintf("class add dev %s parent 1: classid 1:%d htb rate %dkbit ceil %dkbit\n",
			iface, classID, g.Kbps, g.Kbps))
		for _, ip := range ips {
			sb.WriteString(fmt.Sprintf("filter add dev %s parent 1: protocol ip pref %d u32 match ip dst %s flowid 1:%d\n",
				iface, pref, ip, classID))
			pref++
		}
		classID++
	}
	return sb.String(), nil
}
