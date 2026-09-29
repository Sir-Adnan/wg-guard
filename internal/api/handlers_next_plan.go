package api

import (
	"net/http"
	"strconv"

	"github.com/Sir-Adnan/wg-guard/internal/integration"
)

func (s *Server) handleNextPlanGet(w http.ResponseWriter, r *http.Request) {
	owner := TokenFrom(r.Context()).Token.ResellerID
	queued, err := s.Integration.NextPlanForUser(r.Context(), r.PathValue("id"), owner)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_plan": queued})
}

func (s *Server) handleNextPlanActivations(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if len(r.URL.Query()["limit"]) > 1 {
		writeServiceErr(w, r, invalidRequestErr("provide one limit"))
		return
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeServiceErr(w, r, invalidRequestErr("limit must be 1-100"))
			return
		}
	}
	owner := TokenFrom(r.Context()).Token.ResellerID
	items, err := s.Integration.NextPlanActivations(r.Context(), r.PathValue("id"), owner, limit)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleNextPlanPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanID             string `json:"plan_id"`
		CarryUnusedTraffic bool   `json:"carry_unused_traffic"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	owner := TokenFrom(r.Context()).Token.ResellerID
	queued, err := s.Integration.QueueNextPlan(r.Context(), integration.QueueNextPlanInput{
		UserID: r.PathValue("id"), PlanID: req.PlanID, ResellerID: owner,
		CarryUnusedTraffic: req.CarryUnusedTraffic,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.next_plan_queued", queued.UserID, map[string]any{"plan_id": queued.PlanID})
	writeJSON(w, http.StatusOK, queued)
}

func (s *Server) handleNextPlanDelete(w http.ResponseWriter, r *http.Request) {
	owner := TokenFrom(r.Context()).Token.ResellerID
	if err := s.Integration.CancelNextPlan(r.Context(), r.PathValue("id"), owner); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.next_plan_canceled", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}
