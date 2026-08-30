// Package shaper applies speed limits with Linux tc (HTB), independently per
// direction (docs/architecture/networking.md §Shaping): download
// (server→client) on the tunnel interface EGRESS, upload (client→server) via
// an IFB-mirrored INGRESS qdisc. One class per user per interface per
// direction — the limit is a *user* field, so all of that user's device IPs
// share the class (aggregate enforcement) — with one u32 filter per device IP.
// Unclassified traffic flows unshaped (HTB `default 0`, unfiltered ingress),
// so shaping one user never degrades another.
//
// Direction independence: a group with only a download limit touches the
// egress tree only; upload-only groups touch the ingress tree only. The
// ingress tree needs the `ifb` kernel device (`ip link add <name> type ifb`);
// when that is unavailable the error is confined to the upload direction —
// download limits stay enforced and the failure surfaces as a boot/cycle
// finding instead of being silently ignored.
//
// Applies are rendered-state: each direction's command list is a pure
// function of the desired groups; a rebuild deletes the tree (egress root
// qdisc; ingress qdisc + IFB root + IFB link on cleanup) and recreates it in
// one `tc -b` batch, so re-applying never duplicates rules. The manager
// re-applies a direction only when its rendered script changes; the first
// Ensure per process lifetime always applies — the restart-recovery path for
// rules a crashed previous run may have left behind. iproute2 facts live-
// verified in WSL2 are pinned in docs/architecture/networking.md (`qdisc
// replace` is rejected by HTB; `tc qdisc del dev X ingress` exits 0 even when
// absent; ifb links are ordinary netlink devices on this kernel).
//
// Ensure is not safe for concurrent calls on the same interface; the
// composition (boot bring-up, then the single accounting cycle) serializes it.
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
// class per direction. A Kbps of 0 means that direction is unlimited; a group
// exists only when at least one direction is limited.
type Group struct {
	InterfaceName string
	UserID        string // class owner identity (also the render ordering key)
	IPs           []string
	DownKbps      int // server→client (tc egress); 0 = unlimited
	UpKbps        int // client→server (tc ingress/IFB); 0 = unlimited
}

// Manager renders and applies tc state.
type Manager struct {
	Run subprocess.Runner

	mu      sync.Mutex
	applied map[string]dirState // interface → last applied render per direction
	ensured map[string]bool     // interfaces ensured at least once this process
}

// dirState is the per-direction applied render ("" = no tree).
type dirState struct {
	egress  string
	ingress string
}

// New returns a Manager.
func New(run subprocess.Runner) *Manager {
	return &Manager{Run: run, applied: map[string]dirState{}, ensured: map[string]bool{}}
}

// IFBName is the mirror device used for a tunnel interface's upload shaping.
// "ifb-awgN" (≤ 15 chars, unique per interface, stable across restarts).
func IFBName(iface string) string { return "ifb-" + iface }

// Ensure reconciles shaping on one interface to the desired groups. It
// returns true when kernel state was touched. Missing tc/ip binaries are a
// hard error only when limits are desired (an unenforced limit must never
// look enforced); cleanup on a tc-less host is skipped silently — a leftover
// qdisc is inert and visible in `tc qdisc show`.
func (m *Manager) Ensure(ctx context.Context, iface string, groups []Group) (bool, error) {
	egress, err := RenderEgress(iface, groups)
	if err != nil {
		return false, err
	}
	ingress, err := RenderIngress(iface, groups)
	if err != nil {
		return false, err
	}
	ifb := IFBName(iface)

	m.mu.Lock()
	first := !m.ensured[iface]
	m.ensured[iface] = true
	prev := m.applied[iface]
	chgEgress := first || prev.egress != egress
	chgIngress := first || prev.ingress != ingress
	m.mu.Unlock()
	if !chgEgress && !chgIngress {
		return false, nil
	}

	now := dirState{egress: prev.egress, ingress: prev.ingress}
	var firstErr error
	if chgEgress {
		if err := m.applyEgress(ctx, iface, egress); err != nil {
			firstErr = err
		} else {
			now.egress = egress
		}
	}
	if chgIngress {
		if err := m.applyIngress(ctx, iface, ifb, ingress); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			now.ingress = ingress
		}
	}
	m.mu.Lock()
	m.applied[iface] = now
	m.mu.Unlock()
	return now != prev, firstErr
}

// applyEgress rebuilds (or, with an empty script, removes) the egress HTB
// tree. Deleting an absent root qdisc errors — indistinguishable from a real
// failure at acceptable cost, so the error is ignored (pinned fact).
func (m *Manager) applyEgress(ctx context.Context, iface, script string) error {
	_, err := m.Run.Run(ctx, []string{"tc", "qdisc", "del", "dev", iface, "root"})
	if err != nil && errors.Is(err, exec.ErrNotFound) && script == "" {
		return nil // tc-less host, nothing to enforce or clean
	}
	if script == "" {
		return nil
	}
	if err := m.applyBatch(script); err != nil {
		return fmt.Errorf("shaper: apply egress on %s: %w", iface, err)
	}
	return nil
}

