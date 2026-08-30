package web

import (
	"net/http"
	"time"
)

// dashData is the dashboard payload (counters in 5.1; charts + host stats
// land in 5.3 without changing the shape).
type dashData struct {
	Total, Active, Waiting, Online, Expired, Exceeded, Expiring int64
	NodeID                                                      string
	ToolsVersion                                                string
	OnlineWindow                                                int64 // seconds, shown as tooltip meta
}

// handleDashboard renders the operational overview. One aggregate query per
// table — the users/devices tables are indexed for status and handshake
// lookups; at the target scale (thousands of rows) these are sub-ms.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := dashData{
		NodeID:       s.NodeID,
		ToolsVersion: s.ToolsVersion,
		OnlineWindow: 180,
	}
	if v, err := s.Settings.GetInt(ctx, "accounting.online_window_seconds"); err == nil && v > 0 {
		d.OnlineWindow = int64(v)
	}

	cutoff := time.Now().UTC().Add(-time.Duration(d.OnlineWindow) * time.Second).
		Format(time.RFC3339Nano)
	soon := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339Nano)

	err := s.DB.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(status = 'active'), 0),
		COALESCE(SUM(status = 'waiting_first_connection'), 0),
		COALESCE(SUM(status = 'expired'), 0),
		COALESCE(SUM(status = 'traffic_exceeded'), 0),
		COALESCE(SUM(status = 'active' AND expires_at IS NOT NULL AND expires_at < ?), 0)
		FROM users WHERE deleted_at IS NULL`, soon).
		Scan(&d.Total, &d.Active, &d.Waiting, &d.Expired, &d.Exceeded, &d.Expiring)
	if err != nil {
		s.logError(r, "dashboard counters", err)
	}

	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM devices d JOIN users u ON u.id = d.user_id
		WHERE u.deleted_at IS NULL AND d.enabled = 1
		  AND d.last_handshake_at IS NOT NULL AND d.last_handshake_at >= ?`, cutoff).
		Scan(&d.Online)

	_ = s.render(w, r, "dashboard", "app", d)
}
