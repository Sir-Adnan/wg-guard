package amneziawg

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// dumpFieldCount is the interface-line field count of AWG v3.1 tools (stock
// WireGuard emits 4). The parser field-counts and rejects anything else
// loudly — a silent mis-parse would corrupt drift detection.
const (
	awgDumpFields    = 29
	wgDumpFields     = 4 // stock wireguard-tools: signature of a tools mismatch
	peerDumpFields   = 8
	handshakeNever   = 0
	peerLineSentinel = "(none)"
)

// parseDump parses `awg show <iface> dump` output: one 29-field interface
// line followed by 8-field peer lines, all tab-separated. Field order is
// pinned in docs/integrations/amneziawg.md (source: amneziawg-tools
// src/show.c dump_print).
func parseDump(name string, data []byte) (tunnel.InterfaceState, error) {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// Skip comment lines the fixture capture carries (defensive: real output
	// has none, but empty output is a hard error either way).
	var ifLine string
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		ifLine = ln
		break
	}
	if ifLine == "" {
		return tunnel.InterfaceState{}, fmt.Errorf("amneziawg: dump of %s is empty", name)
	}

	fields := strings.Split(ifLine, "\t")
	switch len(fields) {
	case awgDumpFields:
		// pinned format — parsed below
	case wgDumpFields:
		return tunnel.InterfaceState{}, fmt.Errorf(
			"amneziawg: %s produced a stock-WireGuard dump (4 fields): wrong tools on PATH, refusing to parse", name)
	default:
		return tunnel.InterfaceState{}, fmt.Errorf(
			"amneziawg: %s dump has %d interface fields, want %d (AWG v3.1) — unknown format, refusing to parse",
			name, len(fields), awgDumpFields)
	}

	st := tunnel.InterfaceState{Name: name, PublicKey: fields[1]}
	port, err := strconv.Atoi(fields[2])
	if err != nil {
		return tunnel.InterfaceState{}, parseErr(name, "listen_port", fields[2], err)
	}
	st.ListenPort = port

	obf, err := parseInterfaceObfuscation(name, fields)
	if err != nil {
		return tunnel.InterfaceState{}, err
	}
	st.Obfuscation = obf
	// fwmark (f[28]) stays unused: WG-Guard pins an explicit listen port and
	// does not manage fwmark.

	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" || strings.HasPrefix(ln, "#") || ln == ifLine {
			continue
		}
		peer, err := parsePeerLine(name, ln)
		if err != nil {
			return tunnel.InterfaceState{}, err
		}
		st.Peers = append(st.Peers, peer)
	}
	return st, nil
}

