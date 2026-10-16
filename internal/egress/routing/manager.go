package routing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/egress/match"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// ResolverConfig is what the resolver needs from one plan.
type ResolverConfig struct {
	Gateways          []netip.Addr
	Ruleset           *match.Ruleset
	BlockEncryptedDNS bool
}

// Resolver is the built-in DNS resolver (dnsrouter.Server). Configure starts,
// reconfigures or stops it and reports whether it serves every gateway, which
// gates DNS redirection: client DNS is never redirected to a resolver that is
// not listening.
type Resolver interface {
	Configure(ctx context.Context, cfg ResolverConfig) bool
}

// Health thresholds: an outbound is down after this many consecutive failed
// probes and up again after one success.
const failuresDown = 2

// ProbeConcurrency bounds simultaneous health probes.
const ProbeConcurrency = 4

// OutboundStatus is the observed state of one outbound.
type OutboundStatus struct {
	ID            string
	Kind          egress.Kind
	Link          string
	Mark          uint32
	LinkUp        bool
	Mode          string // kernel | userspace (tunneled kinds)
	LinkError     string // fixed category
	Healthy       bool
	Probed        bool
	Failures      int
	Probe         ProbeResult
	LastHandshake time.Time
	Connections   int64 // active flows; counted for least_conn members
	Counted       bool
}

// Status is the latest applied routing state for the panel.
type Status struct {
	AppliedAt      time.Time
	Error          string // fixed category: snapshot | policy | table
	Warnings       []Warning
	ResolverWanted bool
	ResolverActive bool
	Classifying    bool
	Outbounds      map[string]OutboundStatus
	// Counted lists outbounds in enabled least_conn balancers.
	Counted []string
}

// Manager applies plans and tracks outbound health. It is safe for
// concurrent use; applies are serialized.
type Manager struct {
	Store    *egress.Service
	Backend  tunnel.Backend
	Run      subprocess.Runner
	Geo      Geo
	Resolver Resolver
	Prober   Prober
	// Counter counts active flows per outbound mark (least_conn); nil
	// disables counting and least_conn balances by weight only.
	Counter func(mark uint32) (int64, error)
	Log     *slog.Logger

	mu      sync.Mutex // serializes applies; guards applied, modes, ifaces, idle
	applied map[string]string
	modes   map[string]string
	ifaces  []Interface
	// idle records that an empty desired state was fully converged, so later
	// passes with nothing configured cost no subprocess.
	idle bool

	stateMu sync.RWMutex // guards health, load and status
	health  map[string]*healthEntry
	load    map[string]int64
	status  Status
}

type healthEntry struct {
	failures int
	probed   bool
	last     ProbeResult
}

func (m *Manager) logger() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

// Apply converges the kernel to the stored state for the given client
// tunnels. It is called by the canonical runtime pass (boot.BringUp) and is
// idempotent. Failures are reported in Status and returned; they never take
// client tunnels down.
func (m *Manager) Apply(ctx context.Context, ifaces []Interface) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ifaces = append([]Interface(nil), ifaces...)
	return m.apply(ctx)
}

// Reapply re-renders with the last known tunnels (health changes, panel
// edits that do not change tunnels).
func (m *Manager) Reapply(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apply(ctx)
}

