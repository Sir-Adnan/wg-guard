package settings

import (
	"fmt"
	"net"
	"net/netip"
)

// Defaults is the Phase 1 catalog. Every value here is a *recommended
// default* chosen from upstream constraints (docs/operations/deployment.md) —
// administrators may change any of it; final guidance follows the Phase 8 VPS
// matrix. Phases add keys; keys are never renamed (Settings/API contract).
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
		{Key: "interfaces.max_count", Kind: KindInt, Default: 8, Min: 1, Max: 64,
			Category: "interfaces", Persistent: true},

		// Accounting & drift (scheduler + reconcile read these live).
		{Key: "accounting.interval_seconds", Kind: KindInt, Default: 30, Min: 15, Max: 600,
			Category: "accounting"},
		{Key: "accounting.online_window_seconds", Kind: KindInt, Default: 180, Min: 30, Max: 3600,
			Category: "accounting"},
		{Key: "drift.policy", Kind: KindString, Default: "report", Options: []string{"report", "adopt", "remove"},
			Category: "drift", Persistent: true},

		// Security (session lifetimes; admin sessions read these).
		{Key: "security.session_idle_hours", Kind: KindInt, Default: 12, Min: 1, Max: 168,
			Category: "security"},
		{Key: "security.session_absolute_hours", Kind: KindInt, Default: 168, Min: 1, Max: 720,
			Category: "security"},

		// Users (defaults applied at creation; per-user overrides exist).
		{Key: "users.default_device_limit", Kind: KindInt, Default: 3, Min: 1, Max: 100,
			Category: "general"},

		// Backup (encryption is optional; an empty password means plain
		// archives — ADR-0008).
		{Key: "backup.password", Kind: KindSecret, Default: "", Category: "backup", Secret: true},
		{Key: "backup.telegram_token", Kind: KindSecret, Default: "", Category: "backup", Secret: true},
		{Key: "backup.telegram_chat", Kind: KindString, Default: "", Category: "backup",
			Validator: func(v any) error {
				s := v.(string)
				if s == "" {
					return nil
				}
				for _, r := range s {
					if r < '0' || r > '9' {
						return fmt.Errorf("chat id must be numeric")
					}
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
