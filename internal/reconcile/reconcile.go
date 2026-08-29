// Package reconcile makes the tunnel backend match the database — the DB is
// the source of truth (docs/architecture/overview.md). It runs at boot, as a
// cheap drift check on the accounting cycle, and on demand from `doctor
// --fix`. Enabled interfaces and peer-eligible users' devices are applied;
// everything else is removed from interfaces WG-Guard owns. Unknown peers on
// owned interfaces follow the drift policy (report | adopt | remove);
// interfaces unknown to the DB are never touched — ownership requires a DB
// record (ADR-0004).
package reconcile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Policy is the drift policy (mirrors the drift.policy setting).
type Policy string

const (
	PolicyReport Policy = "report"
	PolicyAdopt  Policy = "adopt"
	PolicyRemove Policy = "remove"
)

// Engine reconciles backend state to DB state.
type Engine struct {
	DB      *database.DB
	Backend tunnel.Backend
	Ring    *secrets.KeyRing
	Policy  Policy
}

// DriftItem describes one observed difference and the action taken.
type DriftItem struct {
	Interface string
	Kind      string // missing_interface|mode_transition|param_drift|missing_peer|unknown_peer|foreign_interface|unwanted_interface
	Detail    string
	Action    string // created|applied|added|removed|adopt|reported|recreated|none
}

// InterfaceError is a per-interface failure. Reconcile continues with the
// other interfaces after collecting it: one broken profile must not take
// down the whole bring-up (the operator still sees every error).
type InterfaceError struct {
	Interface string
	Err       string
}

// Report summarizes one reconcile pass.
type Report struct {
	InterfacesCreated int
	InterfacesUpdated int
	InterfacesRemoved int
	PeersAdded        int
	PeersRemoved      int
	PeersUpdated      int
	Drift             []DriftItem
	Errors            []InterfaceError
	Duration          time.Duration
}

// Run performs one full pass. DB-level failures abort the pass (nothing can
// be trusted); backend failures on a single interface are collected in
// Report.Errors and the pass continues with the remaining interfaces.
func (e *Engine) Run(ctx context.Context) (*Report, error) {
	start := time.Now()
	rep := &Report{}

	ifaces, err := e.loadInterfaces(ctx)
	if err != nil {
		return nil, err
	}
	desired, knownKeys, err := e.loadDesiredPeers(ctx)
	if err != nil {
		return nil, err
	}

	for _, ifc := range ifaces {
		if ifc.Enabled {
			if err := e.reconcileInterface(ctx, ifc, desired[ifc.ID], knownKeys, rep); err != nil {
				rep.Errors = append(rep.Errors, InterfaceError{Interface: ifc.Name, Err: err.Error()})
			}
			continue
		}
		// Disabled in DB but present in backend → remove (DB owns the name).
		_, err := e.Backend.Dump(ctx, ifc.Name)
		switch {
		case err == nil:
			if rmErr := e.Backend.RemoveInterface(ctx, ifc.Name); rmErr != nil && !errors.Is(rmErr, tunnel.ErrInterfaceNotFound) {
				rep.Errors = append(rep.Errors, InterfaceError{Interface: ifc.Name, Err: rmErr.Error()})
				continue
			}
			rep.InterfacesRemoved++
			rep.Drift = append(rep.Drift, DriftItem{
				Interface: ifc.Name, Kind: "unwanted_interface",
				Detail: "profile disabled in panel", Action: "removed",
			})
		case errors.Is(err, tunnel.ErrInterfaceNotFound):
			// Already absent: nothing to do.
		default:
			rep.Errors = append(rep.Errors, InterfaceError{Interface: ifc.Name, Err: err.Error()})
		}
	}

	// Foreign interfaces: report only, never touch.
	names, err := e.Backend.ListInterfaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("reconcile: list backend interfaces: %w", err)
	}
	owned := map[string]bool{}
	for _, ifc := range ifaces {
		owned[ifc.Name] = true
	}
	for _, name := range names {
		if !owned[name] {
			rep.Drift = append(rep.Drift, DriftItem{
				Interface: name, Kind: "foreign_interface",
				Detail: "exists in backend but not in WG-Guard's database", Action: "none",
			})
		}
	}

	sort.Slice(rep.Drift, func(i, j int) bool { return rep.Drift[i].Interface < rep.Drift[j].Interface })
	rep.Duration = time.Since(start)
	return rep, nil
}

