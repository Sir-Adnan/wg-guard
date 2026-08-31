// Package tunnel defines the TunnelBackend abstraction: the seam between
// domain services and the AmneziaWG implementation (ADR-0001 — AWG is driven
// through its pinned CLI as a subprocess; nothing AWG-specific leaks above
// this interface). internal/tunnel/amneziawg implements it against the real
// CLI in Phase 2; tunnel/fake is the in-memory implementation for tests and
// dev without root.
package tunnel

import (
	"context"
	"errors"
	"time"
)

// ErrInterfaceNotFound is returned when the named interface does not exist
// in the backend (distinct from DB-not-found domain errors).
var ErrInterfaceNotFound = errors.New("tunnel: interface not found")

// Obfuscation carries the AWG obfuscation parameters for one interface (one
// profile per interface — ADR-0002). Enabled=false means a plain WireGuard
// configuration: every parameter is omitted upstream. Field semantics and
// constraints are pinned in docs/integrations/amneziawg.md; validation of
// the kernel-README constraint set happens in the interface service before
// anything reaches a backend.
//
// The 2.0/3.x-generation fields below are capability-gated: the pinned tools
// parser and Ubuntu 24.04 kernel accept them, but client compatibility varies.
// Phase 8 classifies the full runtime/client contract. They render only when
// set, and a runtime that silently ignores them surfaces through
// verify-after-apply.
type Obfuscation struct {
	Enabled            bool
	Jc                 int
	Jmin, Jmax         int
	S1, S2             int
	H1, H2, H3, H4     uint32
	I1, I2, I3, I4, I5 string // hex blobs, "" = unset (client-side only params)

	S3, S4                 int    // plain u16 when set
	HeaderProtectionKey    string // base64 32-byte key, "" = disabled
	ContentPaddingAddition string // "N" or "N-M" (u16 bounds), "" = disabled
	RekeyAfterTime         string // seconds, "N" or "N-M", "" = upstream default
	RekeyTimeout           string
	RejectAfterTime        string
	KeepaliveTimeout       string
	MaxHandshakeAttempts   string
	RandomTrailers         bool // rendered as "on" (upstream panic history — default off)
	DisableCookies         bool // rendered as "on" (security implications — default off)
}

// legacyVerified returns the subset of fields whose runtime behavior is
// verified end-to-end (the legacy 1.0 parameter set). Drift classification
// recreates links only on verified-set mismatches; capability-gated 2.0/3.x
// parameters are report-only so an unverified runtime can never thrash the
// tunnel with recreate loops (amneziawg.md).
func (o Obfuscation) LegacyVerified() Obfuscation {
	return Obfuscation{
		Enabled: o.Enabled,
		Jc:      o.Jc, Jmin: o.Jmin, Jmax: o.Jmax,
		S1: o.S1, S2: o.S2,
		H1: o.H1, H2: o.H2, H3: o.H3, H4: o.H4,
		I1: o.I1, I2: o.I2, I3: o.I3, I4: o.I4, I5: o.I5,
	}
}

// InterfaceSpec is the identity-level configuration of a backend interface.
// Address was added in Phase 2: a backend that creates the link (kernel path)
// must assign the interface gateway address at bring-up, and the spec is the
// only state the backend receives — the phase-1 draft omitted it.
type InterfaceSpec struct {
	Name        string
	PrivateKey  string // base64
	ListenPort  int
	Fwmark      string // "" = off
	MTU         int
	Address     string // interface gateway CIDR, e.g. "10.8.0.1/24"
	Obfuscation Obfuscation
}

// PeerConfig is the desired state of one peer.
type PeerConfig struct {
	PublicKey        string // base64
	PresharedKey     string // base64, "" = none
	AllowedIPs       []string
	Endpoint         string // host:port, "" = none (server side: usually unset)
	KeepaliveSeconds int    // 0 = off
}

// InterfaceConfig is a full interface configuration: setconf semantics
// (complete replace).
type InterfaceConfig struct {
	PrivateKey  string
	ListenPort  int
	Fwmark      string
	Obfuscation Obfuscation
	Peers       []PeerConfig
}

// PeerState is one peer as observed from the backend.
type PeerState struct {
	PublicKey        string
	PresharedKeySet  bool
	Endpoint         string
	AllowedIPs       []string
	LastHandshake    time.Time // zero = never
	RXBytes          uint64
	TXBytes          uint64
	KeepaliveSeconds int
}

// InterfaceState is the observed state of one interface (dump semantics).
type InterfaceState struct {
	Name        string
	PublicKey   string
	ListenPort  int
	Obfuscation Obfuscation
	Peers       []PeerState
}

// Backend is the control-plane surface WG-Guard needs from the tunnel
// engine. Implementations must be safe for concurrent use.
type Backend interface {
	// ListInterfaces returns the names of interfaces visible to the backend.
	ListInterfaces(ctx context.Context) ([]string, error)
	// CreateInterface brings up a new interface with the given spec (link
	// + config, no peers).
	CreateInterface(ctx context.Context, spec InterfaceSpec) error
	// RemoveInterface tears down and removes the interface.
	RemoveInterface(ctx context.Context, name string) error
	// ApplyInterfaceConfig fully replaces the interface configuration
	// (setconf semantics; active handshakes may reset).
	ApplyInterfaceConfig(ctx context.Context, name string, cfg InterfaceConfig) error
	// SyncPeers diff-applies the peer list without disturbing the interface
	// (syncconf semantics; active sessions survive).
	SyncPeers(ctx context.Context, name string, peers []PeerConfig) error
	// Dump returns the observed state (stats + handshakes + config snapshot).
	Dump(ctx context.Context, name string) (InterfaceState, error)
}
