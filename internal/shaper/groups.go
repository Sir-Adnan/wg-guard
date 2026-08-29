package shaper

import (
	"context"
	"fmt"

	"github.com/Sir-Adnan/wg-guard/internal/database"
)

// LoadGroups returns the desired shaping state: one group per (user,
// interface) with speed limits set, aggregating that user's enabled device
// IPs on enabled interfaces. Soft-deleted or disabled users and disabled
// devices are excluded — they do not have peers in the backend.
func LoadGroups(ctx context.Context, db *database.DB) ([]Group, error) {
	rows, err := db.QueryContext(ctx, `SELECT i.name, u.id, u.speed_limit_kbps, d.ipv4_address
		FROM devices d
		JOIN users u ON u.id = d.user_id
		JOIN tunnel_interfaces i ON i.id = d.interface_id
		WHERE u.deleted_at IS NULL AND u.enabled = 1 AND d.enabled = 1
		  AND i.enabled = 1 AND u.speed_limit_kbps IS NOT NULL
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
			iface, user string
			kbps        int
			ip          string
		)
		if err := rows.Scan(&iface, &user, &kbps, &ip); err != nil {
			return nil, fmt.Errorf("shaper: scan group: %w", err)
		}
		k := key{iface, user}
		if i, ok := idx[k]; ok {
			out[i].IPs = append(out[i].IPs, ip)
			continue
		}
		idx[k] = len(out)
		out = append(out, Group{InterfaceName: iface, UserID: user, IPs: []string{ip}, Kbps: kbps})
	}
	return out, rows.Err()
}
