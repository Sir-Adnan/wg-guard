package egress

import (
	"encoding/base64"
	"net/netip"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// MaxConfigBytes bounds an imported client configuration. Signature packets
// (I1–I5) that imitate QUIC or DNS are kilobytes of hex each.
const MaxConfigBytes = 64 << 10

// DefaultTunnelMTU is used when an imported config carries no MTU. WARP and
// most commercial WireGuard exits work at 1280; larger values are kept when
// the config says so.
const DefaultTunnelMTU = 1280

// maxSignatureBytes bounds one I1–I5 value.
const maxSignatureBytes = 16 << 10

// ParsedConfig is an imported WireGuard/AmneziaWG client configuration split
// into its public and secret halves.
type ParsedConfig struct {
	Tunnel TunnelConfig
	Secret Secret
	// Ignored names the keys that were understood and deliberately not used
	// (DNS, AllowedIPs, scripts, legacy AmneziaWG 1.5 keys), in file order.
	Ignored []string
}

// ParseClientConfig parses a wg-quick / awg-quick client config, the kind a
// VPN provider, another WG-Guard node, wgcf, AmneziaVPN or an AmneziaWG
// server exports. Lines are read the way the pinned amneziawg-tools parser
// reads them: `#` starts a comment anywhere on a line, keys are
// case-insensitive, whitespace is insignificant except inside the I1–I5
// signature values. Exactly one [Peer] is supported. DNS, AllowedIPs, Table
// and script hooks are not used: egress routing decides what enters the
// tunnel. Errors name the offending key but never echo a value.
func ParseClientConfig(text string) (ParsedConfig, error) {
	var out ParsedConfig
	if len(text) > MaxConfigBytes {
		return out, invalid("config", "config_too_large")
	}
	text = strings.TrimPrefix(text, "\uFEFF")
	p := configParser{out: &out, seen: map[string]bool{}}
	section := ""
	peers := 0
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if i := strings.IndexByte(raw, '#'); i >= 0 {
			raw = raw[:i]
		}
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Join(strings.Fields(line[1:len(line)-1]), ""))
			switch section {
			case "interface":
			case "peer":
				peers++
				if peers > 1 {
					return out, invalid("config", "config_multiple_peers")
				}
			default:
				return out, invalidDetail("config", "config_unknown_section", clip(line))
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return out, invalid("config", "config_invalid")
		}
		key = strings.ToLower(strings.Join(strings.Fields(key), ""))
		value = strings.TrimSpace(value)
		if !isSignatureKey(key) {
			value = strings.Join(strings.Fields(value), "")
		}
		var err error
		switch section {
		case "interface":
			err = p.interfaceKey(key, value)
		case "peer":
			err = p.peerKey(key, value)
		default:
			err = invalid("config", "config_invalid")
		}
		if err != nil {
			return out, err
		}
	}
	if peers == 0 {
		return out, invalidDetail("config", "config_missing", "[Peer]")
	}
	for _, need := range []struct{ key, name string }{
		{"privatekey", "PrivateKey"}, {"address", "Address"}, {"publickey", "PublicKey"}, {"endpoint", "Endpoint"},
	} {
		if !p.seen[need.key] {
			return out, invalidDetail("config", "config_missing", need.name)
		}
	}
	if out.Tunnel.MTU == 0 {
		out.Tunnel.MTU = DefaultTunnelMTU
	}
	if !p.seen["persistentkeepalive"] {
		// Absent means the usual NAT-friendly default; an explicit 0 is off.
		out.Tunnel.Keepalive = 25
	}
	obf, err := importedObfuscation(p.obf)
	if err != nil {
		return out, err
	}
	out.Tunnel.Obfuscation = obf
	return out, nil
}

type configParser struct {
	out  *ParsedConfig
	obf  iface.Obfuscation
	seen map[string]bool
}

