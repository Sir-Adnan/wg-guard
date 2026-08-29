// Package fake is an in-memory TunnelBackend for tests and development
// without root or the pinned AWG tooling. It models the semantics the real
// implementation is held to: setconf-style full replace, syncconf-style
// peer diff, dump observation, and interface existence. It is race-safe
// (exercised under -race) and records applied operations for assertions.
package fake

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// OpKind names a recorded backend operation.
type OpKind string

const (
	OpCreate OpKind = "create"
	OpRemove OpKind = "remove"
	OpApply  OpKind = "apply" // setconf
	OpSync   OpKind = "sync"  // syncconf
)

// Op is one recorded operation (test assertions).
type Op struct {
	Kind  OpKind
	Which string // interface name
	At    time.Time
}

// Backend is the fake implementation.
type Backend struct {
	mu         sync.RWMutex
	interfaces map[string]*fakeInterface
	ops        []Op
	// FailOn makes the next operation of that kind return an error
	// (failure-injection for reconcile/rollback tests).
	FailOn map[OpKind]error
}

type fakeInterface struct {
	spec      tunnel.InterfaceSpec
	cfg       tunnel.InterfaceConfig
	publicKey string
	peers     map[string]tunnel.PeerState
}

// New returns an empty fake backend.
func New() *Backend {
	return &Backend{
		interfaces: map[string]*fakeInterface{},
		FailOn:     map[OpKind]error{},
	}
}

func (b *Backend) fail(op OpKind) error {
	if err, ok := b.FailOn[op]; ok && err != nil {
		delete(b.FailOn, op)
		return err
	}
	return nil
}

func (b *Backend) record(kind OpKind, which string) {
	b.ops = append(b.ops, Op{Kind: kind, Which: which, At: time.Now()})
}

// Ops returns a copy of the recorded operations, oldest first.
func (b *Backend) Ops() []Op {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Op, len(b.ops))
	copy(out, b.ops)
	return out
}

// ResetOps clears the operation log.
func (b *Backend) ResetOps() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ops = nil
}

func (b *Backend) ListInterfaces(context.Context) ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.interfaces))
	for name := range b.interfaces {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (b *Backend) CreateInterface(_ context.Context, spec tunnel.InterfaceSpec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fail(OpCreate); err != nil {
		return err
	}
	if _, exists := b.interfaces[spec.Name]; exists {
		return fmt.Errorf("fake: interface %s already exists", spec.Name)
	}
	b.record(OpCreate, spec.Name)
	b.interfaces[spec.Name] = &fakeInterface{
		spec:  spec,
		peers: map[string]tunnel.PeerState{},
	}
	return nil
}

func (b *Backend) RemoveInterface(_ context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fail(OpRemove); err != nil {
		return err
	}
	if _, exists := b.interfaces[name]; !exists {
		return tunnel.ErrInterfaceNotFound
	}
	b.record(OpRemove, name)
	delete(b.interfaces, name)
	return nil
}

func (b *Backend) ApplyInterfaceConfig(_ context.Context, name string, cfg tunnel.InterfaceConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fail(OpApply); err != nil {
		return err
	}
	ifc, exists := b.interfaces[name]
	if !exists {
		return tunnel.ErrInterfaceNotFound
	}
	b.record(OpApply, name)
	ifc.cfg = cfg
	if cfg.ListenPort != 0 {
		ifc.spec.ListenPort = cfg.ListenPort
	}
	ifc.spec.Obfuscation = cfg.Obfuscation
	// setconf replaces the peer set entirely; counters/handshakes of
	// surviving peers are kept so accounting tests stay deterministic.
	peers := make(map[string]tunnel.PeerState, len(cfg.Peers))
	for _, p := range cfg.Peers {
		if old, ok := ifc.peers[p.PublicKey]; ok {
			st := peerState(p)
			st.LastHandshake, st.RXBytes, st.TXBytes = old.LastHandshake, old.RXBytes, old.TXBytes
			peers[p.PublicKey] = st
			continue
		}
		peers[p.PublicKey] = peerState(p)
	}
	ifc.peers = peers
	return nil
}

func (b *Backend) SyncPeers(_ context.Context, name string, peers []tunnel.PeerConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fail(OpSync); err != nil {
		return err
	}
	ifc, exists := b.interfaces[name]
	if !exists {
		return tunnel.ErrInterfaceNotFound
	}
	b.record(OpSync, name)
	// syncconf diff semantics: unknown peers removed, listed peers upserted.
	// An omitted PresharedKey keeps the stored one; runtime state (counters,
	// handshake) of re-listed peers survives.
	for key := range ifc.peers {
		if !containsPeer(peers, key) {
			delete(ifc.peers, key)
		}
	}
	for _, p := range peers {
		if old, ok := ifc.peers[p.PublicKey]; ok {
			st := peerState(p)
			if p.PresharedKey == "" {
				st.PresharedKeySet = old.PresharedKeySet
			}
			st.LastHandshake, st.RXBytes, st.TXBytes = old.LastHandshake, old.RXBytes, old.TXBytes
			ifc.peers[p.PublicKey] = st
			continue
		}
		ifc.peers[p.PublicKey] = peerState(p)
	}
	return nil
}

func containsPeer(peers []tunnel.PeerConfig, key string) bool {
	for _, p := range peers {
		if p.PublicKey == key {
			return true
		}
	}
	return false
}

func peerState(p tunnel.PeerConfig) tunnel.PeerState {
	return tunnel.PeerState{
		PublicKey:        p.PublicKey,
		PresharedKeySet:  p.PresharedKey != "",
		Endpoint:         p.Endpoint,
		AllowedIPs:       append([]string(nil), p.AllowedIPs...),
		KeepaliveSeconds: p.KeepaliveSeconds,
	}
}

func (b *Backend) Dump(_ context.Context, name string) (tunnel.InterfaceState, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	ifc, exists := b.interfaces[name]
	if !exists {
		return tunnel.InterfaceState{}, tunnel.ErrInterfaceNotFound
	}
	st := tunnel.InterfaceState{
		Name:        name,
		PublicKey:   ifc.publicKey,
		ListenPort:  ifc.spec.ListenPort,
		Obfuscation: ifc.spec.Obfuscation,
	}
	for _, p := range ifc.peers {
		st.Peers = append(st.Peers, p)
	}
	return st, nil
}

// SetPeerActivity simulates handshakes and counters (accounting tests).
func (b *Backend) SetPeerActivity(name, publicKey string, handshake time.Time, rx, tx uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	ifc, exists := b.interfaces[name]
	if !exists {
		return tunnel.ErrInterfaceNotFound
	}
	st, ok := ifc.peers[publicKey]
	if !ok {
		return fmt.Errorf("fake: peer %s not on %s", publicKey, name)
	}
	st.LastHandshake = handshake
	st.RXBytes = rx
	st.TXBytes = tx
	ifc.peers[publicKey] = st
	return nil
}

// SetPublicKey sets the interface public key shown in Dump (the fake cannot
// derive it from the private key; tests assert on it explicitly).
func (b *Backend) SetPublicKey(name, publicKey string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	ifc, exists := b.interfaces[name]
	if !exists {
		return tunnel.ErrInterfaceNotFound
	}
	ifc.publicKey = publicKey
	return nil
}
