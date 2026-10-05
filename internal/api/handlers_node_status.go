package api

import (
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/nodestatus"
)

// This is authenticated operational evidence, not the public liveness endpoint.
// HTTP 200 means a snapshot was returned; readiness can still be not_ready.
func (s *Server) handleNodeStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	snapshot := (nodestatus.Source{}).Read(r.Context(), now)
	if s.NodeStatus != nil {
		snapshot = s.NodeStatus(r.Context(), now)
	}
	writeJSON(w, http.StatusOK, snapshot)
}
