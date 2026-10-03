package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
)

// SyncAddresses owns only IPv4 addresses on an already owned tunnel. Add desired
// routes before removing stale ones. IPv6 and every foreign interface stay intact.
func (l *Links) SyncAddresses(ctx context.Context, name string, desired []string) error {
	res, err := l.run(ctx, ipArgv("-j", "-4", "addr", "show", "dev", name))
	if err != nil {
		return err
	}
	var links []struct {
		Addresses []struct {
			Local  string `json:"local"`
			Prefix int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if len(res.Stdout) > 64<<10 || json.Unmarshal(res.Stdout, &links) != nil {
		return fmt.Errorf("network: invalid address inventory")
	}
	have := map[string]bool{}
	for _, link := range links {
		for _, addr := range link.Addresses {
			p, err := netip.ParsePrefix(fmt.Sprintf("%s/%d", addr.Local, addr.Prefix))
			if err != nil || !p.Addr().Is4() {
				return fmt.Errorf("network: invalid IPv4 address inventory")
			}
			have[p.String()] = true
		}
	}
	want := map[string]bool{}
	for _, address := range desired {
		p, err := netip.ParsePrefix(address)
		if err != nil || !p.Addr().Is4() {
			return fmt.Errorf("network: invalid gateway address")
		}
		want[p.String()] = true
	}
	for _, address := range desired {
		p := netip.MustParsePrefix(address)
		if !have[p.String()] {
			// iproute2 cannot add the same local address with a new prefix
			// beside its old alias. Pool policy permits this only for empty
			// overflow pools, so remove just that old gateway before adding.
			for old := range have {
				if netip.MustParsePrefix(old).Addr() == p.Addr() && old != p.String() {
					if _, err := l.run(ctx, ipArgv("addr", "del", old, "dev", name)); err != nil {
						return err
					}
					delete(have, old)
				}
			}
			if err := l.AddAddress(ctx, name, p.String()); err != nil {
				return err
			}
		}
	}
	stale := make([]string, 0, len(have))
	for address := range have {
		if !want[address] {
			stale = append(stale, address)
		}
	}
	sort.Strings(stale)
	for _, address := range stale {
		if !want[address] {
			if _, err := l.run(ctx, ipArgv("addr", "del", address, "dev", name)); err != nil {
				return err
			}
		}
	}
	return nil
}