func (m *Manager) apply(ctx context.Context) error {
	if m.applied == nil {
		m.applied, m.modes = map[string]string{}, map[string]string{}
	}
	st := Status{AppliedAt: time.Now().UTC(), Outbounds: map[string]OutboundStatus{}}
	snap, err := m.Store.LoadSnapshot(ctx)
	if err != nil {
		st.Error = "snapshot"
		m.setStatus(st)
		return fmt.Errorf("routing: load state: %w", err)
	}
	if snap.Empty() && m.idle {
		m.setStatus(st)
		return nil
	}
	in := Inputs{Health: m.healthSnapshot(), Load: m.loadSnapshot()}
	plan := Compile(snap, m.ifaces, in, m.Geo)
	linkErrs := m.syncLinks(ctx, plan)
	if len(linkErrs) > 0 {
		in.Down = map[string]bool{}
		for id := range linkErrs {
			in.Down[id] = true
		}
		plan = Compile(snap, m.ifaces, in, m.Geo)
	}
	var errs []error
	if m.Run != nil {
		if err := syncPolicy(ctx, m.Run, liveRoutes(plan, linkErrs)); err != nil {
			st.Error = "policy"
			errs = append(errs, err)
		}
	}
	resolverOK := false
	if m.Resolver != nil {
		cfg := ResolverConfig{BlockEncryptedDNS: plan.BlockEncryptedDNS}
		if plan.Resolves() {
			cfg.Gateways, cfg.Ruleset = plan.Gateways, plan.Ruleset
		}
		resolverOK = m.Resolver.Configure(ctx, cfg) && plan.Resolves()
	}
	if err := m.syncTable(ctx, plan, resolverOK); err != nil {
		st.Error = "table"
		errs = append(errs, err)
	}
	st.Warnings = plan.Warnings
	st.ResolverWanted, st.ResolverActive = plan.Resolves(), resolverOK
	st.Classifying = len(plan.Classify) > 0 || plan.DefaultChain != chainDirect
	countSet := map[string]bool{}
	for _, b := range snap.Balancers {
		if b.Enabled && b.Strategy == egress.StrategyLeastConn {
			for _, mem := range b.Members {
				if !countSet[mem.OutboundID] {
					countSet[mem.OutboundID] = true
					st.Counted = append(st.Counted, mem.OutboundID)
				}
			}
		}
	}
	linked := map[string]LinkSpec{}
	for _, l := range plan.Links {
		linked[l.OutboundID] = l
	}
	m.stateMu.RLock()
	for _, o := range snap.Outbounds {
		cur := OutboundStatus{ID: o.ID, Kind: o.Kind, Mark: SlotMark(o.Slot)}
		if l, ok := linked[o.ID]; ok {
			cur.Link, cur.Mode = l.Name, m.modes[l.Name]
			cur.LinkUp = linkErrs[o.ID] == ""
			cur.LinkError = linkErrs[o.ID]
		}
		if h := m.health[o.ID]; h != nil {
			cur.Probed, cur.Failures, cur.Probe = h.probed, h.failures, h.last
		}
		cur.Healthy = o.Enabled && cur.LinkError == "" && cur.Failures < failuresDown
		if n, ok := m.load[o.ID]; ok && countSet[o.ID] {
			cur.Connections, cur.Counted = n, true
		}
		st.Outbounds[o.ID] = cur
	}
	m.stateMu.RUnlock()
	m.setStatus(st)
	m.idle = snap.Empty() && len(errs) == 0 && len(linkErrs) == 0
	return errors.Join(errs...)
}

// liveRoutes drops routes whose link failed to apply: a route to a missing or
// down link is rejected by the kernel and would fail the whole policy pass.
func liveRoutes(plan *Plan, linkErrs map[string]string) []RouteSpec {
	if len(linkErrs) == 0 {
		return plan.Routes
	}
	failed := map[string]bool{}
	for _, l := range plan.Links {
		if linkErrs[l.OutboundID] != "" {
			failed[l.Name] = true
		}
	}
	routes := make([]RouteSpec, 0, len(plan.Routes))
	for _, r := range plan.Routes {
		if !failed[r.Link] {
			routes = append(routes, r)
		}
	}
	return routes
}