// ignoredInterfaceKeys are understood and not used: routing, DNS and the
// listening side belong to WG-Guard. J1–J3 and Itime are AmneziaWG 1.5
// sender-side junk options that the pinned 2.x runtime no longer has; they
// never have to match the server, so leaving them out keeps the tunnel
// compatible.
var ignoredInterfaceKeys = map[string]string{
	"dns": "DNS", "listenport": "ListenPort", "table": "Table", "preup": "PreUp", "postup": "PostUp",
	"predown": "PreDown", "postdown": "PostDown", "saveconfig": "SaveConfig", "fwmark": "FwMark",
	"j1": "J1", "j2": "J2", "j3": "J3", "itime": "Itime",
}

func (p *configParser) interfaceKey(key, value string) error {
	if name, ok := ignoredInterfaceKeys[key]; ok {
		p.ignore(name)
		return nil
	}
	if p.seen[key] && key != "address" {
		return invalidDetail("config", "config_duplicate_key", displayKey(key))
	}
	p.seen[key] = true
	o := &p.obf
	switch key {
	case "privatekey":
		if tunnel.ValidatePrivateKey(value) != nil {
			return invalidDetail("config", "config_key", "PrivateKey")
		}
		p.out.Secret.PrivateKey = value
	case "address":
		return p.address(value)
	case "mtu":
		n, err := strconv.Atoi(value)
		if err != nil || n < 576 || n > 1500 {
			return invalid("config", "config_mtu")
		}
		p.out.Tunnel.MTU = n
	case "jc", "jmin", "jmax", "s1", "s2", "s3", "s4":
		n, err := strconv.ParseUint(value, 10, 16)
		if err != nil {
			return obfuscationError(key)
		}
		*map[string]*int{"jc": &o.Jc, "jmin": &o.Jmin, "jmax": &o.Jmax, "s1": &o.S1, "s2": &o.S2, "s3": &o.S3, "s4": &o.S4}[key] = int(n)
	case "h1", "h2", "h3", "h4":
		r, err := awgparam.ParseU32Range(value)
		if err != nil || r.Low() == 0 {
			return obfuscationError(key)
		}
		*map[string]*awgparam.U32Range{"h1": &o.H1, "h2": &o.H2, "h3": &o.H3, "h4": &o.H4}[key] = r
	case "i1", "i2", "i3", "i4", "i5":
		if err := validSignature(value); err != nil {
			return obfuscationError(key)
		}
		*map[string]*string{"i1": &o.I1, "i2": &o.I2, "i3": &o.I3, "i4": &o.I4, "i5": &o.I5}[key] = value
	case "headerprotectionkey":
		k, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(k) != 32 {
			return obfuscationError(key)
		}
		// Canonical form, so the runtime dump compares equal.
		o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(k)
	case "contentpaddingaddition", "rekeyaftertime", "rekeytimeout", "rejectaftertime", "keepalivetimeout", "maxhandshakeattempts":
		r, err := awgparam.ParseU16Range(value)
		if err != nil {
			return obfuscationError(key)
		}
		*map[string]*awgparam.U16Range{
			"contentpaddingaddition": &o.ContentPaddingAddition, "rekeyaftertime": &o.RekeyAfterTime,
			"rekeytimeout": &o.RekeyTimeout, "rejectaftertime": &o.RejectAfterTime,
			"keepalivetimeout": &o.KeepaliveTimeout, "maxhandshakeattempts": &o.MaxHandshakeAttempts,
		}[key] = r
	case "randomtrailers", "disablecookies":
		b, ok := parseFlag(value)
		if !ok {
			return obfuscationError(key)
		}
		if key == "randomtrailers" {
			o.RandomTrailers = b
		} else {
			o.DisableCookies = b
		}
	default:
		return invalidDetail("config", "config_unsupported_key", clip(key))
	}
	return nil
}

// address keeps the first IPv4 address; IPv6 is outside the egress scope. A
// config may list addresses on one line or repeat the key.
func (p *configParser) address(value string) error {
	for _, part := range strings.Split(value, ",") {
		if part == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			a, aerr := netip.ParseAddr(part)
			if aerr != nil {
				return invalid("config", "config_address")
			}
			prefix = netip.PrefixFrom(a, a.BitLen())
		}
		if prefix.Addr().Is4() && p.out.Tunnel.Address == "" {
			p.out.Tunnel.Address = netip.PrefixFrom(prefix.Addr(), 32).String()
		}
	}
	if p.out.Tunnel.Address == "" {
		return invalid("config", "config_address")
	}
	return nil
}

