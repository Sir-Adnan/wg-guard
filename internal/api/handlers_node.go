package api

import (
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/version"
)

// handleNodeHealth is the PUBLIC capability-discovery endpoint (api.md:
// "external systems integrate from GET /node/health alone"). It exposes only
// version and liveness — no topology, no counts.
func (s *Server) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": version.Version,
	})
}

// handleNode reports node identity, uptime, and the interface inventory.
func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	ifaces, err := s.Ifaces.List(r.Context())
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(ifaces))
	for _, ifc := range ifaces {
		devices, err := s.deviceCountForIface(r, ifc.ID)
		if err != nil {
			writeServiceErr(w, r, err)
			return
		}
		items = append(items, map[string]any{
			"id": ifc.ID, "name": ifc.Name, "listen_port": ifc.ListenPort,
			"ipv4_subnet": ifc.Subnet, "enabled": ifc.Enabled,
			"backend_mode": string(ifc.BackendMode), "devices": devices,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        version.Version,
		"node_id":        s.NodeID,
		"tools_version":  s.ToolsVersion,
		"uptime_seconds": int(uptimeOf(s).Seconds()),
		"interfaces":     items,
	})
}

func (s *Server) deviceCountForIface(r *http.Request, ifaceID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM devices WHERE interface_id = ?`, ifaceID).Scan(&n)
	return n, err
}

// handleNodeStats reports runtime performance counters (SPEC §40: basic node
// health; lightweight by design).
func (s *Server) handleNodeStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]any{
		"uptime_seconds": int(uptimeOf(s).Seconds()),
	}
	var users, devices int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&users); err == nil {
		stats["users"] = users
	}
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM devices`).Scan(&devices); err == nil {
		stats["devices"] = devices
	}
	writeJSON(w, http.StatusOK, stats)
}

// uptimeOf is the process uptime (zero without a collector).
func uptimeOf(s *Server) time.Duration {
	if s.Metrics != nil {
		return s.Metrics.Uptime()
	}
	return 0
}

// isOnline reports whether the device's last handshake is inside the
// configured online window (the accounting cycle's definition).
func isOnline(s *Server, lastHandshake *time.Time, windowSeconds int) bool {
	if lastHandshake == nil {
		return false
	}
	return time.Since(*lastHandshake) <= time.Duration(windowSeconds)*time.Second
}

// handleStats is the node-wide statistics rollup (stats.read).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{}
	var total, active, blocked, deleted int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&total); err == nil {
		out["users_total"] = total
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND status = 'active'`).Scan(&active); err == nil {
		out["users_active"] = active
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND status IN ('traffic_exceeded','expired','disabled','suspended')`).Scan(&blocked); err == nil {
		out["users_blocked"] = blocked
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NOT NULL`).Scan(&deleted); err == nil {
		out["users_deleted"] = deleted
	}
	var devices int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&devices); err == nil {
		out["devices"] = devices
	}
	var rx, tx int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(traffic_used_rx),0), COALESCE(SUM(traffic_used_tx),0)
		FROM users WHERE deleted_at IS NULL`).Scan(&rx, &tx); err == nil {
		out["traffic_used_rx"] = rx
		out["traffic_used_tx"] = tx
		out["traffic_used_total"] = rx + tx
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUserStats is the per-user summary (used + limits + remaining).
func (s *Server) handleUserStats(w http.ResponseWriter, r *http.Request) {
	u, err := s.Users.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	used := u.TrafficUsedRX + u.TrafficUsedTX
	out := map[string]any{
		"traffic_used_rx":    u.TrafficUsedRX,
		"traffic_used_tx":    u.TrafficUsedTX,
		"traffic_used_total": used,
	}
	if u.TrafficLimitBytes != nil {
		out["traffic_limit_bytes"] = *u.TrafficLimitBytes
		remaining := *u.TrafficLimitBytes - used
		if remaining < 0 {
			remaining = 0
		}
		out["traffic_remaining_bytes"] = remaining
		if *u.TrafficLimitBytes > 0 {
			out["traffic_percent_used"] = float64(used) / float64(*u.TrafficLimitBytes) * 100
		}
	} else {
		out["traffic_limit_bytes"] = nil
	}
	var deviceCount int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM devices WHERE user_id = ?`, u.ID).Scan(&deviceCount); err == nil {
		out["devices"] = deviceCount
	}
	out["last_activity_at"] = jsonTime(u.LastActivityAt)
	writeJSON(w, http.StatusOK, out)
}

// handleDeviceStats is the per-device observation summary.
func (s *Server) handleDeviceStats(w http.ResponseWriter, r *http.Request) {
	d, err := s.Devices.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	window, _ := s.Settings.GetInt(r.Context(), "accounting.online_window_seconds")
	out := map[string]any{
		"rx_bytes":          d.RXBytes,
		"tx_bytes":          d.TXBytes,
		"last_handshake_at": jsonTime(d.LastHandshake),
		"last_endpoint":     d.LastEndpoint,
	}
	if window > 0 && d.LastHandshake != nil {
		out["online_window_seconds"] = window
		// "Online" is defined by the handshake recency window (the same
		// definition the accounting cycle uses).
		out["online"] = isOnline(s, d.LastHandshake, window)
	}
	writeJSON(w, http.StatusOK, out)
}
