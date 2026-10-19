// Package egress owns the desired state of egress routing: outbounds
// (alternate exits such as Cloudflare WARP, another WireGuard/AmneziaWG server
// or an additional public source address), balancers (groups of outbounds)
// and ordered first-match rules that send selected client traffic through
// them (docs/architecture/egress-routing.md).
//
// The package is storage and validation only. Kernel state is rendered from a
// Snapshot by internal/egress/routing; domain matching runs in the built-in
// resolver (internal/egress/dnsrouter). No proxy engine (Xray, sing-box) is
// involved: packets stay in the kernel and are steered by marks and policy
// routing.
package egress

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
)

// Kind is an outbound's transport.
type Kind string

const (
	// KindWARP is a Cloudflare WARP (or WARP+) WireGuard exit registered by
	// WG-Guard itself.
	KindWARP Kind = "warp"
	// KindWireGuard is an imported WireGuard or AmneziaWG client config.
	KindWireGuard Kind = "wireguard"
	// KindSourceIP keeps the host's own routing but rewrites the source to a
	// different public IPv4 address owned by this server.
	KindSourceIP Kind = "source_ip"
)

// Valid reports whether k is a known outbound kind.
func (k Kind) Valid() bool {
	switch k {
	case KindWARP, KindWireGuard, KindSourceIP:
		return true
	}
	return false
}

// Tunneled reports whether the kind owns a WireGuard-family link.
func (k Kind) Tunneled() bool { return k == KindWARP || k == KindWireGuard }

// TargetKind says where matched traffic goes.
type TargetKind string

const (
	TargetDirect   TargetKind = "direct"   // the server's own route and address
	TargetBlock    TargetKind = "block"    // rejected
	TargetOutbound TargetKind = "outbound" // one outbound
	TargetBalancer TargetKind = "balancer" // one balancer
)

// Target is a rule or default destination.
type Target struct {
	Kind TargetKind
	ID   string // outbound or balancer ID; empty for direct/block
}

// Direct is the zero-configuration target.
var Direct = Target{Kind: TargetDirect}

// String is the stable form-value encoding: "direct", "block",
// "outbound:<id>" or "balancer:<id>".
func (t Target) String() string {
	if t.ID == "" {
		return string(t.Kind)
	}
	return string(t.Kind) + ":" + t.ID
}

// ParseTarget decodes Target.String.
func ParseTarget(s string) (Target, error) {
	kind, id, _ := strings.Cut(strings.TrimSpace(s), ":")
	t := Target{Kind: TargetKind(kind), ID: id}
	switch t.Kind {
	case TargetDirect, TargetBlock:
		if id != "" {
			return Target{}, invalid("target", "target_invalid")
		}
	case TargetOutbound, TargetBalancer:
		if id == "" || len(id) > 64 {
			return Target{}, invalid("target", "target_invalid")
		}
	default:
		return Target{}, invalid("target", "target_invalid")
	}
	return t, nil
}

// TunnelConfig is the non-secret half of a WireGuard-family outbound.
type TunnelConfig struct {
	Address       string            `json:"address"` // local IPv4 CIDR inside the tunnel, e.g. 172.16.0.2/32
	MTU           int               `json:"mtu"`
	PeerPublicKey string            `json:"peer_public_key"`
	Endpoint      string            `json:"endpoint"`                // host:port
	Keepalive     int               `json:"keepalive"`               // seconds, 0 = off
	KeepaliveMax  int               `json:"keepalive_max,omitempty"` // >Keepalive: AmneziaWG random interval
	Obfuscation   iface.Obfuscation `json:"obfuscation"`
	// Via chains the tunnel through another tunneled outbound (its ID): the
	// tunnel's own packets leave through that outbound, so the exit appears
	// next to that outbound's server. One level only.
	Via string `json:"via,omitempty"`
	// Address6 is the tunnel's IPv6 address (/128), when the remote side
	// assigned one. Only the IPv6 site bridge uses it.
	Address6 string `json:"address6,omitempty"`
	// IPv6Sites turns on the IPv6 site bridge: client TCP connections to web
	// ports that this outbound carries are re-originated over the tunnel's
	// IPv6 when the site has an IPv6 address (ADR-0018).
	IPv6Sites bool `json:"ipv6_sites,omitempty"`
}

// Bridges reports whether the IPv6 site bridge is on and usable.
func (t *TunnelConfig) Bridges() bool { return t != nil && t.IPv6Sites && t.Address6 != "" }