// syncLinks converges wgoN links; it returns a fixed error category per
// outbound whose link could not be applied.
func (m *Manager) syncLinks(ctx context.Context, plan *Plan) map[string]string {
	failed := map[string]string{}
	if m.Backend == nil {
		for _, l := range plan.Links {
			failed[l.OutboundID] = "unsupported"
		}
		return failed
	}
	names, err := m.Backend.ListInterfaces(ctx)
	if err != nil {
		m.logger().Warn("egress: list links failed", "err", err)
		for _, l := range plan.Links {
			failed[l.OutboundID] = "inventory"
		}
		return failed
	}
	exists := map[string]bool{}
	for _, n := range names {
		if egress.IsLinkName(n) {
			exists[n] = true
		}
	}
	want := map[string]bool{}
	for _, l := range plan.Links {
		want[l.Name] = true
	}
	for name := range exists {
		if want[name] {
			continue
		}
		if err := m.Backend.RemoveInterface(ctx, name); err != nil && !errors.Is(err, tunnel.ErrInterfaceNotFound) {
			m.logger().Warn("egress: remove stale link failed", "link", name, "err", err)
			continue
		}
		delete(m.applied, name)
		delete(m.modes, name)
	}
	for _, l := range plan.Links {
		if exists[l.Name] && m.applied[l.Name] == l.Hash {
			continue
		}
		if exists[l.Name] && m.applied[l.Name] == "" && m.adopt(ctx, l) {
			m.applied[l.Name] = l.Hash
			continue
		}
		if exists[l.Name] {
			if err := m.Backend.RemoveInterface(ctx, l.Name); err != nil && !errors.Is(err, tunnel.ErrInterfaceNotFound) {
				m.logger().Warn("egress: replace link failed", "link", l.Name, "err", err)
				failed[l.OutboundID] = "replace"
				continue
			}
		}
		mode, err := m.createLink(ctx, l)
		if err != nil {
			m.logger().Warn("egress: create link failed", "link", l.Name, "err", err)
			failed[l.OutboundID] = "create"
			delete(m.applied, l.Name)
			continue
		}
		m.applied[l.Name], m.modes[l.Name] = l.Hash, mode
	}
	return failed
}

// adopt accepts a link that survived a process restart when its live
// identity matches the desired configuration (no session reset on upgrade).
func (m *Manager) adopt(ctx context.Context, l LinkSpec) bool {
	st, err := m.Backend.Dump(ctx, l.Name)
	if err != nil || len(st.Peers) != 1 || len(l.Config.Peers) != 1 {
		return false
	}
	pub, err := tunnel.PublicKeyFromPrivate(l.Config.PrivateKey)
	if err != nil || st.PublicKey != pub || st.Peers[0].PublicKey != l.Config.Peers[0].PublicKey ||
		st.Obfuscation != l.Config.Obfuscation {
		return false
	}
	if adder, ok := m.Backend.(interface {
		EnsureAddresses(context.Context, string, []string) error
	}); ok {
		if err := adder.EnsureAddresses(ctx, l.Name, []string{l.Address}); err != nil {
			return false
		}
	}
	m.modes[l.Name] = st.BackendMode
	return true
}

func (m *Manager) createLink(ctx context.Context, l LinkSpec) (string, error) {
	spec := tunnel.InterfaceSpec{
		Name: l.Name, BackendMode: "kernel", PrivateKey: l.Config.PrivateKey, MTU: l.MTU,
		Addresses: []string{l.Address}, Obfuscation: l.Config.Obfuscation,
	}
	mode := "kernel"
	if err := m.Backend.CreateInterface(ctx, spec); err != nil {
		// Hosts without the kernel module run AmneziaWG in userspace.
		spec.BackendMode, mode = "userspace", "userspace"
		if uerr := m.Backend.CreateInterface(ctx, spec); uerr != nil {
			return "", err
		}
	}
	if err := m.Backend.ApplyInterfaceConfig(ctx, l.Name, l.Config); err != nil {
		_ = m.Backend.RemoveInterface(ctx, l.Name)
		return "", err
	}
	// Replies from the outbound arrive from Internet sources whose main-table
	// route points elsewhere; strict reverse-path filtering would drop them.
	if m.Run != nil {
		if _, err := m.Run.Run(ctx, []string{"sysctl", "-w", "net.ipv4.conf." + l.Name + ".rp_filter=2"}); err != nil {
			m.logger().Warn("egress: loose reverse-path filter not set", "link", l.Name, "err", err)
		}
	}
	return mode, nil
}

