// Package routing renders egress routing state into the kernel: one
// WireGuard-family link per tunneled outbound (wgo<slot>), one policy routing
// table and fwmark rule per link, and one namespaced nftables table
// (`ip wgguard_egress`) that classifies new client connections and marks
// them. Domain rules are fed by the built-in resolver (dnsrouter), which
// adds resolved addresses to per-rule timeout sets in the same table.
//
// The plan is a pure function of the stored state, outbound health and the
// tunnel interfaces (Compile); Manager applies it idempotently. The table is
// never flushed wholesale: static chains and sets are replaced inside one
// nft transaction while the resolver-fed sets keep their elements, so an edit
// does not send already-resolved connections back to the direct route.
package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/egress/match"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Packet marks: an 11-bit field (bits 20–30) leaves the low bits used by
// Docker, kube-proxy and Tailscale untouched.
const (
	MarkMask   uint32 = 0x7ff00000
	markShift         = 20
	MarkDirect uint32 = 0x7fe << markShift
	MarkBlock  uint32 = 0x7ff << markShift
)

// SlotMark is an outbound's packet mark.
func SlotMark(slot int) uint32 { return uint32(slot) << markShift }

// Policy routing: one table per slot; all rules share one priority ahead of
// the main table.
const (
	RulePriority = 5180
	tableBase    = 51800
)

// SlotTable is an outbound's routing table.
func SlotTable(slot int) int { return tableBase + slot }

// TableName is the nftables table egress routing owns (ADR-0004 namespacing).
const TableName = "wgguard_egress"

// Interface is one enabled client tunnel (awgN) and its IPv4 pools.
type Interface struct {
	Name  string
	Pools []string
}

// Health is the latest health of each outbound. Absent means "not probed
// yet" and is treated as healthy so a new outbound routes immediately.
type Health map[string]bool

// Geo materializes geosite/geoip categories (geodata.Store in production).
type Geo interface {
	Domains(wanted []string) (map[string][]egress.Matcher, error)
	Prefixes(wanted []string) (map[string][]netip.Prefix, error)
}

// LinkSpec is one desired outbound link.
type LinkSpec struct {
	OutboundID string
	Name       string
	Slot       int
	MTU        int
	Address    string
	// Address6 is set when the IPv6 site bridge is on: the link carries the
	// tunnel's IPv6 address and IPv6 routes, for the bridge only.
	Address6 string
	Config   tunnel.InterfaceConfig
	Hash     string // change detection over every applied field
}

// fwmark is the link's socket mark (0 when the link is not chained).
func (l LinkSpec) fwmark() uint32 {
	mark, err := strconv.ParseUint(l.Config.Fwmark, 0, 32)
	if err != nil {
		return 0
	}
	return uint32(mark)
}

// RouteSpec is one policy route: mark → table → link.
type RouteSpec struct {
	Mark  uint32
	Table int
	Link  string
}

// BridgeSpec is one outbound with the IPv6 site bridge on: client TCP
// connections to web ports marked for it are redirected to Port on the
// tunnel gateways, where the bridge re-originates them through the link,
// over IPv6 when the site has an IPv6 address.
type BridgeSpec struct {
	OutboundID string
	Mark       uint32
	Port       uint16
}

// BridgePortBase is the first bridge port; an outbound listens on
// BridgePortBase+slot (TCP, tunnel gateway addresses only).
const BridgePortBase = 51800

// BridgePorts are the client destination ports the bridge carries. Other
// ports keep the kernel path over IPv4.
var BridgePorts = []uint16{80, 443}

// BridgePort is an outbound's bridge listener port.
func BridgePort(slot int) uint16 { return uint16(BridgePortBase + slot) }

// SNATSpec rewrites the source address of marked traffic.
type SNATSpec struct {
	Mark    uint32
	Address string
}

// Warning is a non-fatal planning finding shown in the panel.
type Warning struct {
	Kind   string // geosite_missing | geoip_missing | geodata_unavailable | via_unavailable
	Detail string // category name; safe to display
}

// ClassifyRule sends connections whose destination is in Set to Chain.
type ClassifyRule struct {
	RuleID string
	Set    string
	Chain  string
}

