package routing

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// Destinations that never leave through an outbound: private, loopback,
// link-local, CGNAT and multicast/reserved space. Client-to-client and
// client-to-node traffic therefore keeps the main table.
var reservedPrefixes = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/3",
}

// Well-known public resolvers that accept DNS-over-HTTPS on 443 at their
// literal address. Blocking them (and DoT/DoQ on 853) makes clients fall back
// to plain DNS while domain rules are active; hostname-based DoH templates are
// refused by the resolver itself (dnsrouter).
var encryptedDNSAddresses = []string{
	"1.1.1.1", "1.0.0.1", "1.1.1.2", "1.0.0.2", "1.1.1.3", "1.0.0.3",
	"8.8.8.8", "8.8.4.4", "9.9.9.9", "149.112.112.112", "9.9.9.11", "149.112.112.11",
	"208.67.222.222", "208.67.220.220", "94.140.14.14", "94.140.15.15",
	"185.228.168.9", "185.228.169.9", "76.76.2.0", "76.76.10.0",
}

const (
	setReserved     = "reserved"
	setEncryptedDNS = "encrypted_dns"
	chainPrerouting = "prerouting"
	chainClassify   = "classify"
	chainBlockFwd   = "block_forward"
	chainMSS        = "mss_clamp"
	chainDNS        = "dns_redirect"
	chainSourceNAT  = "source_nat"
	dynamicSetSize  = 65536
	elementsPerLine = 512
)

var baseChains = map[string]string{
	chainPrerouting: "type filter hook prerouting priority mangle; policy accept;",
	chainBlockFwd:   "type filter hook forward priority -10; policy accept;",
	chainMSS:        "type filter hook forward priority mangle; policy accept;",
	chainDNS:        "type nat hook prerouting priority dstnat; policy accept;",
	chainSourceNAT:  "type nat hook postrouting priority 99; policy accept;",
}

// Inventory is the object list of the live table (`nft -t -j list table`).
type Inventory struct {
	Present bool
	Chains  map[string]bool
	Sets    map[string]bool
}

// ParseInventory reads terse JSON listing output.
func ParseInventory(data []byte) (Inventory, error) {
	inv := Inventory{Present: true, Chains: map[string]bool{}, Sets: map[string]bool{}}
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return inv, fmt.Errorf("routing: unreadable nft inventory")
	}
	for _, obj := range doc.Nftables {
		for kind, raw := range obj {
			var named struct {
				Name  string `json:"name"`
				Table string `json:"table"`
			}
			if kind != "chain" && kind != "set" {
				continue
			}
			if err := json.Unmarshal(raw, &named); err != nil || named.Table != TableName {
				continue
			}
			if kind == "chain" {
				inv.Chains[named.Name] = true
			} else {
				inv.Sets[named.Name] = true
			}
		}
	}
	return inv, nil
}