// reconcileInterface creates or updates one enabled interface and syncs its
// peer set to the desired devices. knownKeys is the set of public keys the
// DB has device rows for: a peer carrying one of them but not desired is
// *stale* (disabled device or user) and is removed — the drift policy
// governs only keys WG-Guard has never seen.
//
// Obfuscation-mode transitions (plain ↔ obfuscated) recreate the link: the
// pinned runtime accepts explicit zero obfuscation params with EINVAL and
// keeps old params when the block is omitted, so the all-plain state is only
// reachable at link creation (verified against amneziawg-go v3.1, WSL2
// 2026-08-29). Recreating also clears non-WG-Guard peers — their PSKs are
// unknowable, so they cannot be reconstructed.
func (e *Engine) reconcileInterface(ctx context.Context, ifc *dbInterface, desired []peerDesire, knownKeys map[string]bool, rep *Report) error {
	spec := tunnel.InterfaceSpec{
		Name:        ifc.Name,
		ListenPort:  ifc.ListenPort,
		MTU:         ifc.MTU,
		Address:     gatewayAddress(ifc.Subnet),
		Obfuscation: toTunnelObfuscation(ifc.Obf),
	}
	wantCfg := tunnel.InterfaceConfig{
		PrivateKey:  ifc.PrivateKey,
		ListenPort:  ifc.ListenPort,
		Obfuscation: spec.Obfuscation,
	}

	var created bool
	state, err := e.Backend.Dump(ctx, ifc.Name)
	switch {
	case errors.Is(err, tunnel.ErrInterfaceNotFound):
		created = true
		if err := e.Backend.CreateInterface(ctx, spec); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		if err := e.Backend.ApplyInterfaceConfig(ctx, ifc.Name, wantCfg); err != nil {
			return fmt.Errorf("apply: %w", err)
		}
		rep.InterfacesCreated++
		rep.Drift = append(rep.Drift, DriftItem{
			Interface: ifc.Name, Kind: "missing_interface",
			Detail: "recreated from DB", Action: "created",
		})
	case err != nil:
		return fmt.Errorf("dump: %w", err)
	default:
		modeChanged := state.Obfuscation.Enabled != spec.Obfuscation.Enabled
		paramDrift := state.ListenPort != ifc.ListenPort || state.Obfuscation != spec.Obfuscation
		switch {
		case modeChanged:
			// Recreate: setconf cannot move between plain and obfuscated
			// states (see above). Peers are wiped and re-synced below.
			if err := e.Backend.RemoveInterface(ctx, ifc.Name); err != nil {
				return fmt.Errorf("recreate remove: %w", err)
			}
			if err := e.Backend.CreateInterface(ctx, spec); err != nil {
				return fmt.Errorf("recreate create: %w", err)
			}
			rep.InterfacesUpdated++
			created = true
			rep.Drift = append(rep.Drift, DriftItem{
				Interface: ifc.Name, Kind: "mode_transition",
				Detail: fmt.Sprintf("obfuscation mode changed (backend plain=%v, DB plain=%v); link recreated, peers re-synced",
					!state.Obfuscation.Enabled, !spec.Obfuscation.Enabled),
				Action: "recreated",
			})
		case paramDrift:
			if err := e.Backend.ApplyInterfaceConfig(ctx, ifc.Name, wantCfg); err != nil {
				return fmt.Errorf("apply: %w", err)
			}
			rep.InterfacesUpdated++
			rep.Drift = append(rep.Drift, DriftItem{
				Interface: ifc.Name, Kind: "param_drift",
				Detail: fmt.Sprintf("port/params corrected (backend port %d, DB port %d)",
					state.ListenPort, ifc.ListenPort),
				Action: "applied",
			})
		}
	}

	// Peer diff: desired (enabled devices) vs observed. After a create or
	// recreate the observed state is empty/wiped: desired peers are synced
	// wholesale, and stale/unknown peers are simply gone (the
	// mode_transition drift item records it).
	want := map[string]bool{}
	have := map[string]tunnel.PeerState{}
	for _, p := range state.Peers {
		have[p.PublicKey] = p
	}
	sync := make([]tunnel.PeerConfig, 0, len(desired)+len(have))
	dirty := created

	for _, d := range desired {
		want[d.PublicKey] = true
		cfg := tunnel.PeerConfig{
			PublicKey:    d.PublicKey,
			PresharedKey: d.PresharedKey,
			AllowedIPs:   []string{d.AllowedIP},
		}
		sync = append(sync, cfg)
		if created {
			// Fresh state: every desired peer is simply added (the
			// missing_interface/mode_transition drift item covers the why).
			rep.PeersAdded++
			continue
		}
		existing, ok := have[d.PublicKey]
		switch {
		case !ok:
			dirty = true
			rep.PeersAdded++
			rep.Drift = append(rep.Drift, DriftItem{
				Interface: ifc.Name, Kind: "missing_peer",
				Detail: fmt.Sprintf("device %s (%s) missing in backend", d.DeviceName, d.AllowedIP),
				Action: "added",
			})
		case !sameAllowedIPs(existing.AllowedIPs, []string{d.AllowedIP}):
			dirty = true
			rep.PeersUpdated++
			rep.Drift = append(rep.Drift, DriftItem{
				Interface: ifc.Name, Kind: "missing_peer",
				Detail: fmt.Sprintf("device %s allowed-IPs corrected (backend %v, DB %s)",
					d.DeviceName, existing.AllowedIPs, d.AllowedIP),
				Action: "added",
			})
		}
	}
	if !created {
		for key, st := range have {
			if want[key] {
				continue
			}
			if knownKeys[key] {
				// Known device, no longer peer-eligible (disabled device or
				// user, expired, deleted…): the DB is the source of truth.
				dirty = true
				rep.PeersRemoved++
				rep.Drift = append(rep.Drift, DriftItem{
					Interface: ifc.Name, Kind: "stale_peer",
					Detail: fmt.Sprintf("device peer %s removed (not peer-eligible in DB)", describe(key, st)),
					Action: "removed",
				})
				continue
			}
			// Truly unknown peer on an owned interface: drift policy decides.
			// Under report/adopt the peer is passed back to SyncPeers without
			// a PSK (omitted PSK keeps the stored one — syncconf semantics).
			switch e.Policy {
			case PolicyRemove:
				dirty = true
				rep.PeersRemoved++
				rep.Drift = append(rep.Drift, DriftItem{
					Interface: ifc.Name, Kind: "unknown_peer",
					Detail: fmt.Sprintf("unknown peer %s removed (drift policy)", describe(key, st)),
					Action: "removed",
				})
			case PolicyAdopt:
				sync = append(sync, tunnel.PeerConfig{
					PublicKey:        key,
					AllowedIPs:       st.AllowedIPs,
					KeepaliveSeconds: st.KeepaliveSeconds,
				})
				rep.Drift = append(rep.Drift, DriftItem{
					Interface: ifc.Name, Kind: "unknown_peer",
					Detail: fmt.Sprintf("unknown peer %s adopted (drift policy)", describe(key, st)),
					Action: "adopt",
				})
			default: // report
				sync = append(sync, tunnel.PeerConfig{
					PublicKey:        key,
					AllowedIPs:       st.AllowedIPs,
					KeepaliveSeconds: st.KeepaliveSeconds,
				})
				rep.Drift = append(rep.Drift, DriftItem{
					Interface: ifc.Name, Kind: "unknown_peer",
					Detail: fmt.Sprintf("unknown peer %s reported (drift policy)", describe(key, st)),
					Action: "reported",
				})
			}
		}
	}

	// Sync only when there is something to apply (no pointless syncconf
	// churn every cycle).
	if dirty {
		if err := e.Backend.SyncPeers(ctx, ifc.Name, sync); err != nil {
			return fmt.Errorf("sync: %w", err)
		}
	}
	return nil
}

