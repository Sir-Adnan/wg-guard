package iface

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/ipam"
)

type PoolSuggestion struct {
	CIDR      string `json:"cidr"`
	Capacity  uint64 `json:"capacity"`
	Status    string `json:"status"` // available, overlap, current, full
	Interface string `json:"interface,omitempty"`
	Used      uint64 `json:"used"`
	Free      uint64 `json:"free"`
}
type PoolSuggestions struct {
	Automatic string           `json:"automatic"`
	Choices   []PoolSuggestion `json:"choices"`
}

// SuggestPools is a bounded read-only snapshot. Create/Update retain the
// authoritative transactional overlap checks; suggestions reserve no addresses.
func (s *Service) SuggestPools(ctx context.Context, name, exclude string, bits int) (PoolSuggestions, error) {
	out := PoolSuggestions{}
	if bits != 24 && bits != 23 && bits != 22 && bits != 21 && bits != 20 {
		return out, domain.E(domain.CodeInvalidRequest, "unsupported suggested pool size")
	}
	match := nameRe.FindStringSubmatch(name)
	if match == nil {
		return out, domain.E(domain.CodeInvalidRequest, "interface name must be awgN")
	}
	n, _ := strconv.Atoi(match[1])
	max, _ := s.reg.GetInt(ctx, "interfaces.max_count")
	if n >= max {
		return out, domain.E(domain.CodeInvalidRequest, "interface name exceeds configured cap")
	}
	ifaces, err := s.List(ctx)
	if err != nil {
		return out, err
	}
	var routes []netip.Prefix
	if s.hostRoutes != nil {
		routes, err = s.hostRoutes(ctx, name)
		if err != nil {
			return out, err
		}
	}
	usage := map[string]PoolUsage{}
	if exclude != "" {
		current, err := s.Get(ctx, exclude)
		if err != nil {
			return out, err
		}
		if current.Name != name {
			return out, domain.E(domain.CodeInvalidRequest, "profile name does not match")
		}
		pools, err := s.Capacity(ctx, exclude)
		if err != nil {
			return out, err
		}
		for _, pool := range pools {
			usage[pool.CIDR] = pool
		}
	}
	classify := func(cidr string) PoolSuggestion {
		p, err := ipam.Parse(cidr)
		if err != nil {
			return PoolSuggestion{CIDR: cidr, Status: "overlap"}
		}
		choice := PoolSuggestion{CIDR: cidr, Capacity: ipam.Capacity(p), Free: ipam.Capacity(p), Status: "available"}
		for _, i := range ifaces {
			for _, pool := range i.Pools {
				if p.Overlaps(netip.MustParsePrefix(pool)) {
					choice.Status = "overlap"
					choice.Free = 0
					choice.Interface = i.Name
					if i.ID == exclude && pool == cidr {
						u := usage[cidr]
						choice.Status = "current"
						choice.Used = u.Used
						choice.Free = u.Free
						if u.Free == 0 {
							choice.Status = "full"
						}
					}
					return choice
				}
			}
		}
		if checkHostPool(cidr, routes) != nil {
			choice.Status = "overlap"
			choice.Interface = "host"
			choice.Free = 0
		}
		return choice
	}
	preferred := s.defaultPool(ctx, n)
	if classify(preferred).Status == "available" {
		out.Automatic = preferred
	} else {
		for index := 0; index < 256; index++ {
			cidr := fmt.Sprintf("10.8.%d.0/24", index)
			if classify(cidr).Status == "available" {
				out.Automatic = cidr
				break
			}
		}
	}
	step := 1 << (24 - bits)
	// One available size example plus existing/blocked rows explain why adjacent
	// networks cannot be selected. Bound both inspection and response counts.
	available := 0
	for index := 0; index < 256 && len(out.Choices) < 12; index += step {
		choice := classify(fmt.Sprintf("10.8.%d.0/%d", index, bits))
		out.Choices = append(out.Choices, choice)
		if choice.Status == "available" {
			available++
			if available == 6 {
				break
			}
		}
	}
	return out, nil
}