// Plan is the complete desired kernel state.
type Plan struct {
	Ifaces   []string // client tunnel names (classification inputs)
	Gateways []netip.Addr
	Links    []LinkSpec
	Routes   []RouteSpec
	// Routes6 are the IPv6 policy routes of bridged links. Only the bridge's
	// own sockets carry IPv6 through them.
	Routes6 []RouteSpec
	// Bridges lists the outbounds with the IPv6 site bridge on. The manager
	// narrows it to the bridges that are listening before rendering.
	Bridges           []BridgeSpec
	SNAT              []SNATSpec
	Chains            map[string]string // target chain name → statements
	Classify          []ClassifyRule
	DefaultChain      string
	StaticSets        map[string][]netip.Prefix
	DynamicSets       []string
	Ruleset           *match.Ruleset
	BlockEncryptedDNS bool
	MSS               map[string]int // link → MSS for inbound SYN/ACK
	Warnings          []Warning
	// Unlinked lists enabled tunneled outbounds that get no link, by fixed
	// reason: "via" when the outbound they are chained through is missing,
	// disabled or itself chained.
	Unlinked map[string]string
}

// Classifies reports whether the nftables table is needed.
func (p *Plan) Classifies() bool {
	return len(p.Classify) > 0 || p.DefaultChain != chainDirect || len(p.SNAT) > 0 || len(p.MSS) > 0
}

// Resolves reports whether the resolver must run (domain rules exist).
func (p *Plan) Resolves() bool { return !p.Ruleset.Empty() }

const (
	chainDirect = "t_direct"
	chainBlock  = "t_block"
)

func outboundChain(slot int) string { return fmt.Sprintf("t_o%d", slot) }

func idHex(id string) string { return strings.ReplaceAll(id, "-", "") }

// DynamicSetName is the resolver-fed set of one rule.
func DynamicSetName(ruleID string) string { return "r" + idHex(ruleID) + "_d" }

func staticSetName(ruleID string) string { return "r" + idHex(ruleID) + "_c" }

func balancerChain(id string) string { return "t_b" + idHex(id) }

// markKeep selects the mark bits egress routing never changes.
const markKeep = ^MarkMask

// markStatements sets the mark field on the packet and the connection and
// keeps every bit outside MarkMask.
func markStatements(mark uint32) string {
	return fmt.Sprintf("meta mark set meta mark and 0x%08x or 0x%08x ct mark set ct mark and 0x%08x or 0x%08x", markKeep, mark, markKeep, mark)
}

// restoreStatement copies a pinned connection's mark field back onto a
// packet, keeping the other bits. nft cannot OR two runtime values on the
// kernels WG-Guard supports, so each pinned mark gets its own constant
// statement, reached through a verdict map.
func restoreStatement(mark uint32) string {
	return fmt.Sprintf("meta mark set meta mark and 0x%08x or 0x%08x", markKeep, mark)
}

// Inputs are the runtime observations a plan depends on besides stored state.
type Inputs struct {
	Health Health
	// Down lists outbounds whose link failed to apply in this pass.
	Down map[string]bool
	// Load is the active connection count per outbound (least_conn).
	Load map[string]int64
}

type compiler struct {
	snap      *egress.Snapshot
	health    Health
	down      map[string]bool // links that failed to apply
	load      map[string]int64
	outbounds map[string]egress.OutboundState
	balancers map[string]*egress.Balancer
	plan      *Plan
}

// Compile builds the plan from stored state and runtime inputs.
func Compile(snap *egress.Snapshot, ifaces []Interface, in Inputs, geo Geo) *Plan {
	c := &compiler{
		snap: snap, health: in.Health, down: in.Down, load: in.Load,
		outbounds: map[string]egress.OutboundState{},
		balancers: map[string]*egress.Balancer{},
		plan: &Plan{
			Chains:     map[string]string{chainDirect: markStatements(MarkDirect), chainBlock: markStatements(MarkBlock)},
			StaticSets: map[string][]netip.Prefix{},
			MSS:        map[string]int{},
			Ruleset:    &match.Ruleset{},
		},
	}
	for _, ifc := range ifaces {
		c.plan.Ifaces = append(c.plan.Ifaces, ifc.Name)
		for _, pool := range ifc.Pools {
			if p, err := netip.ParsePrefix(pool); err == nil && p.Addr().Is4() {
				c.plan.Gateways = append(c.plan.Gateways, p.Masked().Addr().Next())
			}
		}
	}
	sort.Strings(c.plan.Ifaces)
	for _, o := range snap.Outbounds {
		c.outbounds[o.ID] = o
	}
	for _, b := range snap.Balancers {
		c.balancers[b.ID] = b
	}
	c.outboundState()
	if len(c.plan.Ifaces) == 0 {
		// No client tunnel: nothing to classify; links still come up so the
		// panel can test outbounds.
		c.plan.DefaultChain = chainDirect
		return c.plan
	}
	c.rules(geo)
	c.plan.DefaultChain = c.targetChain(snap.Settings.DefaultTarget)
	c.plan.BlockEncryptedDNS = snap.Settings.BlockEncryptedDNS && !c.plan.Ruleset.Empty()
	c.pruneChains()
	return c.plan
}

