package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"
)

// PoolConflicts inventories IPv4 host routes once per structural mutation.
// The default route is not a subnet conflict; this owned tunnel's own routes
// are excluded during expansion. No route is modified.
func (l *Links) PoolConflicts(ctx context.Context, ownedName string) ([]netip.Prefix, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	res, err := l.run(ctx, ipArgv("-j", "-4", "route", "show", "table", "all"))
	if err != nil {
		return nil, err
	}
	var routes []struct {
		Destination string `json:"dst"`
		Device      string `json:"dev"`
	}
	if len(res.Stdout) > 1<<20 || json.Unmarshal(res.Stdout, &routes) != nil {
		return nil, fmt.Errorf("network: invalid route inventory")
	}
	out := make([]netip.Prefix, 0, len(routes))
	for _, route := range routes {
		if route.Destination == "default" || route.Device == ownedName || route.Destination == "" {
			continue
		}
		p, err := netip.ParsePrefix(route.Destination)
		if err != nil {
			if addr, e := netip.ParseAddr(route.Destination); e == nil && addr.Is4() {
				p = netip.PrefixFrom(addr, 32)
			} else {
				return nil, fmt.Errorf("network: invalid IPv4 route")
			}
		}
		if p.Bits() > 0 && p.Addr().Is4() {
			out = append(out, p.Masked())
		}
	}
	return out, nil
}