func describe(key string, st tunnel.PeerState) string {
	if len(st.AllowedIPs) > 0 {
		return st.AllowedIPs[0]
	}
	if len(key) > 12 {
		return key[:12] + "…"
	}
	return key
}

func sameAllowedIPs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dbInterface is the engine's view of a tunnel_interfaces row.
type dbInterface struct {
	ID         string
	Name       string
	ListenPort int
	Subnet     string // device pool CIDR, e.g. "10.8.0.0/24"
	MTU        int
	Obf        obfuscation
	Enabled    bool
	PrivateKey string // decrypted for backend apply
}

// gatewayAddress derives the interface address (first host of the pool) —
// the same address the device allocator reserves (.1, internal/iface).
// Derived, not stored: it is a pure function of the pool.
func gatewayAddress(subnet string) string {
	p, err := netip.ParsePrefix(subnet)
	if err != nil {
		return "" // invalid pool: creation fails later with a clear error
	}
	return netip.PrefixFrom(p.Addr().Next(), p.Bits()).String()
}

type obfuscation struct {
	Enabled        bool
	Jc             int
	Jmin, Jmax     int
	S1, S2         int
	H1, H2, H3, H4 uint32
}

func toTunnelObfuscation(o obfuscation) tunnel.Obfuscation {
	return tunnel.Obfuscation{
		Enabled: o.Enabled,
		Jc:      o.Jc, Jmin: o.Jmin, Jmax: o.Jmax,
		S1: o.S1, S2: o.S2,
		H1: o.H1, H2: o.H2, H3: o.H3, H4: o.H4,
	}
}

