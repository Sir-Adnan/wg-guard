package web

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Server) handlePoolSuggestions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	bits := 24
	if raw := r.URL.Query().Get("prefix"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_size"})
			return
		}
		bits = value
	}
	suggestions, err := s.Ifaces.SuggestPools(r.Context(), r.URL.Query().Get("name"), r.URL.Query().Get("exclude"), bits)
	if err != nil {
		w.WriteHeader(422)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "pool_suggestions_unavailable"})
		return
	}
	_ = json.NewEncoder(w).Encode(suggestions)
}