// outboundState renders links, routes and SNAT for every enabled outbound.
func (c *compiler) outboundState() {
	for _, o := range c.snap.Outbounds {
		if !o.Enabled {
			continue
		}
		mark := SlotMark(o.Slot)
		switch {
		case o.Kind.Tunneled() && o.Tunnel != nil:
			fwmark, ok := c.chainMark(o)
			if !ok {
				continue
			}
			spec := linkSpec(o, fwmark)
			c.plan.Links = append(c.plan.Links, spec)
			c.plan.Routes = append(c.plan.Routes, RouteSpec{Mark: mark, Table: SlotTable(o.Slot), Link: spec.Name})
			if spec.Address6 != "" {
				c.plan.Routes6 = append(c.plan.Routes6, RouteSpec{Mark: mark, Table: SlotTable(o.Slot), Link: spec.Name})
				c.plan.Bridges = append(c.plan.Bridges, BridgeSpec{OutboundID: o.ID, Mark: mark, Port: BridgePort(o.Slot)})
			}
			if o.Tunnel.MTU > 0 {
				c.plan.MSS[spec.Name] = o.Tunnel.MTU - 40
			}
		case o.Kind == egress.KindSourceIP && o.SourceIP != "":
			c.plan.SNAT = append(c.plan.SNAT, SNATSpec{Mark: mark, Address: o.SourceIP})
		}
	}
}

func linkSpec(o egress.OutboundState, fwmark uint32) LinkSpec {
	t := o.Tunnel
	peer := tunnel.PeerConfig{
		PublicKey:    t.PeerPublicKey,
		PresharedKey: o.Secret.PresharedKey,
		AllowedIPs:   []string{"0.0.0.0/0"},
		Endpoint:     t.Endpoint,
	}
	peer.PersistentKeepalive = keepalive(t.Keepalive, t.KeepaliveMax)
	address6 := ""
	if t.Bridges() {
		address6 = t.Address6
		peer.AllowedIPs = append(peer.AllowedIPs, "::/0")
	}
	cfg := tunnel.InterfaceConfig{
		PrivateKey:  o.Secret.PrivateKey,
		Obfuscation: t.Obfuscation.Tunnel(),
		Peers:       []tunnel.PeerConfig{peer},
	}
	if fwmark != 0 {
		cfg.Fwmark = fmt.Sprintf("0x%x", fwmark)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%s|%s|%s|%s|%s|%s|%+v", o.LinkName(), t.MTU, t.Address, cfg.PrivateKey,
		peer.PublicKey, peer.PresharedKey, peer.Endpoint, peer.PersistentKeepalive, cfg.Obfuscation)
	if fwmark != 0 {
		// Appended only when set, so unchained links keep their hash.
		fmt.Fprintf(h, "|fwmark=%d", fwmark)
	}
	if address6 != "" {
		fmt.Fprintf(h, "|v6=%s", address6)
	}
	return LinkSpec{
		OutboundID: o.ID, Name: o.LinkName(), Slot: o.Slot, MTU: t.MTU, Address: t.Address, Address6: address6,
		Config: cfg, Hash: hex.EncodeToString(h.Sum(nil))[:24],
	}
}

// chainMark returns the socket mark of an outbound chained through another
// one: the link's own encrypted packets carry the other outbound's mark, so
// policy routing sends the tunnel through it (WARP then exits next to that
// outbound's server). An outbound whose chain is broken gets no link at all:
// leaving through the main route instead would silently change its exit.
func (c *compiler) chainMark(o egress.OutboundState) (uint32, bool) {
	if o.Tunnel.Via == "" {
		return 0, true
	}
	v, ok := c.outbounds[o.Tunnel.Via]
	if !ok || !v.Enabled || !v.Kind.Tunneled() || v.Tunnel == nil || v.Tunnel.Via != "" || v.ID == o.ID {
		if c.plan.Unlinked == nil {
			c.plan.Unlinked = map[string]string{}
		}
		c.plan.Unlinked[o.ID] = "via"
		c.plan.Warnings = append(c.plan.Warnings, Warning{Kind: "via_unavailable", Detail: o.Name})
		return 0, false
	}
	return SlotMark(v.Slot), true
}

