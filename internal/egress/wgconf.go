package egress

import (
	"net/netip"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// MaxConfigBytes bounds an imported client configuration.
const MaxConfigBytes = 16 << 10

// DefaultTunnelMTU is used when an imported config carries no MTU. WARP and
// most commercial WireGuard exits work at 1280; larger values are kept when
// the config says so.
const DefaultTunnelMTU = 1280

// ParsedConfig is an imported WireGuard/AmneziaWG client configuration split
// into its public and secret halves.
type ParsedConfig struct {
	Tunnel TunnelConfig
	Secret Secret
}

// ParseClientConfig parses a standard wg-quick style client config (the kind
// a VPN provider, another WG-Guard node, wgcf or AmneziaVPN exports). Exactly
// one [Peer] is supported. DNS, AllowedIPs, Table and script hooks are not
// used: egress routing decides what enters the tunnel. Errors never echo key
// material.
func ParseClientConfig(text string) (ParsedConfig, error) {
	var out ParsedConfig
	if len(text) > MaxConfigBytes {
		return out, invalid("config", "config_too_large")
	}
	section := ""
	peers := 0
	obf := iface.Obfuscation{}
	obfSeen := false
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			switch section {
			case "interface":
			case "peer":
				peers++
				if peers > 1 {
					return out, invalid("config", "config_multiple_peers")
				}
			default:
				return out, invalid("config", "config_invalid")
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return out, invalid("config", "config_invalid")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch section {
		case "interface":
			if err := parseInterfaceKey(&out, &obf, &obfSeen, key, value); err != nil {
				return out, err
			}
		case "peer":
			if err := parsePeerKey(&out, key, value); err != nil {
				return out, err
			}
		default:
			return out, invalid("config", "config_invalid")
		}
	}
	if out.Secret.PrivateKey == "" || out.Tunnel.PeerPublicKey == "" || out.Tunnel.Endpoint == "" || out.Tunnel.Address == "" {
		return out, invalid("config", "config_incomplete")
	}
	if out.Tunnel.MTU == 0 {
		out.Tunnel.MTU = DefaultTunnelMTU
	}
	if out.Tunnel.Keepalive == 0 {
		out.Tunnel.Keepalive = 25
	}
	if obfSeen {
		obf.Enabled = true
		if err := iface.ValidateObfuscation(obf); err != nil {
			return out, invalid("config", "config_obfuscation")
		}
	}
	out.Tunnel.Obfuscation = obf
	return out, nil
}

func parseInterfaceKey(out *ParsedConfig, obf *iface.Obfuscation, seen *bool, key, value string) error {
	switch key {
	case "privatekey":
		if tunnel.ValidatePrivateKey(value) != nil {
			return invalid("config", "config_key")
		}
		out.Secret.PrivateKey = value
	case "address":
		// Keep the first IPv4 address; IPv6 is outside the tunnel scope.
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			p, err := netip.ParsePrefix(part)
			if err != nil {
				a, aerr := netip.ParseAddr(part)
				if aerr != nil {
					return invalid("config", "config_address")
				}
				p = netip.PrefixFrom(a, a.BitLen())
			}
			if p.Addr().Is4() && out.Tunnel.Address == "" {
				out.Tunnel.Address = netip.PrefixFrom(p.Addr(), 32).String()
			}
		}
		if out.Tunnel.Address == "" {
			return invalid("config", "config_address")
		}
	case "mtu":
		n, err := strconv.Atoi(value)
		if err != nil || n < 576 || n > 1500 {
			return invalid("config", "config_mtu")
		}
		out.Tunnel.MTU = n
	case "dns", "listenport", "table", "preup", "postup", "predown", "postdown", "saveconfig", "fwmark":
		// Ignored by design: routing and DNS belong to WG-Guard.
	case "jc", "jmin", "jmax", "s1", "s2", "s3", "s4":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 65535 {
			return invalid("config", "config_obfuscation")
		}
		*seen = true
		switch key {
		case "jc":
			obf.Jc = n
		case "jmin":
			obf.Jmin = n
		case "jmax":
			obf.Jmax = n
		case "s1":
			obf.S1 = n
		case "s2":
			obf.S2 = n
		case "s3":
			obf.S3 = n
		case "s4":
			obf.S4 = n
		}
	case "h1", "h2", "h3", "h4":
		r, err := awgparam.ParseU32Range(value)
		if err != nil {
			return invalid("config", "config_obfuscation")
		}
		*seen = true
		switch key {
		case "h1":
			obf.H1 = r
		case "h2":
			obf.H2 = r
		case "h3":
			obf.H3 = r
		case "h4":
			obf.H4 = r
		}
	case "i1", "i2", "i3", "i4", "i5":
		if len(value) > 8192 {
			return invalid("config", "config_obfuscation")
		}
		*seen = true
		switch key {
		case "i1":
			obf.I1 = value
		case "i2":
			obf.I2 = value
		case "i3":
			obf.I3 = value
		case "i4":
			obf.I4 = value
		case "i5":
			obf.I5 = value
		}
	default:
		return invalidDetail("config", "config_unsupported_key", key)
	}
	return nil
}

func parsePeerKey(out *ParsedConfig, key, value string) error {
	switch key {
	case "publickey":
		if tunnel.ValidatePublicKey(value) != nil {
			return invalid("config", "config_key")
		}
		out.Tunnel.PeerPublicKey = value
	case "presharedkey":
		if tunnel.ValidatePrivateKey(value) != nil {
			return invalid("config", "config_key")
		}
		out.Secret.PresharedKey = value
	case "endpoint":
		if err := validEndpoint(value); err != nil {
			return err
		}
		out.Tunnel.Endpoint = value
	case "persistentkeepalive":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 65535 {
			return invalid("config", "config_invalid")
		}
		out.Tunnel.Keepalive = n
	case "allowedips":
		// Ignored: everything steered into the outbound may leave through it.
	default:
		return invalidDetail("config", "config_unsupported_key", key)
	}
	return nil
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
