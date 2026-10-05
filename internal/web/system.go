package web

import (
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/nodestatus"
)

type systemData struct {
	Snapshot  nodestatus.Snapshot
	Attention []systemAttention
}

type systemAttention struct{ Code string }

func (s *Server) handleSystemPage(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	d := systemData{Snapshot: (nodestatus.Source{}).Read(r.Context(), now)}
	if s.NodeStatus != nil {
		d.Snapshot = s.NodeStatus(r.Context(), now)
	}
	if d.Snapshot.Readiness != nodestatus.Ready {
		d.Attention = append(d.Attention, systemAttention{"readiness"})
	}
	if d.Snapshot.Runtime.State == nodestatus.Pending {
		d.Attention = append(d.Attention, systemAttention{"runtime"})
	}
	if d.Snapshot.Accounting.State != nodestatus.Current {
		d.Attention = append(d.Attention, systemAttention{"accounting"})
	}
	if d.Snapshot.Telemetry.State == nodestatus.Unavailable || d.Snapshot.Telemetry.State == nodestatus.Degraded {
		d.Attention = append(d.Attention, systemAttention{"telemetry"})
	}
	_ = s.render(w, r, "system", "app", d)
}

func statusTone(state nodestatus.State) string {
	switch state {
	case nodestatus.Ready, nodestatus.Applied, nodestatus.Current, nodestatus.Healthy:
		return "badge--ok"
	case nodestatus.Pending, nodestatus.Degraded:
		return "badge--warn"
	case nodestatus.Unobserved:
		return ""
	default:
		return "badge--danger"
	}
}

func (v *View) StatusTone(state nodestatus.State) string { return statusTone(state) }
