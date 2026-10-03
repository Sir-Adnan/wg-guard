// Package ipam defines the shared, bounded IPv4-pool contract. It contains no
// tunnel mutations or account policy; allocation, capacity and runtime consumers
// use the same ordered pools and reserved-address convention.
package ipam

import (
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

const MaxPools = 16

// Parse validates canonical RFC1918 IPv4 networks with at least five peer slots.
func Parse(cidr string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() || p != p.Masked() || p.Bits() > 29 || !p.Addr().IsPrivate() || !Last(p).IsPrivate() {
		return netip.Prefix{}, domain.E(domain.CodeSubnetInvalid, "pool must be a canonical RFC1918 IPv4 CIDR, /29 or larger")
	}
	return p, nil
}

func Last(p netip.Prefix) netip.Addr {
	r := p.Masked().Addr().As4()
	n := uint32(r[0])<<24 | uint32(r[1])<<16 | uint32(r[2])<<8 | uint32(r[3])
	n |= uint32((uint64(1) << (32 - p.Bits())) - 1)
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
}

func Capacity(p netip.Prefix) uint64 { return uint64(1)<<(32-p.Bits()) - 3 }

func Gateway(cidr string) string {
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() {
		return ""
	}
	return netip.PrefixFrom(p.Masked().Addr().Next(), p.Bits()).String()
}

// Decode combines the stable primary network and the stored ordered extras.
// Stored invalid/overlapping pools fail closed rather than disappearing.
func Decode(primary, extras string) ([]string, error) {
	var additional []string
	if err := json.Unmarshal([]byte(extras), &additional); err != nil {
		return nil, fmt.Errorf("ipam: invalid stored pool list")
	}
	all := append([]string{primary}, additional...)
	if err := Validate(all); err != nil {
		return nil, err
	}
	return all, nil
}

func Validate(pools []string) error {
	if len(pools) < 1 || len(pools) > MaxPools {
		return domain.E(domain.CodeSubnetInvalid, "an interface needs 1-%d pools", MaxPools)
	}
	prefixes := make([]netip.Prefix, 0, len(pools))
	for _, cidr := range pools {
		p, err := Parse(cidr)
		if err != nil {
			return err
		}
		for _, other := range prefixes {
			if p.Overlaps(other) {
				return domain.E(domain.CodeSubnetOverlap, "pool %s overlaps pool %s", p, other)
			}
		}
		prefixes = append(prefixes, p)
	}
	return nil
}

func Extras(pools []string) string {
	raw, _ := json.Marshal(pools[1:])
	return string(raw)
}