// applyIngress rebuilds (or removes) the ingress mirror: the IFB device, the
// ingress qdisc with the mirred redirect, and the HTB tree on the IFB.
func (m *Manager) applyIngress(ctx context.Context, iface, ifb, script string) error {
	if script == "" {
		// `tc qdisc del dev X ingress` exits 0 even when absent (pinned);
		// the rest is best-effort host cleanup.
		_, _ = m.Run.Run(ctx, []string{"tc", "qdisc", "del", "dev", iface, "ingress"})
		_, _ = m.Run.Run(ctx, []string{"tc", "qdisc", "del", "dev", ifb, "root"})
		_, _ = m.Run.Run(ctx, []string{"ip", "link", "del", ifb})
		return nil
	}

	// IFB device: create when missing (the add error is only fatal when the
	// link does not exist afterwards — i.e. the ifb kernel support is absent),
	// then bring it up.
	_, err := m.Run.Run(ctx, []string{"ip", "link", "add", ifb, "type", "ifb"})
	if err != nil {
		if _, serr := m.Run.Run(ctx, []string{"ip", "link", "show", ifb}); serr != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return fmt.Errorf("shaper: iproute2 (ip) is not installed; upload limits cannot be enforced")
			}
			return fmt.Errorf("shaper: ifb device %s unavailable (kernel ifb support is required for upload limits): %w", ifb, err)
		}
	}
	if _, err := m.Run.Run(ctx, []string{"ip", "link", "set", ifb, "up"}); err != nil {
		return fmt.Errorf("shaper: ifb %s up: %w", ifb, err)
	}

	// Reset the previous tree (both dels tolerated when absent).
	_, _ = m.Run.Run(ctx, []string{"tc", "qdisc", "del", "dev", iface, "ingress"})
	_, _ = m.Run.Run(ctx, []string{"tc", "qdisc", "del", "dev", ifb, "root"})
	if err := m.applyBatch(script); err != nil {
		return fmt.Errorf("shaper: apply ingress on %s: %w", iface, err)
	}
	return nil
}

// applyBatch runs one `tc -b` batch from a 0600 temp file. The rebuild dels
// run outside the batch (argv, errors ignored) at the call sites. `qdisc
// replace` cannot be used: HTB does not support the change operation behind
// it ("Change operation not supported by specified qdisc", iproute2 — pinned).
// Between del and add a rebuild window is briefly unshaped — rebuilds are
// rare (configuration changes), so that is acceptable.
func (m *Manager) applyBatch(script string) error {
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
	_, err = m.Run.Run(context.Background(), []string{"tc", "-b", path})
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("shaper: tc (iproute2) is not installed; speed limits cannot be enforced")
	}
	return err
}

// sortedGroups returns the groups ordered by user ID with IPs sorted —
// identical desired state always renders identically.
func sortedGroups(groups []Group) []Group {
	out := append([]Group(nil), groups...)
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	for _, g := range out {
		sort.Strings(g.IPs)
	}
	return out
}

// RenderEgress produces the tc batch for the download (egress) tree on one
// interface: build the root HTB qdisc (the previous one is deleted by the
// caller), then one class per download-limited user and one filter per device
// IP. Class/filter IDs are assigned over the sorted group list. No
// download-limited groups renders "" — Ensure handles cleanup.
func RenderEgress(iface string, groups []Group) (string, error) {
	var limited []Group
	for _, g := range sortedGroups(groups) {
		if g.DownKbps > 0 {
			limited = append(limited, g)
		}
	}
	if len(limited) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("qdisc add dev %s root handle 1: htb default 0\n", iface))
	classID := 10
	pref := 100
	for _, g := range limited {
		ips := append([]string(nil), g.IPs...)
		sort.Strings(ips)
		sb.WriteString(fmt.Sprintf("class add dev %s parent 1: classid 1:%d htb rate %dkbit ceil %dkbit\n",
			iface, classID, g.DownKbps, g.DownKbps))
		for _, ip := range ips {
			sb.WriteString(fmt.Sprintf("filter add dev %s parent 1: protocol ip pref %d u32 match ip dst %s flowid 1:%d\n",
				iface, pref, ip, classID))
			pref++
		}
		classID++
	}
	return sb.String(), nil
}

// RenderIngress produces the tc batch for the upload (ingress) tree: the
// ingress qdisc on the tunnel interface, the mirred redirect into the IFB
// device, and the HTB tree on the IFB (classes match the client SOURCE
// address — the mirror of the egress design). No upload-limited groups
// renders "" — Ensure's cleanup path removes the tree.
func RenderIngress(iface string, groups []Group) (string, error) {
	ifb := IFBName(iface)
	var limited []Group
	for _, g := range sortedGroups(groups) {
		if g.UpKbps > 0 {
			limited = append(limited, g)
		}
	}
	if len(limited) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("qdisc add dev %s handle ffff: ingress\n", iface))
	sb.WriteString(fmt.Sprintf("filter add dev %s parent ffff: protocol ip pref 1 u32 match u32 0 0 action mirred egress redirect dev %s\n",
		iface, ifb))
	sb.WriteString(fmt.Sprintf("qdisc add dev %s root handle 1: htb default 0\n", ifb))
	classID := 10
	pref := 100
	for _, g := range limited {
		ips := append([]string(nil), g.IPs...)
		sort.Strings(ips)
		sb.WriteString(fmt.Sprintf("class add dev %s parent 1: classid 1:%d htb rate %dkbit ceil %dkbit\n",
			ifb, classID, g.UpKbps, g.UpKbps))
		for _, ip := range ips {
			sb.WriteString(fmt.Sprintf("filter add dev %s parent 1: protocol ip pref %d u32 match ip src %s flowid 1:%d\n",
				ifb, pref, ip, classID))
			pref++
		}
		classID++
	}
	return sb.String(), nil
}
