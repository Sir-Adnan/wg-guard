package api

import "net/http"

func (s *Server) handleIfaceCapacity(w http.ResponseWriter, r *http.Request) {
	pools, err := s.Ifaces.Capacity(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	var capacity, used, free uint64
	for _, p := range pools {
		capacity += p.Capacity
		used += p.Used
		free += p.Free
	}
	writeJSON(w, http.StatusOK, map[string]any{"interface_id": pathID(r, "id"), "capacity": capacity, "used": used, "free": free, "pools": pools})
}
