package shaper

import (
	"context"
	"fmt"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

// LoadGroups returns the desired shaping state: one group per (user,
// interface) with at least one direction limited, aggregating that user's
// enabled device IPs on enabled interfaces. Soft-deleted or disabled users
// and disabled devices are excluded — they do not have peers in the backend.
// NULL in a direction column means unlimited (0 in the Group).
func LoadGroups(ctx context.Context, db *database.DB) ([]Group, error) {
	rows, err := db.QueryContext(ctx, `SELECT i.name, u.id, u.speed_limit_down_kbps,
			u.speed_limit_up_kbps, d.ipv4_address
		FROM devices d
		JOIN users u ON u.id = d.user_id
		JOIN tunnel_interfaces i ON i.id = d.interface_id
		WHERE u.deleted_at IS NULL AND u.enabled = 1 AND d.enabled = 1 AND i.enabled = 1
		  AND (u.speed_limit_down_kbps IS NOT NULL OR u.speed_limit_up_kbps IS NOT NULL)
		ORDER BY i.name, u.id, d.ipv4_address`)
	if err != nil {
		return nil, fmt.Errorf("shaper: load groups: %w", err)
	}
	defer rows.Close()

	type key struct{ iface, user string }
	idx := map[key]int{}
	var out []Group
	for rows.Next() {
		var (
			iface, user, ip string
			down, up        *int
		)
		if err := rows.Scan(&iface, &user, &down, &up, &ip); err != nil {
			return nil, fmt.Errorf("shaper: scan group: %w", err)
		}
		k := key{iface, user}
		if i, ok := idx[k]; ok {
			out[i].IPs = append(out[i].IPs, ip)
			continue
		}
		g := Group{InterfaceName: iface, UserID: user, IPs: []string{ip}}
		if down != nil {
			g.DownKbps = *down
		}
		if up != nil {
			g.UpKbps = *up
		}
		idx[k] = len(out)
		out = append(out, g)
	}
	return out, rows.Err()
}