// WARPAccount is non-secret WARP registration metadata.
type WARPAccount struct {
	DeviceID     string    `json:"device_id"`
	AccountType  string    `json:"account_type"` // free | limited | unlimited | team
	Plus         bool      `json:"plus"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Secret is the sealed half of an outbound. It never reaches templates, logs
// or the audit trail.
type Secret struct {
	PrivateKey   string `json:"private_key,omitempty"`
	PresharedKey string `json:"preshared_key,omitempty"`
	WARPToken    string `json:"warp_token,omitempty"`
	WARPLicense  string `json:"warp_license,omitempty"`
}

// Outbound is one alternate exit.
type Outbound struct {
	ID        string
	Name      string
	Kind      Kind
	Slot      int
	Enabled   bool
	Tunnel    *TunnelConfig // warp, wireguard
	SourceIP  string        // source_ip
	WARP      *WARPAccount  // warp
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LinkName is the kernel link a tunneled outbound owns.
func (o *Outbound) LinkName() string { return LinkName(o.Slot) }

// LinkPrefix names every egress-owned link. The tunnel reconcile engine treats
// these links as owned by egress routing, never as foreign drift.
const LinkPrefix = "wgo"

// MaxSlots bounds outbounds per node (marks, tables and links derive from it).
const MaxSlots = 63

// LinkName returns wgo<slot>.
func LinkName(slot int) string { return fmt.Sprintf("%s%d", LinkPrefix, slot) }

// IsLinkName reports whether name is an egress-owned link.
func IsLinkName(name string) bool {
	rest, ok := strings.CutPrefix(name, LinkPrefix)
	if !ok || rest == "" || len(rest) > 2 || rest[0] == '0' {
		return false
	}
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
	}
	return n >= 1 && n <= MaxSlots
}

// Strategy selects a balancer member.
type Strategy string

const (
	// StrategySticky spreads clients across healthy members by a hash of the
	// client tunnel address, so one device keeps one exit IP (sites such as
	// Google and AI services treat exit changes as suspicious).
	StrategySticky Strategy = "sticky"
	// StrategyRoundRobin hands each new connection to the next member
	// (weighted). One device may use several exit IPs.
	StrategyRoundRobin Strategy = "round_robin"
	// StrategyRandom picks a member per new connection at random (weighted).
	StrategyRandom Strategy = "random"
	// StrategyLeastConn sends new connections preferentially to the members
	// with the fewest active connections relative to their weight; shares
	// are recomputed from live connection counts on every health pass.
	StrategyLeastConn Strategy = "least_conn"
	// StrategyFailover uses the first healthy member in order.
	StrategyFailover Strategy = "failover"
)

// Strategies is the display order.
var Strategies = []Strategy{StrategySticky, StrategyRoundRobin, StrategyRandom, StrategyLeastConn, StrategyFailover}

func (s Strategy) Valid() bool {
	for _, v := range Strategies {
		if v == s {
			return true
		}
	}
	return false
}

// PerConnection reports whether one device may leave through several
// members (everything except sticky and failover).
func (s Strategy) PerConnection() bool {
	return s == StrategyRoundRobin || s == StrategyRandom || s == StrategyLeastConn
}

// Fallback applies when no member is healthy.
type Fallback string

const (
	FallbackDirect Fallback = "direct"
	FallbackBlock  Fallback = "block"
)

func (f Fallback) Valid() bool { return f == FallbackDirect || f == FallbackBlock }

// Member is one balancer entry.
type Member struct {
	OutboundID string `json:"outbound_id"`
	Weight     int    `json:"weight"` // 1..10; ignored by failover
}

// MaxWeight bounds one member's share in a sticky balancer.
const MaxWeight = 10

// Balancer groups outbounds.
type Balancer struct {
	ID        string
	Name      string
	Strategy  Strategy
	Fallback  Fallback
	Members   []Member
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Rule is one ordered routing rule.
type Rule struct {
	ID        string
	Name      string
	Position  int
	Enabled   bool
	Target    Target
	Presets   []string // built-in service IDs (presets.go)
	Matchers  string   // one matcher per line (see ParseMatchers)
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Settings are installation-wide routing options.
type Settings struct {
	// DefaultTarget receives client traffic that no rule matched.
	DefaultTarget Target
	// BlockEncryptedDNS stops DNS-over-TLS (853) and DNS-over-HTTPS to
	// well-known public resolvers from tunnel clients while domain rules are
	// active, so clients fall back to plain DNS that the resolver can see.
	BlockEncryptedDNS bool
	GeositeURL        string
	GeoIPURL          string
	UpdatedAt         time.Time
}

// ValidationError is a field-scoped, localizable validation failure. Reason is
// a stable catalog suffix (egress.error.<reason>); Detail optionally names the
// offending value (a matcher line) and is safe to display.
type ValidationError struct {
	Field  string
	Reason string
	Detail string
}

func (e *ValidationError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("egress: %s: %s (%s)", e.Field, e.Reason, e.Detail)
	}
	return fmt.Sprintf("egress: %s: %s", e.Field, e.Reason)
}

func invalid(field, reason string) error { return &ValidationError{Field: field, Reason: reason} }

func invalidDetail(field, reason, detail string) error {
	return &ValidationError{Field: field, Reason: reason, Detail: detail}
}

// AsValidation extracts a ValidationError.
func AsValidation(err error) (*ValidationError, bool) {
	var v *ValidationError
	ok := errors.As(err, &v)
	return v, ok
}

// ErrNotFound reports a missing outbound, balancer or rule.
var ErrNotFound = domain.E(domain.CodeNotFound, "egress object not found")

const maxNameLen = 48

func validName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return invalid("name", "name_required")
	}
	if len([]rune(name)) > maxNameLen || strings.ContainsAny(name, "\x00\r\n\t") {
		return invalid("name", "name_invalid")
	}
	return nil
}