func (m *Manager) syncTable(ctx context.Context, plan *Plan, resolver bool) error {
	if m.Run == nil {
		return nil
	}
	inv, err := m.inventory(ctx)
	if err != nil {
		return err
	}
	needed := plan.Classifies() || (resolver && plan.Resolves())
	if !needed {
		if !inv.Present {
			return nil
		}
		if _, err := m.Run.Run(ctx, []string{"nft", "delete", "table", "ip", TableName}); err != nil {
			return fmt.Errorf("routing: remove table: %w", err)
		}
		return nil
	}
	script := RenderTable(plan, inv, resolver)
	f, err := os.CreateTemp("", "wg-guard-egress-*.nft")
	if err != nil {
		return fmt.Errorf("routing: temp script: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	_, werr := f.Write(script)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return fmt.Errorf("routing: temp script: %w", errors.Join(werr, cerr))
	}
	if _, err := m.Run.Run(ctx, []string{"nft", "-f", path}); err != nil {
		return fmt.Errorf("routing: apply table: %w", err)
	}
	return nil
}

func (m *Manager) inventory(ctx context.Context) (Inventory, error) {
	res, err := m.Run.Run(ctx, []string{"nft", "-t", "-j", "list", "table", "ip", TableName})
	if err != nil {
		if objectMissing(res, err) {
			return Inventory{}, nil
		}
		return Inventory{}, fmt.Errorf("routing: inspect table: %w", err)
	}
	return ParseInventory(res.Stdout)
}

func (m *Manager) setStatus(st Status) {
	m.stateMu.Lock()
	m.status = st
	m.stateMu.Unlock()
}

// Status returns a copy of the latest status.
func (m *Manager) Status() Status {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	st := m.status
	st.Outbounds = make(map[string]OutboundStatus, len(m.status.Outbounds))
	for k, v := range m.status.Outbounds {
		st.Outbounds[k] = v
	}
	st.Warnings = append([]Warning(nil), m.status.Warnings...)
	return st
}

func (m *Manager) loadSnapshot() map[string]int64 {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	out := make(map[string]int64, len(m.load))
	for k, v := range m.load {
		out[k] = v
	}
	return out
}

// countLoads refreshes active connection counts for least_conn members and
// reports whether any changed enough to re-render shares.
func (m *Manager) countLoads(st Status) bool {
	if m.Counter == nil || len(st.Counted) == 0 {
		return false
	}
	changed := false
	fresh := map[string]int64{}
	for _, id := range st.Counted {
		o, ok := st.Outbounds[id]
		if !ok {
			continue
		}
		n, err := m.Counter(o.Mark)
		if err != nil {
			return false
		}
		fresh[id] = n
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	for id, n := range fresh {
		old, seen := m.load[id]
		diff := n - old
		if diff < 0 {
			diff = -diff
		}
		if !seen || diff > max(20, old/5) {
			changed = true
		}
		if cur, ok := m.status.Outbounds[id]; ok {
			cur.Connections, cur.Counted = n, true
			m.status.Outbounds[id] = cur
		}
	}
	m.load = fresh
	return changed
}

func (m *Manager) healthSnapshot() Health {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	h := Health{}
	for id, e := range m.health {
		if e.probed {
			h[id] = e.failures < failuresDown
		}
	}
	return h
}

// record stores one probe and reports whether the healthy state flipped.
func (m *Manager) record(id string, res ProbeResult) bool {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.health == nil {
		m.health = map[string]*healthEntry{}
	}
	e := m.health[id]
	if e == nil {
		e = &healthEntry{}
		m.health[id] = e
	}
	before := !e.probed || e.failures < failuresDown
	if res.OK {
		e.failures = 0
	} else if e.failures < failuresDown {
		e.failures++
	}
	e.probed, e.last = true, res
	after := e.failures < failuresDown
	if cur, ok := m.status.Outbounds[id]; ok {
		cur.Probed, cur.Failures, cur.Probe = true, e.failures, res
		cur.Healthy = cur.LinkError == "" && after
		m.status.Outbounds[id] = cur
	}
	return before != after
}

// ProbeAll probes every applied outbound (bounded concurrency) and re-renders
// routing when an outbound's health flips. It is the egress health worker's
// pass; the scheduler only signals it.
func (m *Manager) ProbeAll(ctx context.Context) error {
	st := m.Status()
	// Repair: something outside WG-Guard removed the classification table.
	if st.Classifying && m.Run != nil && st.Error == "" {
		if inv, err := m.inventory(ctx); err == nil && !inv.Present {
			m.logger().Warn("egress: routing table missing; reapplying")
			if err := m.Reapply(ctx); err != nil {
				return err
			}
			st = m.Status()
		}
	}
	if m.Prober == nil {
		if m.countLoads(st) && ctx.Err() == nil {
			return m.Reapply(ctx)
		}
		return nil
	}
	type target struct {
		id   string
		mark uint32
		link string
	}
	var targets []target
	for id, o := range st.Outbounds {
		if o.LinkError != "" {
			continue
		}
		if o.Link != "" || o.Kind == egress.KindSourceIP {
			targets = append(targets, target{id: id, mark: o.Mark, link: o.Link})
		}
	}
	sem := make(chan struct{}, ProbeConcurrency)
	var wg sync.WaitGroup
	var flipped sync.Once
	changed := false
	for _, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res := m.Prober.Probe(ctx, t.mark)
			if m.record(t.id, res) {
				flipped.Do(func() { changed = true })
			}
			if t.link != "" && m.Backend != nil {
				if dump, err := m.Backend.Dump(ctx, t.link); err == nil && len(dump.Peers) == 1 {
					m.stateMu.Lock()
					if cur, ok := m.status.Outbounds[t.id]; ok {
						cur.LastHandshake = dump.Peers[0].LastHandshake
						m.status.Outbounds[t.id] = cur
					}
					m.stateMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if m.countLoads(m.Status()) {
		changed = true
	}
	if changed && ctx.Err() == nil {
		return m.Reapply(ctx)
	}
	return nil
}

// Test probes one outbound now (panel "Test" action) and re-renders when its
// health flips.
func (m *Manager) Test(ctx context.Context, id string) (ProbeResult, error) {
	st := m.Status()
	o, ok := st.Outbounds[id]
	if !ok {
		return ProbeResult{}, egress.ErrNotFound
	}
	if o.LinkError != "" || (o.Link == "" && o.Kind != egress.KindSourceIP) || m.Prober == nil {
		return ProbeResult{Error: "unreachable", At: time.Now().UTC()}, nil
	}
	res := m.Prober.Probe(ctx, o.Mark)
	if m.record(id, res) {
		if err := m.Reapply(ctx); err != nil {
			return res, err
		}
	}
	return res, nil
}

// LinkCounters reads cumulative link byte counters from sysfs (no subprocess).
func LinkCounters(link string) (rx, tx uint64, ok bool) {
	read := func(name string) (uint64, bool) {
		b, err := os.ReadFile("/sys/class/net/" + link + "/statistics/" + name)
		if err != nil {
			return 0, false
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		return v, err == nil
	}
	if !egress.IsLinkName(link) {
		return 0, 0, false
	}
	rx, ok1 := read("rx_bytes")
	tx, ok2 := read("tx_bytes")
	return rx, tx, ok1 && ok2
}

// Teardown removes every egress-owned kernel object: the nftables table, the
// fwmark rules and the given links. It is used by uninstall and when egress
// routing is reset; missing objects are success.
func Teardown(ctx context.Context, run subprocess.Runner, links []string) error {
	var errs []error
	if res, err := run.Run(ctx, []string{"nft", "delete", "table", "ip", TableName}); err != nil && !objectMissing(res, err) {
		errs = append(errs, fmt.Errorf("routing: remove table: %w", err))
	}
	if res, err := run.Run(ctx, []string{"ip", "-j", "-4", "rule", "show"}); err != nil {
		errs = append(errs, fmt.Errorf("routing: list policy rules: %w", err))
	} else if strings.TrimSpace(string(res.Stdout)) != "" {
		live, err := ownedRules(res.Stdout)
		if err != nil {
			errs = append(errs, err)
		}
		for mark, table := range live {
			if _, err := run.Run(ctx, ruleArgs("del", mark, table)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, link := range links {
		if !egress.IsLinkName(link) {
			continue
		}
		if res, err := run.Run(ctx, []string{"ip", "link", "del", "dev", link}); err != nil && !objectMissing(res, err) {
			errs = append(errs, fmt.Errorf("routing: remove %s: %w", link, err))
		}
	}
	return errors.Join(errs...)
}

// objectMissing classifies "already absent" failures of nft and iproute2.
func objectMissing(res subprocess.Result, err error) bool {
	msg := strings.ToLower(string(res.Stderr) + " " + err.Error())
	var exit *subprocess.ExitError
	if errors.As(err, &exit) {
		msg += " " + strings.ToLower(exit.Stderr)
	}
	return strings.Contains(msg, "no such file") || strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "cannot find device")
}