func (p *configParser) peerKey(key, value string) error {
	switch key {
	case "allowedips":
		// Everything steered into the outbound may leave through it.
		p.ignore("AllowedIPs")
		return nil
	case "advancedsecurity":
		// A tools-only flag: the pinned kernel module and userspace daemon
		// have no counterpart, so it changes nothing on the wire.
		p.ignore("AdvancedSecurity")
		return nil
	}
	if p.seen[key] {
		return invalidDetail("config", "config_duplicate_key", displayKey(key))
	}
	p.seen[key] = true
	t := &p.out.Tunnel
	switch key {
	case "publickey":
		if tunnel.ValidatePublicKey(value) != nil {
			return invalidDetail("config", "config_key", "PublicKey")
		}
		t.PeerPublicKey = value
	case "presharedkey":
		if tunnel.ValidatePrivateKey(value) != nil {
			return invalidDetail("config", "config_key", "PresharedKey")
		}
		p.out.Secret.PresharedKey = value
	case "endpoint":
		// The error belongs to the pasted config, not the override field.
		if validEndpoint(value) != nil {
			return invalidDetail("config", "config_value", "Endpoint")
		}
		t.Endpoint = value
	case "persistentkeepalive":
		low, high, ok := parseKeepalive(value)
		if !ok {
			return invalidDetail("config", "config_value", "PersistentKeepalive")
		}
		t.Keepalive, t.KeepaliveMax = low, high
	default:
		return invalidDetail("config", "config_unsupported_key", clip(key))
	}
	return nil
}

func (p *configParser) ignore(name string) {
	for _, n := range p.out.Ignored {
		if n == name {
			return
		}
	}
	p.out.Ignored = append(p.out.Ignored, name)
}

// parseKeepalive reads `off`, `N` or the AmneziaWG `N-M` interval. The high
// bound is 0 for a single value.
func parseKeepalive(value string) (int, int, bool) {
	if strings.EqualFold(value, "off") {
		return 0, 0, true
	}
	r, err := awgparam.ParseU16Range(value)
	if err != nil {
		return 0, 0, false
	}
	if r.Low() == r.High() {
		return int(r.Low()), 0, true
	}
	if r.Low() == 0 {
		return 0, 0, false
	}
	return int(r.Low()), int(r.High()), true
}

// parseFlag accepts the pinned tools' boolean spellings.
func parseFlag(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "on", "true", "yes":
		return true, true
	case "off", "false", "no":
		return false, true
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return false, false
	}
	return n != 0, true
}

func isSignatureKey(key string) bool {
	return len(key) == 2 && key[0] == 'i' && key[1] >= '1' && key[1] <= '5'
}

// signatureTags is the union of the tags the pinned runtimes build: the
// kernel module knows b, c, t, r, rc, rd; the userspace daemon b, t, r, rc,
// rd, d, ds, dz. A tag only one runtime knows still imports: the link falls
// back to the other runtime when the first rejects it.
var signatureTags = map[string]bool{"b": true, "c": true, "t": true, "r": true, "rc": true, "rd": true, "d": true, "ds": true, "dz": true}