func quotedNames(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = fmt.Sprintf("%q", n)
	}
	return "{ " + strings.Join(q, ", ") + " }"
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RenderTable renders one nft transaction that converges the live table
// (described by inv) to the plan. resolver says whether the resolver is
// listening, which gates DNS redirection.
func RenderTable(p *Plan, inv Inventory, resolver bool) []byte {
	t := "ip " + TableName
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	classifying := len(p.Ifaces) > 0 && (len(p.Classify) > 0 || p.DefaultChain != chainDirect || p.BlockEncryptedDNS)
	redirect := resolver && len(p.Ifaces) > 0 && p.Resolves()

	chains := map[string]string{} // name → base spec ("" for regular)
	sets := map[string]string{}   // name → definition
	if classifying {
		chains[chainPrerouting] = baseChains[chainPrerouting]
		chains[chainClassify] = ""
		chains[chainBlockFwd] = baseChains[chainBlockFwd]
		for name := range p.Chains {
			chains[name] = ""
		}
		sets[setReserved] = "type ipv4_addr; flags interval; auto-merge;"
		if p.BlockEncryptedDNS {
			sets[setEncryptedDNS] = "type ipv4_addr; flags interval; auto-merge;"
		}
		for name := range p.StaticSets {
			sets[name] = "type ipv4_addr; flags interval; auto-merge;"
		}
		for _, name := range p.DynamicSets {
			sets[name] = fmt.Sprintf("type ipv4_addr; flags timeout; size %d;", dynamicSetSize)
		}
	}
	if len(p.MSS) > 0 {
		chains[chainMSS] = baseChains[chainMSS]
	}
	if redirect {
		chains[chainDNS] = baseChains[chainDNS]
	}
	if len(p.SNAT) > 0 {
		chains[chainSourceNAT] = baseChains[chainSourceNAT]
	}

	line("add table %s", t)
	// Every chain exists before any rule references it; rules come last.
	for _, name := range sortedKeys(chains) {
		if spec := chains[name]; spec != "" {
			line("add chain %s %s { %s }", t, name, spec)
		} else {
			line("add chain %s %s", t, name)
		}
	}
	flushed := map[string]bool{}
	for _, name := range append(sortedKeys(inv.Chains), sortedKeys(chains)...) {
		if !flushed[name] {
			flushed[name] = true
			line("flush chain %s %s", t, name)
		}
	}
	for _, name := range sortedKeys(sets) {
		line("add set %s %s { %s }", t, name, sets[name])
	}
	elements := func(set string, prefixes []string) {
		line("flush set %s %s", t, set)
		for i := 0; i < len(prefixes); i += elementsPerLine {
			end := min(i+elementsPerLine, len(prefixes))
			line("add element %s %s { %s }", t, set, strings.Join(prefixes[i:end], ", "))
		}
	}
	if classifying {
		elements(setReserved, reservedPrefixes)
		if p.BlockEncryptedDNS {
			elements(setEncryptedDNS, encryptedDNSAddresses)
		}
		for _, name := range sortedKeys(p.StaticSets) {
			elements(name, prefixStrings(p.StaticSets[name]))
		}
	}
	for _, name := range sortedKeys(inv.Chains) {
		if _, keep := chains[name]; !keep {
			line("delete chain %s %s", t, name)
		}
	}
	for _, name := range sortedKeys(inv.Sets) {
		if _, keep := sets[name]; !keep {
			line("delete set %s %s", t, name)
		}
	}

	ifaces := quotedNames(p.Ifaces)
	if classifying {
		mask := fmt.Sprintf("0x%08x", MarkMask)
		line("add rule %s %s iifname != %s return", t, chainPrerouting, ifaces)
		line("add rule %s %s ct mark and %s != 0 meta mark set ct mark and %s return", t, chainPrerouting, mask, mask)
		// Flows that predate classification keep the direct route.
		line("add rule %s %s ct state established,related goto %s", t, chainPrerouting, chainDirect)
		line("add rule %s %s ip daddr @%s return", t, chainPrerouting, setReserved)
		line("add rule %s %s fib daddr type local return", t, chainPrerouting)
		if redirect {
			line("add rule %s %s meta l4proto { tcp, udp } th dport 53 return", t, chainPrerouting)
		}
		line("add rule %s %s jump %s", t, chainPrerouting, chainClassify)

		if p.BlockEncryptedDNS {
			line("add rule %s %s meta l4proto { tcp, udp } th dport 853 goto %s", t, chainClassify, chainBlock)
			line("add rule %s %s ip daddr @%s meta l4proto { tcp, udp } th dport 443 goto %s", t, chainClassify, setEncryptedDNS, chainBlock)
		}
		for _, r := range p.Classify {
			line("add rule %s %s ip daddr @%s goto %s", t, chainClassify, r.Set, r.Chain)
		}
		line("add rule %s %s goto %s", t, chainClassify, p.DefaultChain)
		for _, name := range sortedKeys(p.Chains) {
			line("add rule %s %s %s", t, name, p.Chains[name])
		}
		block := fmt.Sprintf("0x%08x", MarkBlock)
		line("add rule %s %s meta mark and %s == %s meta l4proto tcp reject with tcp reset", t, chainBlockFwd, mask, block)
		line("add rule %s %s meta mark and %s == %s reject with icmp type admin-prohibited", t, chainBlockFwd, mask, block)
	}
	for _, link := range sortedKeys(p.MSS) {
		line("add rule %s %s oifname %q tcp flags syn / syn,rst tcp option maxseg size set rt mtu", t, chainMSS, link)
		line("add rule %s %s iifname %q tcp flags syn / syn,rst tcp option maxseg size set %d", t, chainMSS, link, p.MSS[link])
	}
	if redirect {
		line("add rule %s %s iifname %s meta l4proto { tcp, udp } th dport 53 redirect to :53", t, chainDNS, ifaces)
	}
	for _, s := range p.SNAT {
		line("add rule %s %s meta mark and 0x%08x == 0x%08x snat to %s", t, chainSourceNAT, MarkMask, s.Mark, s.Address)
	}
	return []byte(b.String())
}

func prefixStrings(prefixes []netip.Prefix) []string {
	out := make([]string, 0, len(prefixes))
	seen := map[netip.Prefix]bool{}
	for _, p := range prefixes {
		if !seen[p] {
			seen[p] = true
			out = append(out, p.String())
		}
	}
	sort.Strings(out)
	return out
}
