package routing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
// not listening. Forget tells it that the named resolver-fed sets were
// (re)created empty (first apply, or repair after something outside WG-Guard
// flushed the table), so its memory of written elements is stale.
type Resolver interface {
	Configure(ctx context.Context, cfg ResolverConfig) bool
	Forget(sets []string)
}

// Bridge is the IPv6 site bridge (sitebridge.Server). Configure starts,
// reconfigures or stops its listeners and returns the outbounds whose
// bridge listens on every gateway; only those get the redirect rules, so
// client connections are never redirected to a port nobody serves.
type Bridge interface {
	Configure(ctx context.Context, cfg BridgeConfig) map[string]bool
	// Stats counts bridged connections per outbound.
	Stats() map[string]BridgeStats
}

// BridgeStats counts one outbound's bridged connections since the bridge
// started.
type BridgeStats struct {
	IPv6     uint64 // re-originated over IPv6
	IPv4     uint64 // carried over IPv4 (no IPv6 address, or IPv6 failed)
	Failed   uint64 // neither family connected
	Refused  uint64 // over the connection budget, or not a bridgeable flow
	Inflight int64
}

// BridgeStats returns the bridge counters (nil without a bridge).
func (m *Manager) BridgeStats() map[string]BridgeStats {
	if m.Bridge == nil {
		return nil
	}
	return m.Bridge.Stats()
}

// BridgeConfig is what the bridge needs from one plan.
type BridgeConfig struct {
	Gateways []netip.Addr
	Bridges  []BridgeSpec
}

// Health thresholds: an outbound is down after this many consecutive failed
// probes and up again after one success.
const failuresDown = 2

// ProbeConcurrency bounds simultaneous health probes.
const ProbeConcurrency = 4

