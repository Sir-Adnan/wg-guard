package install

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

// seedTimeout bounds one CLI seeding invocation. The first call also creates
// the database (migrations) and the master key, so it is generous.
const seedTimeout = 2 * time.Minute

// builtInPool is the subnet the interface service derives for awg0 when no
// explicit pool is set (10.8.N.0/24 ladder) — choosing it means "no custom
// pool", so it is never seeded.
const builtInPool = "10.8.0.0/24"

// seed is one registry/schedule write applied before the service first boots.
type seed struct {
	label string
	argv  []string
	stdin string // non-empty → RunWithInput (secret transport)
}

// planSeeds turns the wizard's choices into CLI invocations. Values equal to
// the registry defaults are skipped so a default install writes nothing
// beyond the endpoint. The bot token travels via stdin (`settings set …
// -stdin`), never via argv (docs/operations/security.md).
func planSeeds(p Plan) []seed {
	defaults := registryDefaults()
	skip := func(key, val string) bool {
		def, ok := defaults[key]
		return ok && normalizeValue(val) == normalizeValue(def)
	}

	var seeds []seed
	add := func(label string, argv ...string) {
		seeds = append(seeds, seed{label: label, argv: argv})
	}
	// Client configs embed the endpoint: seeding it from the panel domain
	// makes the first exported config work without a Settings visit.
	if endpoint := p.VPNEndpoint(); endpoint != "" {
		add("public endpoint", BinPath, "settings", "set", "node.endpoint", endpoint)
	}
	if p.PortMin != 0 && !skip("network.port_min", strconv.Itoa(p.PortMin)) {
		add("AWG port range start", BinPath, "settings", "set", "network.port_min", strconv.Itoa(p.PortMin))
	}
	if p.PortMax != 0 && !skip("network.port_max", strconv.Itoa(p.PortMax)) {
		add("AWG port range end", BinPath, "settings", "set", "network.port_max", strconv.Itoa(p.PortMax))
	}
	if p.VPNSubnet != "" && p.VPNSubnet != builtInPool && !skip("network.default_pool", p.VPNSubnet) {
		add("VPN subnet pool", BinPath, "settings", "set", "network.default_pool", p.VPNSubnet)
	}
	if p.MTU != 0 && !skip("network.mtu", strconv.Itoa(p.MTU)) {
		add("client MTU", BinPath, "settings", "set", "network.mtu", strconv.Itoa(p.MTU))
	}
	if p.ClientDNS != "" && !skip("network.dns_servers", p.ClientDNS) {
		add("client DNS servers", BinPath, "settings", "set", "network.dns_servers", p.ClientDNS)
	}
	if p.TelegramToken != "" {
		seeds = append(seeds, seed{
			label: "Telegram bot token",
			argv:  []string{BinPath, "settings", "set", "backup.telegram_token", "-stdin"},
			stdin: p.TelegramToken,
		})
	}
	if p.TelegramChat != "" {
		add("Telegram chat", BinPath, "settings", "set", "backup.telegram_chat", p.TelegramChat)
	}
	if p.TelegramToken != "" && p.TelegramChat != "" && p.TelegramTime != "" {
		add("backup schedule", BinPath, "backup", "schedule-add",
			"-name", "installer-daily", "-kind", "daily", "-time", p.TelegramTime)
	}
	return seeds
}

// seedSettings applies the wizard's runtime choices through the installed
// CLI, BEFORE the service first boots: the settings registry caches values
// in memory, so post-boot writes would stay invisible until a restart. In
// docker mode this runs while the state file does not exist yet, so the
// shim is guaranteed to execute host-direct (mode routing needs
// install-state.json) and the bind-mounted data dir gives host and
// container the same database.
func seedSettings(ctx context.Context, h Host, p Plan, out io.Writer) error {
	seeds := planSeeds(p)
	if len(seeds) == 0 {
		return nil
	}
	step(out, "Initial settings")
	for _, s := range seeds {
		progress(out, s.label)
		var err error
		if s.stdin != "" {
			err = h.RunWithInput(ctx, s.argv, strings.NewReader(s.stdin+"\n"), seedTimeout)
		} else {
			err = h.Run(ctx, s.argv, seedTimeout)
		}
		if err != nil {
			return fmt.Errorf("install: apply %s: %w", s.label, err)
		}
	}
	return nil
}

// registryDefaults flattens the registry defaults for skip-if-default
// comparisons (settings.Defaults is pure — no database involved).
func registryDefaults() map[string]string {
	m := make(map[string]string, 32)
	for _, d := range settings.Defaults() {
		switch v := d.Default.(type) {
		case int:
			m[d.Key] = strconv.Itoa(v)
		case string:
			m[d.Key] = v
		case []string:
			m[d.Key] = strings.Join(v, ",")
		}
	}
	return m
}

// normalizeValue trims whitespace so "1.1.1.1, 1.0.0.1" compares equal to
// the registry default "1.1.1.1,1.0.0.1".
func normalizeValue(s string) string {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, ",")
}
