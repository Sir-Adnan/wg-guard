package iface

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/ipam"
)

// Prefer the configured/name-derived pool, then scan a bounded /24 ladder.
// This prevents an earlier /22 or overflow pool from breaking blank creation.
func (s *Service) selectAutomaticPool(ctx context.Context, tx *sql.Tx, i *Interface) error {
	if err := s.checkAutomaticPool(ctx, tx, i, i.Subnet); err == nil {
		return nil
	} else if domain.CodeOf(err) != domain.CodeSubnetOverlap {
		return err
	}
	for n := 0; n < 256; n++ {
		pool := fmt.Sprintf("10.8.%d.0/24", n)
		if err := s.checkAutomaticPool(ctx, tx, i, pool); err == nil {
			i.Subnet, i.Pools = pool, []string{pool}
			return nil
		} else if domain.CodeOf(err) != domain.CodeSubnetOverlap {
			return err
		}
	}
	return domain.E(domain.CodeSubnetOverlap, "automatic IPv4 pools are occupied; choose an explicit non-overlapping pool")
}

func checkHostPool(pool string, routes []netip.Prefix) error {
	p := netip.MustParsePrefix(pool)
	for _, route := range routes {
		if p.Overlaps(route) {
			return domain.E(domain.CodeSubnetOverlap, "pool %s overlaps host route %s", pool, route)
		}
	}
	return nil
}
func (s *Service) checkAutomaticPool(ctx context.Context, tx *sql.Tx, i *Interface, pool string) error {
	if err := checkHostPool(pool, i.blockedRoutes); err != nil {
		return err
	}
	return s.checkOverlaps(ctx, tx, i.ID, pool)
}

type PoolUsage struct {
	CIDR     string `json:"cidr"`
	Capacity uint64 `json:"capacity"`
	Used     uint64 `json:"used"`
	Free     uint64 `json:"free"`
}

// Capacity includes every registered configuration, also disabled peers and
// retained devices of old soft-deleted accounts. It is a snapshot, not a lease.
func (s *Service) Capacity(ctx context.Context, id string) ([]PoolUsage, error) {
	i, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]PoolUsage, len(i.Pools))
	for n, pool := range i.Pools {
		out[n] = PoolUsage{CIDR: pool, Capacity: ipam.Capacity(netip.MustParsePrefix(pool))}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ipv4_address FROM devices WHERE interface_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, domain.E(domain.CodeSubnetInvalid, "stored device address is invalid")
		}
		for n := range out {
			if netip.MustParsePrefix(out[n].CIDR).Contains(p.Addr()) {
				out[n].Used++
				break
			}
		}
	}
	for n := range out {
		if out[n].Used <= out[n].Capacity {
			out[n].Free = out[n].Capacity - out[n].Used
		}
	}
	return out, rows.Err()
}

func (s *Service) updatePoolsTx(ctx context.Context, tx *sql.Tx, i *Interface, pools []string) error {
	if err := ipam.Validate(pools); err != nil {
		return err
	}
	var primary, extras string
	if err := tx.QueryRowContext(ctx, `SELECT ipv4_subnet, ipv4_extra_pools FROM tunnel_interfaces WHERE id = ?`, i.ID).Scan(&primary, &extras); err != nil {
		return err
	}
	if pools[0] != primary {
		return domain.E(domain.CodeSubnetInvalid, "the primary pool is immutable; add an overflow pool instead")
	}
	old, err := ipam.Decode(primary, extras)
	if err != nil {
		return err
	}
	for _, pool := range pools {
		if err := checkHostPool(pool, i.blockedRoutes); err != nil {
			return err
		}
		if err := s.checkOverlaps(ctx, tx, i.ID, pool); err != nil {
			return err
		}
	}
	// An occupied pool cannot be removed or resized; disabled devices count.
	removed := make([]netip.Prefix, 0, len(old))
	for _, pool := range old {
		found := false
		for _, kept := range pools {
			if pool == kept {
				found = true
				break
			}
		}
		if !found {
			removed = append(removed, netip.MustParsePrefix(pool))
		}
	}
	if len(removed) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT ipv4_address FROM devices WHERE interface_id = ?`, i.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			addr, err := netip.ParsePrefix(raw)
			if err != nil {
				rows.Close()
				return err
			}
			for _, pool := range removed {
				if pool.Contains(addr.Addr()) {
					rows.Close()
					return domain.E(domain.CodeSubnetOverlap, "pool %s still contains registered devices", pool)
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tunnel_interfaces SET ipv4_extra_pools = ? WHERE id = ?`, ipam.Extras(pools), i.ID); err != nil {
		return err
	}
	i.Pools = append([]string(nil), pools...)
	return nil
}