// usable reports whether traffic may be sent to the outbound now.
func (c *compiler) usable(id string) (egress.OutboundState, bool) {
	o, ok := c.outbounds[id]
	if !ok || !o.Enabled || c.down[id] || c.plan.Unlinked[id] != "" {
		return o, false
	}
	if healthy, probed := c.health[id]; probed && !healthy {
		return o, false
	}
	switch {
	case o.Kind.Tunneled():
		return o, o.Tunnel != nil
	case o.Kind == egress.KindSourceIP:
		return o, o.SourceIP != ""
	}
	return o, false
}

func (c *compiler) outboundChainFor(o egress.OutboundState) string {
	name := outboundChain(o.Slot)
	c.plan.Chains[name] = markStatements(SlotMark(o.Slot))
	return name
}

// targetChain resolves a target to the chain that marks it. Unavailable
// single outbounds fail open to the direct route; balancers use their
// configured fallback.
func (c *compiler) targetChain(t egress.Target) string {
	switch t.Kind {
	case egress.TargetBlock:
		return chainBlock
	case egress.TargetOutbound:
		if o, ok := c.usable(t.ID); ok {
			return c.outboundChainFor(o)
		}
		return chainDirect
	case egress.TargetBalancer:
		return c.balancerTarget(t.ID)
	}
	return chainDirect
}

func (c *compiler) balancerTarget(id string) string {
	b, ok := c.balancers[id]
	if !ok || !b.Enabled {
		return chainDirect
	}
	type live struct {
		id     string
		chain  string
		weight int
	}
	var members []live
	for _, m := range b.Members {
		if o, ok := c.usable(m.OutboundID); ok {
			w := m.Weight
			if w < 1 || w > egress.MaxWeight {
				w = 1
			}
			members = append(members, live{id: o.ID, chain: c.outboundChainFor(o), weight: w})
		}
	}
	if len(members) == 0 {
		if b.Fallback == egress.FallbackBlock {
			return chainBlock
		}
		return chainDirect
	}
	if b.Strategy == egress.StrategyFailover || len(members) == 1 {
		return members[0].chain
	}
	shares := make([]int, len(members))
	for i, m := range members {
		shares[i] = m.weight
	}
	if b.Strategy == egress.StrategyLeastConn {
		loads := make([]int64, len(members))
		for i, m := range members {
			loads[i] = c.load[m.id]
		}
		shares = leastConnShares(shares, loads)
	}
	var buckets []string
	for i, m := range members {
		for n := 0; n < shares[i]; n++ {
			buckets = append(buckets, m.chain)
		}
	}
	entries := make([]string, len(buckets))
	for i, chain := range buckets {
		entries[i] = fmt.Sprintf("%d : goto %s", i, chain)
	}
	// Sticky hashes the client tunnel address (one exit per device); the
	// per-connection strategies draw a counter or random number per new flow.
	// The sticky hash has a fixed seed: without one the kernel draws a new
	// seed every time the rule is loaded, and every apply would move devices
	// to another exit.
	selector := fmt.Sprintf("jhash ip saddr mod %d seed 0x%x", len(buckets), stickySeed)
	switch b.Strategy {
	case egress.StrategyRoundRobin:
		selector = fmt.Sprintf("numgen inc mod %d", len(buckets))
	case egress.StrategyRandom, egress.StrategyLeastConn:
		selector = fmt.Sprintf("numgen random mod %d", len(buckets))
	}
	name := balancerChain(b.ID)
	c.plan.Chains[name] = fmt.Sprintf("%s vmap { %s }", selector, strings.Join(entries, ", "))
	return name
}

// stickySeed keeps the sticky balancer's device-to-bucket mapping stable
// across rule reloads and restarts.
const stickySeed = 0x5747_4744

// leastConnBuckets bounds the least-connections bucket map.
const leastConnBuckets = 20

// leastConnShares turns weights and active connection counts into bucket
// shares: a member's share of new connections falls as its load per unit of
// weight rises, and every live member keeps at least one bucket so its count
// keeps being observed.
func leastConnShares(weights []int, loads []int64) []int {
	scores := make([]float64, len(weights))
	total := 0.0
	for i, w := range weights {
		perWeight := float64(loads[i]) / float64(w)
		scores[i] = float64(w) / (1 + perWeight)
		total += scores[i]
	}
	shares := make([]int, len(weights))
	for i := range weights {
		shares[i] = max(1, int(scores[i]/total*leastConnBuckets+0.5))
	}
	return shares
}