// parseInterfaceObfuscation reads Jc/Jmin/Jmax/S1/S2 (fields 4–8) and H1–H4
// (fields 11–14) and I1–I5 (fields 15–19).
func parseInterfaceObfuscation(name string, f []string) (tunnel.Obfuscation, error) {
	invalid := func(field, val string, err error) (tunnel.Obfuscation, error) {
		return tunnel.Obfuscation{}, parseErr(name, field, val, err)
	}
	jc, err := strconv.Atoi(f[3])
	if err != nil {
		return invalid("Jc", f[3], err)
	}
	jmin, err := strconv.Atoi(f[4])
	if err != nil {
		return invalid("Jmin", f[4], err)
	}
	jmax, err := strconv.Atoi(f[5])
	if err != nil {
		return invalid("Jmax", f[5], err)
	}
	s1, err := strconv.Atoi(f[6])
	if err != nil {
		return invalid("S1", f[6], err)
	}
	s2, err := strconv.Atoi(f[7])
	if err != nil {
		return invalid("S2", f[7], err)
	}
	o := tunnel.Obfuscation{Enabled: jc != 0, Jc: jc, Jmin: jmin, Jmax: jmax, S1: s1, S2: s2}
	for i := 0; i < 4; i++ {
		v, err := parseHeaderField(f[10+i])
		if err != nil {
			return invalid(fmt.Sprintf("H%d", i+1), f[10+i], err)
		}
		switch i {
		case 0:
			o.H1 = v
		case 1:
			o.H2 = v
		case 2:
			o.H3 = v
		case 3:
			o.H4 = v
		}
	}
	if jc == 0 {
		// Kernel-module baseline: a fresh plain interface dumps
		// H1..H4 = 1,2,3,4 — inert defaults that vanish the moment junk
		// packets are configured. The userspace daemon dumps zeros. Normalize
		// so the verify-after-apply gate and drift comparison compare the
		// applied plain profile against an equivalent observation (captured
		// on the VPS kernel, 2026-08-31; docs/integrations/amneziawg.md).
		o.H1, o.H2, o.H3, o.H4 = 0, 0, 0, 0
	}
	// I1–I5: hex blob or literal "(null)".
	is := [5]*string{&o.I1, &o.I2, &o.I3, &o.I4, &o.I5}
	for i := 0; i < 5; i++ {
		if v := f[14+i]; v != "(null)" {
			*is[i] = v
		}
	}
	// 2.0/3.x generation fields (capability-gated, amneziawg.md dump table):
	// f[8] S3, f[9] S4 (plain uints); f[19] header-protection key ("(none)"
	// when unset); f[20]–f[25] u16-range strings ("0" when unset); f[26]
	// random_trailers, f[27] disable_cookies ("on"/"off").
	if o.S3, err = strconv.Atoi(f[8]); err != nil {
		return invalid("S3", f[8], err)
	}
	if o.S4, err = strconv.Atoi(f[9]); err != nil {
		return invalid("S4", f[9], err)
	}
	if v := f[19]; v != "(none)" {
		o.HeaderProtectionKey = v
	}
	for i, dst := range []*string{&o.ContentPaddingAddition, &o.RekeyAfterTime,
		&o.RekeyTimeout, &o.RejectAfterTime, &o.KeepaliveTimeout, &o.MaxHandshakeAttempts} {
		if v := f[20+i]; v != "0" {
			*dst = v
		}
	}
	o.RandomTrailers = f[26] == "on"
	o.DisableCookies = f[27] == "on"
	return o, nil
}

// parseHeaderField accepts a plain number (verified against the pinned
// upstream) and tolerates the u32-range form "N-M" (low bound kept — a range
// can only come from a foreign configuration and reconciles back to a plain
// value).
func parseHeaderField(s string) (uint32, error) {
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

func parsePeerLine(name, line string) (tunnel.PeerState, error) {
	f := strings.Split(line, "\t")
	if len(f) != peerDumpFields {
		return tunnel.PeerState{}, fmt.Errorf(
			"amneziawg: %s peer line has %d fields, want %d — unknown format, refusing to parse",
			name, len(f), peerDumpFields)
	}
	invalid := func(field, val string, err error) (tunnel.PeerState, error) {
		return tunnel.PeerState{}, parseErr(name, field, val, err)
	}
	p := tunnel.PeerState{PublicKey: f[0], PresharedKeySet: f[1] != peerLineSentinel}
	if f[2] != peerLineSentinel {
		p.Endpoint = f[2]
	}
	if f[3] != peerLineSentinel {
		for _, cidr := range strings.Split(f[3], ",") {
			p.AllowedIPs = append(p.AllowedIPs, strings.TrimSpace(cidr))
		}
	}
	hs, err := strconv.ParseUint(f[4], 10, 64)
	if err != nil {
		return invalid("latest_handshake", f[4], err)
	}
	if hs != handshakeNever {
		p.LastHandshake = time.Unix(int64(hs), 0)
	}
	if p.RXBytes, err = strconv.ParseUint(f[5], 10, 64); err != nil {
		return invalid("rx_bytes", f[5], err)
	}
	if p.TXBytes, err = strconv.ParseUint(f[6], 10, 64); err != nil {
		return invalid("tx_bytes", f[6], err)
	}
	if f[7] != "off" {
		ka, err := strconv.Atoi(f[7])
		if err != nil {
			return invalid("persistent_keepalive", f[7], err)
		}
		p.KeepaliveSeconds = ka
	}
	return p, nil
}

func parseErr(iface, field, val string, err error) error {
	return fmt.Errorf("amneziawg: dump of %s: field %s = %q: %w", iface, field, val, err)
}