// OutboundStatus is the observed state of one outbound.
type OutboundStatus struct {
	ID        string
	Kind      egress.Kind
	Enabled   bool
	SourceIP  string // source_ip: the address the exit must show
	Link      string
	Mark      uint32
	LinkUp    bool
	Mode      string // kernel | userspace (tunneled kinds)
	LinkError string // fixed category
	Endpoint  string // configured endpoint (tunneled kinds)
	// IPv6 reports that the link carries IPv6 (bridge on, address and
	// routes installed); Bridge is the IPv6 site bridge state: "" (off),
	// active, idle (no client tunnel to serve, or the listener failed) or
	// no_ipv6 (the host refused the IPv6 address or route).
	IPv6          bool
	Bridge        string
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
	// Drain deletes the tracked connections of an outbound mark; nil keeps
	// them. When an exit stops carrying traffic (down, disabled, deleted),
	// its connections would otherwise hang until each application gives up;
	// drained, their next packet is classified again and the application
	// reconnects through the new route at once.
	Drain func(mark uint32) error
	// Bridge is the IPv6 site bridge; nil disables it.
	Bridge Bridge
	// Sites runs site tests and Speeds speed tests; nil reports them
	// unsupported.
	Sites  *SiteTester
	Speeds *SpeedTester
	// Lookup resolves endpoint names for re-resolution (nil: the node's
	// resolver).
	Lookup Lookup
	Log    *slog.Logger

	mu      sync.Mutex // serializes applies; guards applied, modes, ifaces, idle
	applied map[string]string
	modes   map[string]string
	ifaces  []Interface
	// idle records that an empty desired state was fully converged, so later
	// passes with nothing configured cost no subprocess.
	idle bool
	// routed is the mark of every outbound that carried traffic after the
	// last apply (the drain baseline).
	routed map[string]uint32
	// v6Clean records that no IPv6 policy state is left to remove.
	v6Clean bool

	stateMu sync.RWMutex // guards health, healthDirty, load and status
	health  map[string]*healthEntry
	// healthDirty records a health flip (panel Test) that no apply has
	// rendered yet; the next health pass re-renders.
	healthDirty bool
	load        map[string]int64
	status      Status
	// retryAt and retryDelay pace the health pass's retries of an apply
	// that did not fully converge (a link, the policy or the table failed).
	retryAt    time.Time
	retryDelay time.Duration
	// reresolveAt paces endpoint re-resolution per outbound.
	reresolveAt map[string]time.Time

	diag diagnostics // site and speed test bounds and results
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
	m.pruneHealth(snap)
	in := Inputs{Health: m.healthSnapshot(), Load: m.loadSnapshot()}
	plan := Compile(snap, m.ifaces, in, m.Geo)
	linkErrs := m.syncLinks(ctx, plan)
	for id, reason := range plan.Unlinked {
		linkErrs[id] = reason
	}
	if len(linkErrs) > 0 {
		in.Down = map[string]bool{}
		for id := range linkErrs {
			in.Down[id] = true
		}
		plan = Compile(snap, m.ifaces, in, m.Geo)
	}
	var errs []error
	noIPv6 := map[string]bool{}
	if m.Run != nil {
		if err := syncPolicy(ctx, m.Run, liveRoutes(plan, linkErrs)); err != nil {
			st.Error = "policy"
			errs = append(errs, err)
		}
		noIPv6 = m.syncIPv6(ctx, plan, linkErrs)
	}
	m.configureBridge(ctx, plan, linkErrs, noIPv6)
	resolverOK := false
	if m.Resolver != nil {
		cfg := ResolverConfig{BlockEncryptedDNS: plan.BlockEncryptedDNS}
		if plan.Resolves() {
			cfg.Gateways, cfg.Ruleset = plan.Gateways, plan.Ruleset
		}
		resolverOK = m.Resolver.Configure(ctx, cfg) && plan.Resolves()
	}
	created, err := m.syncTable(ctx, plan, resolverOK)
	if err != nil {
		st.Error = "table"
		errs = append(errs, err)
	}
	if m.Resolver != nil && len(created) > 0 {
		m.Resolver.Forget(created)
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
	bridged := map[string]bool{}
	for _, b := range plan.Bridges {
		bridged[b.OutboundID] = true
	}
	m.stateMu.RLock()
	for _, o := range snap.Outbounds {
		cur := OutboundStatus{ID: o.ID, Kind: o.Kind, Enabled: o.Enabled, SourceIP: o.SourceIP, Mark: SlotMark(o.Slot)}
		if o.Tunnel != nil {
			cur.Endpoint = o.Tunnel.Endpoint
		}
		if l, ok := linked[o.ID]; ok {
			cur.Link, cur.Mode = l.Name, m.modes[l.Name]
			cur.LinkUp = linkErrs[o.ID] == ""
			if l.Address6 != "" && cur.LinkUp {
				cur.IPv6 = !noIPv6[o.ID]
				switch {
				case !cur.IPv6:
					cur.Bridge = "no_ipv6"
				case bridged[o.ID]:
					cur.Bridge = "active"
				default:
					cur.Bridge = "idle"
				}
			}
		}
		cur.LinkError = linkErrs[o.ID]
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
	m.drain(st)
	m.idle = snap.Empty() && len(errs) == 0 && len(linkErrs) == 0
	return errors.Join(errs...)
}

// drain deletes the connections of every outbound that carried traffic after
// the previous apply and no longer does. It runs after the table is applied,
// so the drained flows are classified against the new routes.
func (m *Manager) drain(st Status) {
	now := map[string]uint32{}
	for id, o := range st.Outbounds {
		if o.Healthy {
			now[id] = o.Mark
		}
	}
	if m.Drain != nil && m.routed != nil {
		for id, mark := range m.routed {
			if _, still := now[id]; still {
				continue
			}
			if err := m.Drain(mark); err != nil {
				m.logger().Warn("egress: connection drain failed", "outbound", id, "err", err)
			} else {
				m.logger().Info("egress: drained connections of an exit that stopped carrying traffic", "outbound", id)
			}
		}
	}
	m.routed = now
}

// syncIPv6 installs the IPv6 address and policy routes of bridged links. It
// returns the outbounds whose IPv6 path could not be installed; their bridge
// stays off and their IPv4 path is unaffected.
func (m *Manager) syncIPv6(ctx context.Context, plan *Plan, linkErrs map[string]string) map[string]bool {
	failed := map[string]bool{}
	byLink := map[string]string{}
	var routes []RouteSpec
	for _, l := range plan.Links {
		if l.Address6 == "" || linkErrs[l.OutboundID] != "" {
			continue
		}
		byLink[l.Name] = l.OutboundID
		if _, err := m.Run.Run(ctx, []string{"ip", "-6", "addr", "replace", l.Address6, "dev", l.Name}); err != nil {
			m.logger().Warn("egress: IPv6 address not set; the IPv6 site bridge stays off", "link", l.Name, "err", err)
			failed[l.OutboundID] = true
		}
	}
	for _, r := range plan.Routes6 {
		if id, ok := byLink[r.Link]; ok && !failed[id] {
			routes = append(routes, r)
		}
	}
	if len(routes) == 0 && m.v6Clean {
		return failed
	}
	bad, err := syncPolicy6(ctx, m.Run, routes)
	for link := range bad {
		failed[byLink[link]] = true
	}
	if err != nil && len(routes) > 0 {
		m.logger().Warn("egress: IPv6 policy routing incomplete", "err", err)
	}
	// With nothing wanted, a host without IPv6 policy routing has nothing
	// left to remove either.
	m.v6Clean = len(routes) == 0
	return failed
}

// configureBridge starts the bridge for usable bridged outbounds and narrows
// plan.Bridges to the ones that listen.
func (m *Manager) configureBridge(ctx context.Context, plan *Plan, linkErrs map[string]string, noIPv6 map[string]bool) {
	var want []BridgeSpec
	for _, b := range plan.Bridges {
		if linkErrs[b.OutboundID] == "" && !noIPv6[b.OutboundID] {
			want = append(want, b)
		}
	}
	plan.Bridges = nil
	if m.Bridge == nil {
		return
	}
	cfg := BridgeConfig{}
	if len(plan.Ifaces) > 0 && len(want) > 0 {
		cfg = BridgeConfig{Gateways: plan.Gateways, Bridges: want}
	}
	active := m.Bridge.Configure(ctx, cfg)
	for _, b := range cfg.Bridges {
		if active[b.OutboundID] {
			plan.Bridges = append(plan.Bridges, b)
		}
	}
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

// adopt accepts a link that survived a process restart (no session reset on
// upgrade). Its identity (keys, obfuscation) must match the desired
// configuration; everything a restart may have left behind that the live
// state cannot prove (endpoint, preshared key, keepalive, MTU, address) is
// converged in place: peers through SyncPeers (syncconf keeps sessions), the
// MTU through `ip link`, the address through EnsureAddresses. Any step that
// cannot be done means the link is recreated instead.
func (m *Manager) adopt(ctx context.Context, l LinkSpec) bool {
	st, err := m.Backend.Dump(ctx, l.Name)
	if err != nil || len(st.Peers) != 1 || len(l.Config.Peers) != 1 || m.Run == nil {
		return false
	}
	pub, err := tunnel.PublicKeyFromPrivate(l.Config.PrivateKey)
	if err != nil || st.PublicKey != pub || st.Peers[0].PublicKey != l.Config.Peers[0].PublicKey ||
		st.Obfuscation != l.Config.Obfuscation || st.Fwmark != l.fwmark() {
		return false
	}
	adder, ok := m.Backend.(interface {
		EnsureAddresses(context.Context, string, []string) error
	})
	if !ok {
		return false
	}
	if err := m.Backend.SyncPeers(ctx, l.Name, l.Config.Peers); err != nil {
		m.logger().Warn("egress: adopt link peer sync failed", "link", l.Name, "err", err)
		return false
	}
	if l.MTU > 0 {
		if _, err := m.Run.Run(ctx, []string{"ip", "link", "set", "dev", l.Name, "mtu", strconv.Itoa(l.MTU)}); err != nil {
			return false
		}
	}
	if err := adder.EnsureAddresses(ctx, l.Name, []string{l.Address}); err != nil {
		return false
	}
	m.modes[l.Name] = st.BackendMode
	return true
}

// bringUp creates the link on the kernel module and falls back to the
// userspace daemon, both when the module is missing and when it rejects the
// imported parameters: the two pinned runtimes accept different signature
// packet tags (docs/integrations/amneziawg.md), and an imported config was
// written for whichever runtime the remote side expected.
func (m *Manager) bringUp(ctx context.Context, spec tunnel.InterfaceSpec, l LinkSpec) (string, error) {
	var errs []error
	for _, mode := range []string{"kernel", "userspace"} {
		spec.BackendMode = mode
		if err := m.Backend.CreateInterface(ctx, spec); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", mode, err))
			continue
		}
		err := m.Backend.ApplyInterfaceConfig(ctx, l.Name, l.Config)
		if err == nil {
			return mode, nil
		}
		_ = m.Backend.RemoveInterface(ctx, l.Name)
		errs = append(errs, fmt.Errorf("%s: %w", mode, err))
	}
	return "", errors.Join(errs...)
}

func (m *Manager) createLink(ctx context.Context, l LinkSpec) (string, error) {
	spec := tunnel.InterfaceSpec{
		Name: l.Name, BackendMode: "kernel", PrivateKey: l.Config.PrivateKey, MTU: l.MTU,
		Addresses: []string{l.Address}, Obfuscation: l.Config.Obfuscation,
	}
	mode, err := m.bringUp(ctx, spec, l)
	if err != nil {
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

// syncTable converges the nftables table. It returns the resolver-fed sets
// the live table did not have, which this pass (re)creates empty.
func (m *Manager) syncTable(ctx context.Context, plan *Plan, resolver bool) ([]string, error) {
	if m.Run == nil {
		return nil, nil
	}
	inv, err := m.inventory(ctx)
	if err != nil {
		return nil, err
	}
	needed := plan.Classifies() || (resolver && plan.Resolves())
	if !needed {
		if !inv.Present {
			return nil, nil
		}
		if _, err := m.Run.Run(ctx, []string{"nft", "delete", "table", "ip", TableName}); err != nil {
			return nil, fmt.Errorf("routing: remove table: %w", err)
		}
		return nil, nil
	}
	var created []string
	for _, name := range plan.DynamicSets {
		if !inv.Sets[name] {
			created = append(created, name)
		}
	}
	script := RenderTable(plan, inv, resolver)
	f, err := os.CreateTemp("", "wg-guard-egress-*.nft")
	if err != nil {
		return created, fmt.Errorf("routing: temp script: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	_, werr := f.Write(script)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return created, fmt.Errorf("routing: temp script: %w", errors.Join(werr, cerr))
	}
	if _, err := m.Run.Run(ctx, []string{"nft", "-f", path}); err != nil {
		return created, fmt.Errorf("routing: apply table: %w", err)
	}
	return created, nil
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

// pruneHealth drops health of deleted outbounds. The apply that follows
// renders the current health, so a pending flip is consumed here too.
func (m *Manager) pruneHealth(snap *egress.Snapshot) {
	live := make(map[string]bool, len(snap.Outbounds))
	for _, o := range snap.Outbounds {
		live[o.ID] = true
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	for id := range m.health {
		if !live[id] {
			delete(m.health, id)
		}
	}
	m.healthDirty = false
}

// takeHealthDirty reports and clears a health flip no apply has rendered.
func (m *Manager) takeHealthDirty() bool {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	dirty := m.healthDirty
	m.healthDirty = false
	return dirty
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
	// Retry: an apply that did not converge (an endpoint name that did not
	// resolve at boot, a transient nft or ip failure) is retried with backoff
	// instead of waiting for an unrelated edit.
	if m.retryDue(st, time.Now()) {
		m.logger().Info("egress: retrying an apply that did not converge")
		if err := m.Reapply(ctx); err != nil {
			m.logger().Warn("egress: retry failed", "err", err)
		}
		st = m.Status()
	}
	// Repair: something outside WG-Guard removed the routing table.
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
		ipv6 bool
	}
	var targets []target
	for id, o := range st.Outbounds {
		if o.LinkError != "" || !o.Enabled {
			continue
		}
		if o.Link != "" || o.Kind == egress.KindSourceIP {
			targets = append(targets, target{id: id, mark: o.Mark, link: o.Link, ipv6: o.IPv6})
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
			res := m.probe(ctx, t.mark, t.ipv6)
			if ctx.Err() != nil {
				// Cut off by the pass deadline: says nothing about the exit.
				return
			}
			res = verifyExit(st.Outbounds[t.id], res)
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
	if m.reresolve(ctx, time.Now()) {
		changed = true
	}
	if m.countLoads(m.Status()) {
		changed = true
	}
	if m.takeHealthDirty() {
		changed = true
	}
	if changed && ctx.Err() == nil {
		return m.Reapply(ctx)
	}
	return nil
}

// probe observes one outbound; a link that carries IPv6 also reports its
// IPv6 exit (informational: it never fails the probe, since the bridge falls
// back to IPv4).
func (m *Manager) probe(ctx context.Context, mark uint32, ipv6 bool) ProbeResult {
	res := m.Prober.Probe(ctx, mark)
	if p, ok := m.Prober.(IPv6Prober); ok && ipv6 && res.OK && ctx.Err() == nil {
		res.ExitIP6 = p.ProbeIPv6(ctx, mark)
	}
	return res
}

// Endpoint re-resolution: WireGuard resolves an endpoint name once, when the
// peer is set. A down link whose endpoint is a name (a dynamic-DNS server,
// a provider that moved) is checked against a fresh lookup at most every
// reresolveEvery; when the name now points elsewhere the link's peers are
// set again, which resolves the name anew without replacing the link.
const (
	reresolveEvery   = 5 * time.Minute
	reresolveTimeout = 3 * time.Second
)

func (m *Manager) reresolve(ctx context.Context, now time.Time) bool {
	if m.Backend == nil {
		return false
	}
	lookup := m.Lookup
	if lookup == nil {
		lookup = DefaultLookup
	}
	changed := false
	for id, o := range m.Status().Outbounds {
		if o.Link == "" || o.LinkError != "" || !o.Enabled || o.Failures < failuresDown {
			continue
		}
		host, _, err := net.SplitHostPort(o.Endpoint)
		if err != nil {
			continue
		}
		if _, err := netip.ParseAddr(host); err == nil {
			continue // a literal address never changes by itself
		}
		m.stateMu.Lock()
		if m.reresolveAt == nil {
			m.reresolveAt = map[string]time.Time{}
		}
		due := now.After(m.reresolveAt[id])
		if due {
			m.reresolveAt[id] = now.Add(reresolveEvery)
		}
		m.stateMu.Unlock()
		if !due {
			continue
		}
		dump, err := m.Backend.Dump(ctx, o.Link)
		if err != nil || len(dump.Peers) != 1 {
			continue
		}
		live, err := netip.ParseAddrPort(dump.Peers[0].Endpoint)
		if err != nil {
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, reresolveTimeout)
		addrs, err := lookup(lctx, "ip4", host)
		cancel()
		if err != nil || len(addrs) == 0 {
			continue
		}
		moved := true
		for _, a := range addrs {
			if a.Unmap() == live.Addr().Unmap() {
				moved = false
			}
		}
		if moved {
			m.logger().Info("egress: endpoint name points to a new address; setting the peer again", "link", o.Link)
			m.mu.Lock()
			delete(m.applied, o.Link) // the next apply adopts the link and re-sets its peer
			m.mu.Unlock()
			changed = true
		}
	}
	return changed
}

// verifyExit fails a source_ip probe that did not leave from the configured
// address: the request went out, but through the wrong source (the SNAT rule
// is missing, or the address is not on the uplink).
func verifyExit(o OutboundStatus, res ProbeResult) ProbeResult {
	if o.Kind == egress.KindSourceIP && res.OK && res.ExitIP != "" && o.SourceIP != "" && res.ExitIP != o.SourceIP {
		res.OK, res.Error = false, "wrong_exit"
	}
	return res
}

// Retry backoff for an apply that did not converge.
const (
	retryFirst = 30 * time.Second
	retryMax   = 10 * time.Minute
)

// retryDue reports whether the last apply left work undone and its backoff
// has elapsed. A converged state resets the backoff.
func (m *Manager) retryDue(st Status, now time.Time) bool {
	pending := st.Error != ""
	for _, o := range st.Outbounds {
		// A broken chain is a configuration state, not a transient failure.
		if o.LinkError != "" && o.LinkError != "via" {
			pending = true
		}
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if !pending || st.AppliedAt.IsZero() {
		m.retryAt, m.retryDelay = time.Time{}, 0
		return false
	}
	if m.retryAt.IsZero() {
		m.retryDelay = retryFirst
		m.retryAt = st.AppliedAt.Add(m.retryDelay)
	}
	if now.Before(m.retryAt) {
		return false
	}
	m.retryDelay = min(2*m.retryDelay, retryMax)
	m.retryAt = now.Add(m.retryDelay)
	return true
}

// Test probes one outbound now (panel "Test" action) and records the result.
// It never touches the kernel, so a read-only viewer can run it: a health
// flip is rendered by the next health pass (ProbeAll), like a scheduled probe
// result.
func (m *Manager) Test(ctx context.Context, id string) (ProbeResult, error) {
	st := m.Status()
	o, ok := st.Outbounds[id]
	if !ok {
		return ProbeResult{}, egress.ErrNotFound
	}
	if o.LinkError != "" || (o.Link == "" && o.Kind != egress.KindSourceIP) || m.Prober == nil {
		return ProbeResult{Error: "unreachable", At: time.Now().UTC()}, nil
	}
	res := verifyExit(o, m.probe(ctx, o.Mark, o.IPv6))
	if m.record(id, res) {
		m.stateMu.Lock()
		m.healthDirty = true
		m.stateMu.Unlock()
	}
	return res, nil
}

// WARP rotation: a new identity usually brings a new exit address, but
// Cloudflare may hand out the same one again. RotateWARP re-registers until
// the probed exit changes, at most rotateAttempts times (registrations are
// rate-limited, so the bound is small).
const rotateAttempts = 3

// RotateResult reports a WARP rotation.
type RotateResult struct {
	Name     string
	OldIP    string // "" when the exit was unknown
	NewIP    string // "" when the new identity did not answer a probe
	OldIP6   string
	NewIP6   string
	Attempts int
	Changed  bool
}

// RotateWARP gives a WARP outbound a new identity and keeps trying while the
// exit address stays the same.
func (m *Manager) RotateWARP(ctx context.Context, id string) (RotateResult, error) {
	var out RotateResult
	if o, ok := m.Status().Outbounds[id]; ok && o.Probed && o.Probe.OK {
		out.OldIP, out.OldIP6 = o.Probe.ExitIP, o.Probe.ExitIP6
	}
	for out.Attempts < rotateAttempts {
		o, err := m.Store.RenewWARP(ctx, id)
		if err != nil {
			if out.Attempts == 0 {
				return out, err
			}
			break // keep the identity that was already applied
		}
		out.Attempts++
		out.Name = o.Name
		if err := m.Reapply(ctx); err != nil {
			m.logger().Warn("egress: apply after WARP renewal incomplete", "err", err)
		}
		res, err := m.Test(ctx, id)
		if err != nil || !res.OK {
			out.NewIP, out.NewIP6 = "", ""
			break // the new identity is applied; health shows its state
		}
		out.NewIP, out.NewIP6 = res.ExitIP, res.ExitIP6
		if out.OldIP == "" || res.ExitIP != out.OldIP {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	out.Changed = out.NewIP != "" && out.NewIP != out.OldIP
	return out, nil
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

// ReleaseRedirects stops redirecting client DNS to the built-in resolver and
// client web connections to the IPv6 site bridge. It runs when the panel
// stops: without it, clients keep being redirected to listeners that no
// longer exist and lose DNS (or the bridged sites) until the next start. The
// next apply re-adds the redirect rules. A crash still leaves them in place
// until the panel restarts.
func (m *Manager) ReleaseRedirects(ctx context.Context) error {
	if m.Run == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for _, chain := range []string{chainDNS, chainBridge} {
		res, err := m.Run.Run(ctx, []string{"nft", "flush", "chain", "ip", TableName, chain})
		if err != nil && !objectMissing(res, err) {
			errs = append(errs, fmt.Errorf("routing: release %s: %w", chain, err))
		}
	}
	return errors.Join(errs...)
}

// Teardown removes every egress-owned kernel object: the nftables table, the
// fwmark rules and the given links. It is used by uninstall and when egress
// routing is reset; missing objects are success.
func Teardown(ctx context.Context, run subprocess.Runner, links []string) error {
	var errs []error
	if res, err := run.Run(ctx, []string{"nft", "delete", "table", "ip", TableName}); err != nil && !objectMissing(res, err) {
		errs = append(errs, fmt.Errorf("routing: remove table: %w", err))
	}
	for _, family := range []string{"-4", "-6"} {
		res, err := run.Run(ctx, []string{"ip", "-j", family, "rule", "show"})
		if err != nil {
			if family == "-4" { // a host without IPv6 has no IPv6 rules to remove
				errs = append(errs, fmt.Errorf("routing: list policy rules: %w", err))
			}
			continue
		}
		if strings.TrimSpace(string(res.Stdout)) == "" {
			continue
		}
		live, err := ownedRules(res.Stdout)
		if err != nil {
			errs = append(errs, err)
		}
		for mark, table := range live {
			if _, err := run.Run(ctx, familyRuleArgs(family, "del", mark, table)); err != nil {
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
	// The slot tables keep their unreachable guard route after the links are
	// gone. Flushing an empty table is harmless, so every slot is cleared.
	for slot := 1; slot <= egress.MaxSlots; slot++ {
		_, _ = run.Run(ctx, []string{"ip", "-4", "route", "flush", "table", strconv.Itoa(tableBase + slot)})
		_, _ = run.Run(ctx, []string{"ip", "-6", "route", "flush", "table", strconv.Itoa(tableBase + slot)})
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
