package settings

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
)

// validHostname is the syntactic hostname check used by node.endpoint
// validation (DNS resolution itself is deliberately NOT required here — an
// endpoint may be registered before its DNS record exists; it is a warning,
// not an error, surface at doctor time).
func validHostname(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i, r := range label {
			ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-'
			if !ok || (r == '-' && (i == 0 || i == len(label)-1)) {
				return false
			}
		}
	}
	return true
}

// ValidEndpoint validates the host or host:port form written to client
// configuration Endpoint lines. URLs, paths, control characters, whitespace,
// invalid bracket notation, and ports outside 1..65535 are rejected. Bare
// IPv6 addresses are accepted and bracketed by the client-config renderer.
func ValidEndpoint(s string) error {
	if s == "" {
		return nil
	}
	if strings.TrimSpace(s) != s || strings.ContainsAny(s, "\x00\r\n\t") {
		return fmt.Errorf("%q is not a canonical endpoint", s)
	}

	host := s
	portText := ""
	bracketed := strings.HasPrefix(s, "[")
	if bracketed && strings.HasSuffix(s, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Is6() {
			return fmt.Errorf("%q is not a bracketed IPv6 address", s)
		}
		return nil
	}
	if bracketed || strings.Count(s, ":") == 1 {
		var err error
		host, portText, err = net.SplitHostPort(s)
		if err != nil || host == "" || portText == "" {
			return fmt.Errorf("%q is not a valid host:port endpoint", s)
		}
		if bracketed {
			addr, err := netip.ParseAddr(host)
			if err != nil || !addr.Is6() {
				return fmt.Errorf("%q uses brackets for a non-IPv6 host", s)
			}
		}
	} else if strings.Contains(s, ":") {
		if _, err := netip.ParseAddr(s); err != nil {
			return fmt.Errorf("%q is not a valid IPv6 address", s)
		}
		return nil
	}

	if addr, err := netip.ParseAddr(host); err != nil {
		if !validHostname(host) {
			return fmt.Errorf("%q is not an IP address or hostname", s)
		}
	} else if bracketed && !addr.Is6() {
		return fmt.Errorf("%q uses brackets for a non-IPv6 host", s)
	}
	if portText == "" {
		return nil
	}
	for _, r := range portText {
		if r < '0' || r > '9' {
			return fmt.Errorf("%q has an invalid port", s)
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%q has a port outside 1-65535", s)
	}
	return nil
}

// Defaults is the Phase 1 catalog. Every value here is a *recommended
// default* chosen from upstream constraints (docs/operations/deployment.md) —
// administrators may change any of it; final guidance follows the Phase 11
// production matrix. Phases add keys; keys are never renamed (Settings/API contract).
func Defaults() []Definition {
	return []Definition{
		// Networking (recommended configurable defaults).
		{Key: "network.mtu", Kind: KindInt, Default: 1420, Min: 576, Max: 65535,
			Category: "networking", Persistent: true},
		{Key: "network.port_min", Kind: KindInt, Default: 30000, Min: 1024, Max: 65535,
			Category: "networking", Persistent: true},
		{Key: "network.port_max", Kind: KindInt, Default: 50000, Min: 1024, Max: 65535,
			Category: "networking", Persistent: true},
		{Key: "network.dns_servers", Kind: KindStringList, Default: []string{"1.1.1.1", "1.0.0.1"},
			Category: "networking", Persistent: true,
			Validator: func(v any) error {
				for _, s := range v.([]string) {
					if net.ParseIP(s) == nil {
						return fmt.Errorf("%q is not an IP address", s)
					}
				}
				return nil
			}},
		// Pool offered to the first interface (awg0) when its subnet is left
		// blank; later interfaces continue the 10.8.N.0/24 ladder. The
		// installer seeds this so a conflicting 10.8.0.0/24 can be avoided at
		// install time.
		{Key: "network.default_pool", Kind: KindString, Default: "", Category: "networking", Persistent: true,
			Validator: func(v any) error {
				if s := v.(string); s != "" {
					return ValidSubnet(s)
				}
				return nil
			}},
		{Key: "interfaces.max_count", Kind: KindInt, Default: 8, Min: 1, Max: 64,
			Category: "interfaces", Persistent: true},

		// Accounting & drift (scheduler + reconcile read these live).
		{Key: "accounting.interval_seconds", Kind: KindInt, Default: 30, Min: 15, Max: 600,
			Category: "accounting"},
		{Key: "accounting.online_window_seconds", Kind: KindInt, Default: 180, Min: 30, Max: 3600,
			Category: "accounting"},
		// Traffic samples are flushed from an in-memory accumulator on this
		// cadence (not every cycle) to bound SQLite churn; accumulated
		// totals never depend on it (persisted every cycle).
		{Key: "accounting.sample_flush_seconds", Kind: KindInt, Default: 300, Min: 60, Max: 3600,
			Category: "accounting"},
		{Key: "accounting.sample_retention_hours", Kind: KindInt, Default: 48, Min: 24, Max: 48,
			Category: "accounting"},
		{Key: "accounting.rollup_hourly_days", Kind: KindInt, Default: 30, Min: 7, Max: 90,
			Category: "accounting"},
		{Key: "accounting.rollup_daily_days", Kind: KindInt, Default: 365, Min: 30, Max: 730,
			Category: "accounting"},
		{Key: "drift.policy", Kind: KindString, Default: "report", Options: []string{"report", "adopt", "remove"},
			Category: "drift", Persistent: true},

		// Security (session lifetimes; admin sessions read these).
		{Key: "security.session_idle_hours", Kind: KindInt, Default: 12, Min: 1, Max: 168,
			Category: "security"},
		{Key: "security.session_absolute_hours", Kind: KindInt, Default: 168, Min: 1, Max: 720,
			Category: "security"},

		// Subscription links: public base for /sub/{token} URLs (a dedicated
		// short-domain host when set; empty = this panel's own origin). The
		// settings screen binds to this key in Phase 6.
		{Key: "subscription.base_url", Kind: KindString, Default: "", Category: "general",
			Validator: func(v any) error {
				s := v.(string)
				if s == "" {
					return nil
				}
				u, err := url.Parse(s)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
					u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
					return fmt.Errorf("%q is not an absolute http(s) base URL", s)
				}
				return nil
			}},

		// Users (defaults applied at creation; per-user overrides exist).
		// The preset lists feed the create-user quick chips; the settings
		// screen manages their values. default_quota_gb = 0 means "no traffic
		// preset preselected"; default_duration_months = 0 means the create
		// form defaults to no-expiry.
		{Key: "users.default_device_limit", Kind: KindInt, Default: 3, Min: 1, Max: 100,
			Category: "general"},
		{Key: "users.default_iface_id", Kind: KindString, Default: "", Category: "general",
			Validator: func(v any) error {
				if s := v.(string); len(s) > 64 {
					return fmt.Errorf("interface id must be at most 64 characters")
				}
				return nil
			}},
		{Key: "users.default_quota_gb", Kind: KindInt, Default: 100, Min: 0, Max: 1000000,
			Category: "general"},
		{Key: "users.default_duration_months", Kind: KindInt, Default: 1, Min: 0, Max: 120,
			Category: "general"},

		// Config download filenames: [prefix]username-device[suffix].conf
		// (sanitized to [A-Za-z0-9._-]; empty disables the part).
		{Key: "downloads.filename_prefix", Kind: KindString, Default: "", Category: "general",
			Validator: func(v any) error {
				s := v.(string)
				if len(s) > 24 {
					return fmt.Errorf("filename prefix must be at most 24 characters")
				}
				return nil
			}},
		{Key: "downloads.filename_suffix", Kind: KindString, Default: "", Category: "general",
			Validator: func(v any) error {
				s := v.(string)
				if len(s) > 24 {
					return fmt.Errorf("filename suffix must be at most 24 characters")
				}
				return nil
			}},
		{Key: "users.quota_presets_gb", Kind: KindStringList, Default: []string{
			"20", "50", "70", "100", "150", "200", "300", "500", "700", "1000"},
			Category: "general",
			Validator: func(v any) error {
				for _, s := range v.([]string) {
					n, err := strconv.Atoi(s)
					if err != nil || n <= 0 || n > 1000000 {
						return fmt.Errorf("quota presets must be positive GB integers, got %q", s)
					}
				}
				return nil
			}},
		{Key: "users.duration_presets_months", Kind: KindStringList, Default: []string{"1", "3", "6", "12"},
			Category: "general",
			Validator: func(v any) error {
				for _, s := range v.([]string) {
					n, err := strconv.Atoi(s)
					if err != nil || n <= 0 || n > 120 {
						return fmt.Errorf("duration presets must be positive month integers, got %q", s)
					}
				}
				return nil
			}},

		// Node identity (client-config Endpoint and webhook envelopes).
		// node.id is filled with the hostname on first serve when empty.
		{Key: "node.id", Kind: KindString, Default: "", Category: "general",
			Validator: func(v any) error {
				if s := v.(string); len(s) > 128 {
					return fmt.Errorf("node id must be at most 128 characters")
				}
				return nil
			}},
		{Key: "node.endpoint", Kind: KindString, Default: "", Category: "general",
			Validator: func(v any) error {
				return ValidEndpoint(v.(string))
			}},

		// Client-config rendering (GET /api/v1/devices/{id}/config).
		{Key: "network.client_allowed_ips", Kind: KindString, Default: "0.0.0.0/0", Category: "networking",
			Validator: func(v any) error {
				for _, s := range strings.Split(v.(string), ",") {
					if s = strings.TrimSpace(s); s != "" {
						if _, _, err := net.ParseCIDR(s); err != nil {
							return fmt.Errorf("%q is not a valid CIDR", s)
						}
					}
				}
				return nil
			}},
		{Key: "network.client_persistent_keepalive", Kind: KindString, Default: "25",
			Category: "networking",
			Validator: func(v any) error {
				if _, err := awgparam.ParseU16Range(v.(string)); err != nil {
					return fmt.Errorf("persistent keepalive must be 0, N, or N-M within 0-65535")
				}
				return nil
			}},

		// Webhook delivery (internal/webhook): exponential backoff caps here;
		// a dead delivery is visible in the API and manually redeliverable.
		{Key: "webhooks.max_attempts", Kind: KindInt, Default: 12, Min: 1, Max: 50,
			Category: "webhooks"},

		// REST API (internal/api): per-token fixed-window request limit;
		// 0 disables limiting (trusted internal networks only).
		{Key: "api.rate_limit_per_minute", Kind: KindInt, Default: 600, Min: 0, Max: 100000,
			Category: "security"},

		// Backup (encryption is optional; an empty password means plain
		// archives — ADR-0008).
		{Key: "backup.password", Kind: KindSecret, Default: "", Category: "backup", Secret: true,
			Validator: func(v any) error {
				if s, _ := v.(string); s != "" && len(s) < 8 {
					return fmt.Errorf("backup password must be at least 8 characters")
				}
				return nil
			}},
		{Key: "backup.telegram_token", Kind: KindSecret, Default: "", Category: "backup", Secret: true},
		{Key: "backup.telegram_chat", Kind: KindString, Default: "", Category: "backup",
			Validator: func(v any) error {
				s := v.(string)
				if s == "" {
					return nil
				}
				id, err := strconv.ParseInt(s, 10, 64)
				if err != nil || id == 0 || strings.HasPrefix(s, "+") {
					return fmt.Errorf("chat id must be a nonzero signed integer")
				}
				return nil
			}},
		{Key: "backup.retention_count", Kind: KindInt, Default: 14, Min: 1, Max: 365,
			Category: "backup"},
	}
}

// ValidSubnet is shared validation used by the interface service and tests
// for pool-shaped strings.
func ValidSubnet(s string) error {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return fmt.Errorf("%q is not a valid CIDR", s)
	}
	if p != p.Masked() {
		return fmt.Errorf("%q has host bits set", s)
	}
	if !p.Addr().Is4() {
		return fmt.Errorf("only IPv4 pools are supported")
	}
	return nil
}