// peerDesire is one device that should exist as a peer.
type peerDesire struct {
	PublicKey    string
	PresharedKey string
	AllowedIP    string
	DeviceName   string
}

func (e *Engine) loadInterfaces(ctx context.Context) ([]*dbInterface, error) {
	rows, err := e.DB.QueryContext(ctx, `SELECT id, name, listen_port, ipv4_subnet, mtu, public_key,
		private_key_encrypted, jc, jmin, jmax, s1, s2, h1, h2, h3, h4, enabled
		FROM tunnel_interfaces ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("reconcile: load interfaces: %w", err)
	}
	defer rows.Close()
	var out []*dbInterface
	for rows.Next() {
		var (
			ifc                  dbInterface
			pubkey               string
			privEnc              []byte
			jc, jmin, jm, s1, s2 sql.NullInt64
			h1, h2, h3, h4       sql.NullInt64
		)
		if err := rows.Scan(&ifc.ID, &ifc.Name, &ifc.ListenPort, &ifc.Subnet, &ifc.MTU, &pubkey, &privEnc,
			&jc, &jmin, &jm, &s1, &s2, &h1, &h2, &h3, &h4, &ifc.Enabled); err != nil {
			return nil, fmt.Errorf("reconcile: scan interface: %w", err)
		}
		_ = pubkey // drift on the server key is covered by private-key apply
		ifc.Obf = obfuscation{
			Enabled: jc.Valid,
			Jc:      int(jc.Int64), Jmin: int(jmin.Int64), Jmax: int(jm.Int64),
			S1: int(s1.Int64), S2: int(s2.Int64),
			H1: uint32(h1.Int64), H2: uint32(h2.Int64), H3: uint32(h3.Int64), H4: uint32(h4.Int64),
		}
		pt, err := e.Ring.Decrypt(privEnc)
		if err != nil {
			return nil, fmt.Errorf("reconcile: decrypt private key of %s: %w", ifc.Name, err)
		}
		ifc.PrivateKey = string(pt)
		out = append(out, &ifc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconcile: load interfaces: %w", err)
	}
	return out, nil
}

// loadDesiredPeers returns, per interface ID, the peers that should exist:
// enabled devices of enabled, live users whose status wants peers. It also
// returns every public key the DB has a device row for (stale-peer
// detection).
func (e *Engine) loadDesiredPeers(ctx context.Context) (map[string][]peerDesire, map[string]bool, error) {
	rows, err := e.DB.QueryContext(ctx, `SELECT d.id, d.interface_id, d.name, d.ipv4_address,
		d.public_key, d.preshared_key_encrypted
		FROM devices d JOIN users u ON u.id = d.user_id
		WHERE u.deleted_at IS NULL AND u.enabled = 1 AND d.enabled = 1
		  AND u.status IN ('active', 'waiting_first_connection')`)
	if err != nil {
		return nil, nil, fmt.Errorf("reconcile: load devices: %w", err)
	}
	type row struct {
		id, ifaceID, name, ipv4, pubkey string
		psk                             []byte
	}
	var rs []row
	for rows.Next() {
		var r row
		var psk []byte
		if err := rows.Scan(&r.id, &r.ifaceID, &r.name, &r.ipv4, &r.pubkey, &psk); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("reconcile: scan device: %w", err)
		}
		r.psk = psk
		rs = append(rs, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, fmt.Errorf("reconcile: load devices: %w", err)
	}
	rows.Close()

	knownKeys := map[string]bool{}
	keyRows, err := e.DB.QueryContext(ctx, `SELECT public_key FROM devices`)
	if err != nil {
		return nil, nil, fmt.Errorf("reconcile: load device keys: %w", err)
	}
	defer keyRows.Close()
	for keyRows.Next() {
		var k string
		if err := keyRows.Scan(&k); err != nil {
			return nil, nil, fmt.Errorf("reconcile: scan device keys: %w", err)
		}
		knownKeys[k] = true
	}
	if err := keyRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("reconcile: load device keys: %w", err)
	}

	out := map[string][]peerDesire{}
	for _, r := range rs {
		p := peerDesire{
			PublicKey:  r.pubkey,
			AllowedIP:  r.ipv4,
			DeviceName: r.name,
		}
		if len(r.psk) > 0 {
			pt, err := e.Ring.Decrypt(r.psk)
			if err != nil {
				return nil, nil, fmt.Errorf("reconcile: decrypt psk for device %s: %w", r.id, err)
			}
			p.PresharedKey = string(pt)
		}
		out[r.ifaceID] = append(out[r.ifaceID], p)
	}
	return out, knownKeys, nil
}
