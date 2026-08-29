// Package amneziawg implements tunnel.Backend against the pinned awg CLI
// (docs/integrations/amneziawg.md — nothing here may assume upstream behavior
// that document does not verify). Responsibilities: render awg configs, parse
// the AWG v3.1 29-field dump format, and drive interface lifecycle through
// iproute2 + `awg setconf`/`syncconf` with verify-after-apply.
//
// Deliberate design points:
//
//   - Config files are written to 0600 temp files and deleted immediately:
//     `awg setconf`/`syncconf` take a file path, and secrets via argv are
//     forbidden (docs/operations/security.md). The file lives for the duration
//     of one CLI call.
//   - ApplyInterfaceConfig verifies the result by dumping the interface
//     afterwards. The tools parser accepts more than the runtime enforces and
//     some reset semantics are unverified on the kernel backend; a post-apply
//     dump converts any silent mismatch into a hard error instead of an
//     endless reconcile-drift loop.
//   - Obfuscation parameters are written only for obfuscated profiles: the
//     pinned runtime rejects explicit zeros with EINVAL (verified, WSL2
//     2026-08-29), so plain profiles omit the block and rely on setconf
//     full-replace semantics — checked by the post-apply verify and by an
//     integration test against the real daemon.
package amneziawg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// renderSetconf renders a complete interface configuration (setconf
// semantics: full replace, including an explicit listen port — AWG issue
// #148 resets the port when it is left implicit).
func renderSetconf(cfg tunnel.InterfaceConfig) []byte {
	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	sb.WriteString("PrivateKey = " + cfg.PrivateKey + "\n")
	if cfg.ListenPort > 0 {
		sb.WriteString("ListenPort = " + strconv.Itoa(cfg.ListenPort) + "\n")
	}
	if cfg.Fwmark != "" {
		sb.WriteString("FwMark = " + cfg.Fwmark + "\n")
	}
	writeObfuscation(&sb, cfg.Obfuscation)
	for _, p := range cfg.Peers {
		writePeer(&sb, p)
	}
	return []byte(sb.String())
}

// renderSyncconf renders a peers-only configuration. Fields omitted from a
// peer section keep their stored value under syncconf semantics — so an
// omitted PresharedKey preserves the existing PSK (the reconcile engine
// relies on this to re-list unknown peers under report/adopt policies
// without knowing their PSK).
func renderSyncconf(peers []tunnel.PeerConfig) []byte {
	var sb strings.Builder
	for _, p := range peers {
		writePeer(&sb, p)
	}
	return []byte(sb.String())
}

func writeObfuscation(sb *strings.Builder, o tunnel.Obfuscation) {
	// Plain profiles omit the obfuscation block entirely: the runtime
	// REJECTS explicit zeros ("Unable to modify interface: Invalid
	// argument" — verified against the pinned daemon, WSL2 2026-08-29).
	// Omitted keys under setconf are expected to reset to zero (full-replace
	// semantics); ApplyInterfaceConfig's post-apply verify detects any
	// deviation and the obf→plain transition is integration-tested.
	if !o.Enabled {
		return
	}
	sb.WriteString("Jc = " + strconv.Itoa(o.Jc) + "\n")
	sb.WriteString("Jmin = " + strconv.Itoa(o.Jmin) + "\n")
	sb.WriteString("Jmax = " + strconv.Itoa(o.Jmax) + "\n")
	sb.WriteString("S1 = " + strconv.Itoa(o.S1) + "\n")
	sb.WriteString("S2 = " + strconv.Itoa(o.S2) + "\n")
	sb.WriteString("H1 = " + strconv.FormatUint(uint64(o.H1), 10) + "\n")
	sb.WriteString("H2 = " + strconv.FormatUint(uint64(o.H2), 10) + "\n")
	sb.WriteString("H3 = " + strconv.FormatUint(uint64(o.H3), 10) + "\n")
	sb.WriteString("H4 = " + strconv.FormatUint(uint64(o.H4), 10) + "\n")
	// I1–I5 are opt-in (iOS clients reject configs carrying them — upstream
	// issue #115); written only when the profile sets them.
	for i, v := range []string{o.I1, o.I2, o.I3, o.I4, o.I5} {
		if v != "" {
			sb.WriteString(fmt.Sprintf("I%d = %s\n", i+1, v))
		}
	}
}

func writePeer(sb *strings.Builder, p tunnel.PeerConfig) {
	sb.WriteString("\n[Peer]\n")
	sb.WriteString("PublicKey = " + p.PublicKey + "\n")
	if p.PresharedKey != "" {
		sb.WriteString("PresharedKey = " + p.PresharedKey + "\n")
	}
	if len(p.AllowedIPs) > 0 {
		sb.WriteString("AllowedIPs = " + strings.Join(p.AllowedIPs, ", ") + "\n")
	}
	if p.Endpoint != "" {
		sb.WriteString("Endpoint = " + p.Endpoint + "\n")
	}
	if p.KeepaliveSeconds > 0 {
		sb.WriteString("PersistentKeepalive = " + strconv.Itoa(p.KeepaliveSeconds) + "\n")
	}
}