// validSignature checks an I1–I5 value is a safe single line of well-formed
// <tag value> elements. The runtime parses the values themselves.
func validSignature(value string) error {
	if value == "" || len(value) > maxSignatureBytes {
		return invalid("config", "config_obfuscation")
	}
	if err := iface.ValidateConfigText(iface.Obfuscation{I1: value}); err != nil {
		return invalid("config", "config_obfuscation")
	}
	rest := value
	for rest != "" {
		start := strings.IndexByte(rest, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(rest[start:], '>')
		if end < 0 {
			return invalid("config", "config_obfuscation")
		}
		fields := strings.Fields(rest[start+1 : start+end])
		if len(fields) == 0 || len(fields) > 2 || !signatureTags[fields[0]] {
			return invalid("config", "config_obfuscation")
		}
		rest = rest[start+end+1:]
	}
	if !strings.Contains(value, "<") {
		return invalid("config", "config_obfuscation")
	}
	return nil
}

// importedObfuscation applies what the pinned runtimes themselves enforce,
// not the narrower policy WG-Guard uses for the profiles it generates: an
// imported config was written for someone else's server and must be used as
// it is. Junk-only configs get the stock H1–H4 a runtime starts with, and a
// set that changes nothing on the wire is plain WireGuard.
func importedObfuscation(o iface.Obfuscation) (iface.Obfuscation, error) {
	hs := []*awgparam.U32Range{&o.H1, &o.H2, &o.H3, &o.H4}
	for i, h := range hs {
		if h.IsZero() {
			*h = awgparam.ScalarU32(uint32(i + 1))
		}
	}
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			if hs[i].Overlaps(*hs[j]) {
				return iface.Obfuscation{}, invalidDetail("config", "config_headers_overlap", "H"+strconv.Itoa(i+1)+", H"+strconv.Itoa(j+1))
			}
		}
	}
	if o.Jc > iface.JcMax {
		return iface.Obfuscation{}, obfuscationError("jc")
	}
	if o.Jc > 0 && o.Jmin > o.Jmax {
		return iface.Obfuscation{}, invalidDetail("config", "config_junk_range", "Jmin, Jmax")
	}
	if o.HeaderProtectionKey != "" && (o.S1 < 12 || o.S2 < 12 || o.S3 < 12 || o.S4 < 12) {
		return iface.Obfuscation{}, invalidDetail("config", "config_header_protection", "S1–S4")
	}
	t := o.Tunnel()
	if t.PlainEquivalent() {
		return iface.Obfuscation{}, nil
	}
	o.Enabled = true
	return o, nil
}

func obfuscationError(key string) error {
	return invalidDetail("config", "config_obfuscation", displayKey(key))
}

// displayKeys spells the keys the way configs and upstream documentation do.
var displayKeys = map[string]string{
	"privatekey": "PrivateKey", "address": "Address", "mtu": "MTU", "jc": "Jc", "jmin": "Jmin", "jmax": "Jmax",
	"s1": "S1", "s2": "S2", "s3": "S3", "s4": "S4", "h1": "H1", "h2": "H2", "h3": "H3", "h4": "H4",
	"i1": "I1", "i2": "I2", "i3": "I3", "i4": "I4", "i5": "I5", "headerprotectionkey": "HeaderProtectionKey",
	"contentpaddingaddition": "ContentPaddingAddition", "rekeyaftertime": "RekeyAfterTime", "rekeytimeout": "RekeyTimeout",
	"rejectaftertime": "RejectAfterTime", "keepalivetimeout": "KeepaliveTimeout", "maxhandshakeattempts": "MaxHandshakeAttempts",
	"randomtrailers": "RandomTrailers", "disablecookies": "DisableCookies", "publickey": "PublicKey",
	"presharedkey": "PresharedKey", "endpoint": "Endpoint", "persistentkeepalive": "PersistentKeepalive",
}

func displayKey(key string) string {
	if name, ok := displayKeys[key]; ok {
		return name
	}
	return clip(key)
}

// clip bounds a key or section name echoed in an error (never a value).
func clip(s string) string {
	if r := []rune(s); len(r) > 32 {
		return string(r[:32]) + "…"
	}
	return s
}

// validEndpoint requires host:port (a WireGuard endpoint always has a port).
func validEndpoint(value string) error {
	if value == "" || settings.ValidEndpoint(value) != nil {
		return invalid("endpoint", "endpoint_invalid")
	}
	if strings.HasPrefix(value, "[") {
		if !strings.Contains(value, "]:") {
			return invalid("endpoint", "endpoint_invalid")
		}
		return nil
	}
	if strings.Count(value, ":") != 1 {
		return invalid("endpoint", "endpoint_invalid")
	}
	return nil
}