// rules compiles ordered rules into sets, classification and the resolver
// ruleset.
func (c *compiler) rules(geo Geo) {
	type parsed struct {
		rule     *egress.Rule
		matchers []egress.Matcher
	}
	var (
		list      []parsed
		geosites  = map[string]bool{}
		geoips    = map[string]bool{}
		siteNames []string
		ipNames   []string
	)
	for _, r := range c.snap.Rules {
		if !r.Enabled {
			continue
		}
		ms, err := egress.ParseMatchers(r.Matchers)
		if err != nil {
			continue // validated at save; a stored invalid rule is skipped, never half-applied
		}
		ms = append(egress.PresetMatchers(r.Presets), ms...)
		for _, m := range ms {
			switch m.Type {
			case egress.MatchGeosite:
				if !geosites[m.Value] {
					geosites[m.Value] = true
					siteNames = append(siteNames, m.Value)
				}
			case egress.MatchGeoIP:
				if !geoips[m.Value] {
					geoips[m.Value] = true
					ipNames = append(ipNames, m.Value)
				}
			}
		}
		list = append(list, parsed{rule: r, matchers: ms})
	}
	var sites map[string][]egress.Matcher
	var ips map[string][]netip.Prefix
	if geo != nil {
		var err error
		if sites, err = geo.Domains(siteNames); err != nil {
			c.plan.Warnings = append(c.plan.Warnings, Warning{Kind: "geodata_unavailable", Detail: "geosite"})
		}
		if ips, err = geo.Prefixes(ipNames); err != nil {
			c.plan.Warnings = append(c.plan.Warnings, Warning{Kind: "geodata_unavailable", Detail: "geoip"})
		}
	}
	for _, name := range siteNames {
		if _, ok := sites[name]; !ok {
			c.plan.Warnings = append(c.plan.Warnings, Warning{Kind: "geosite_missing", Detail: name})
		}
	}
	for _, name := range ipNames {
		if _, ok := ips[name]; !ok {
			c.plan.Warnings = append(c.plan.Warnings, Warning{Kind: "geoip_missing", Detail: name})
		}
	}
	for _, p := range list {
		var domains []egress.Matcher
		var prefixes []netip.Prefix
		for _, m := range p.matchers {
			switch m.Type {
			case egress.MatchGeosite:
				domains = append(domains, sites[m.Value]...)
			case egress.MatchGeoIP:
				prefixes = append(prefixes, ips[m.Value]...)
			case egress.MatchCIDR:
				prefixes = append(prefixes, netip.MustParsePrefix(m.Value))
			default:
				domains = append(domains, m)
			}
		}
		if len(domains) == 0 && len(prefixes) == 0 {
			continue
		}
		chain := c.targetChain(p.rule.Target)
		if d := match.NewDomains(domains); d.Len() > 0 {
			set := DynamicSetName(p.rule.ID)
			c.plan.DynamicSets = append(c.plan.DynamicSets, set)
			c.plan.Ruleset.Entries = append(c.plan.Ruleset.Entries, match.Entry{RuleID: p.rule.ID, SetName: set, Domains: d})
			c.plan.Classify = append(c.plan.Classify, ClassifyRule{RuleID: p.rule.ID, Set: set, Chain: chain})
		}
		if len(prefixes) > 0 {
			set := staticSetName(p.rule.ID)
			c.plan.StaticSets[set] = prefixes
			c.plan.Classify = append(c.plan.Classify, ClassifyRule{RuleID: p.rule.ID, Set: set, Chain: chain})
		}
	}
}

// pruneChains keeps only target chains something jumps to (balancer member
// chains are referenced from their vmap).
func (c *compiler) pruneChains() {
	used := map[string]bool{chainDirect: true, chainBlock: true, c.plan.DefaultChain: true}
	for _, r := range c.plan.Classify {
		used[r.Chain] = true
	}
	for name, body := range c.plan.Chains {
		if strings.HasPrefix(name, "t_b") && used[name] {
			for _, field := range strings.Fields(body) {
				if strings.HasPrefix(field, "t_o") {
					used[strings.TrimSuffix(field, ",")] = true
				}
			}
		}
	}
	for name := range c.plan.Chains {
		if !used[name] {
			delete(c.plan.Chains, name)
		}
	}
}

// keepalive is the peer interval: off, a fixed number of seconds, or an
// AmneziaWG random interval low–high.
func keepalive(low, high int) awgparam.U16Range {
	if low <= 0 || low > 65535 {
		return awgparam.U16Range{}
	}
	if high > low && high <= 65535 {
		if r, err := awgparam.NewU16Range(uint16(low), uint16(high)); err == nil {
			return r
		}
	}
	return awgparam.ScalarU16(uint16(low))
}
