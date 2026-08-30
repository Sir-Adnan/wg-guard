package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func pathID(r *http.Request, name string) string {
	return r.PathValue(name)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var req userCreateReq
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := s.Users.Create(r.Context(), user.Input{
		Username:           req.Username,
		DisplayName:        req.DisplayName,
		Note:               req.Note,
		Tags:               req.Tags,
		TrafficLimitBytes:  req.TrafficLimitBytes,
		SpeedLimitDownKbps: req.SpeedLimitDownKbps,
		SpeedLimitUpKbps:   req.SpeedLimitUpKbps,
		DeviceLimit:        req.DeviceLimit,
		PlanID:             req.PlanID,
		InterfaceID:        req.InterfaceID,
		StartPolicy:        domain.StartPolicy(req.StartPolicy),
		DurationSeconds:    req.DurationSeconds,
		Enabled:            req.Enabled,
		Metadata:           req.Metadata,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.created", u.ID, map[string]any{"username": u.Username})
	writeJSON(w, http.StatusCreated, toUserDTO(u))
}

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lq := user.ListQuery{}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit must be an integer")
			return
		}
		lq.Limit = n
	}
	lq.Cursor = q.Get("cursor")
	if v := q.Get("sort"); v != "" {
		lq.Sort = user.ListSort(v)
	}
	// Sort-specific default order: username/expires read naturally ascending.
	switch lq.Sort {
	case user.SortUsername, user.SortExpiresAt:
		lq.Desc = q.Get("order") != "desc"
	default:
		lq.Desc = q.Get("order") != "asc"
	}

	f := user.ListFilter{}
	if v := q.Get("username"); v != "" {
		f.Username = &v
	}
	if v := q.Get("status"); v != "" {
		st := domain.UserStatus(v)
		if !st.Valid() {
			writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", "unknown status filter")
			return
		}
		f.Status = &st
	}
	if q.Has("traffic_exceeded") {
		v := q.Get("traffic_exceeded") == "true" || q.Get("traffic_exceeded") == "1"
		f.TrafficExceeded = &v
	}
	if q.Has("enabled") {
		v := q.Get("enabled") == "true" || q.Get("enabled") == "1"
		f.Enabled = &v
	}
	for key, dst := range map[string]**time.Time{
		"expires_before": &f.ExpiresBefore,
		"expires_after":  &f.ExpiresAfter,
		"created_before": &f.CreatedBefore,
		"created_after":  &f.CreatedAfter,
	} {
		if v := q.Get(key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", key+" must be RFC3339")
				return
			}
			*dst = &t
		}
	}
	if v := q.Get("plan_id"); v != "" {
		f.PlanID = &v
	}
	if v := q.Get("interface_id"); v != "" {
		f.InterfaceID = &v
	}
	lq.Filter = f

	page, err := s.Users.ListPage(r.Context(), lq)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	items := make([]userDTO, 0, len(page.Items))
	for _, u := range page.Items {
		items = append(items, toUserDTO(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"next_cursor": page.NextCursor,
	})
}

func (s *Server) handleUserGet(w http.ResponseWriter, r *http.Request) {
	u, err := s.Users.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req userPatchReq
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := s.Users.Update(r.Context(), id, user.Input{
		DisplayName:        req.DisplayName,
		Note:               req.Note,
		Tags:               req.Tags,
		TrafficLimitBytes:  req.TrafficLimitBytes,
		SpeedLimitDownKbps: req.SpeedLimitDownKbps,
		SpeedLimitUpKbps:   req.SpeedLimitUpKbps,
		DeviceLimit:        req.DeviceLimit,
		PlanID:             req.PlanID,
		InterfaceID:        req.InterfaceID,
		DurationSeconds:    req.DurationSeconds,
		Enabled:            req.Enabled,
		Metadata:           req.Metadata,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.updated", id, map[string]any{"username": u.Username})
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	u, err := s.Users.Get(r.Context(), id)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	if err := s.Users.SoftDelete(r.Context(), id); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.deleted", id, map[string]any{"username": u.Username})
	s.reconcile(r) // deleted users lose their peers
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleUserEnable(w http.ResponseWriter, r *http.Request) {
	u, err := s.Users.SetEnabledStatus(r.Context(), pathID(r, "id"), true, "")
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.enabled", u.ID, map[string]any{"username": u.Username})
	s.reconcile(r)
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

func (s *Server) handleUserDisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(w, r, &req) // body optional
	reason := domain.DisableReason(req.Reason)
	u, err := s.Users.SetEnabledStatus(r.Context(), pathID(r, "id"), false, reason)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.disabled", u.ID, map[string]any{"username": u.Username})
	s.reconcile(r)
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

func (s *Server) handleUserRenew(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		Mode            string     `json:"mode"`
		DurationSeconds *int64     `json:"duration_seconds"`
		Exact           *time.Time `json:"exact"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := s.Users.Renew(r.Context(), id, req.Mode, req.DurationSeconds, req.Exact)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.renewed", id, map[string]any{"mode": req.Mode})
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

// --- Traffic mutations (idempotent; SPEC §24) ---

func (s *Server) handleTrafficAdd(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		RXBytes *int64 `json:"rx_bytes"`
		TXBytes *int64 `json:"tx_bytes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	rx, tx := int64(0), int64(0)
	if req.RXBytes != nil {
		rx = *req.RXBytes
	}
	if req.TXBytes != nil {
		tx = *req.TXBytes
	}
	if err := s.Accounting.AddTraffic(r.Context(), id, rx, tx, s.actor(r)); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.traffic_added", id, map[string]any{"rx_bytes": rx, "tx_bytes": tx})
	u, err := s.Users.Get(r.Context(), id)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

func (s *Server) handleTrafficSet(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		RXBytes *int64 `json:"rx_bytes"`
		TXBytes *int64 `json:"tx_bytes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.Accounting.SetTraffic(r.Context(), id, req.RXBytes, req.TXBytes, s.actor(r)); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.traffic_set", id, nil)
	u, err := s.Users.Get(r.Context(), id)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

func (s *Server) handleTrafficReset(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if err := s.Accounting.ResetTraffic(r.Context(), id, s.actor(r)); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.traffic_reset", id, nil)
	u, err := s.Users.Get(r.Context(), id)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

// --- Bulk (SPEC §11) ---

func (s *Server) handleBulkCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count              int              `json:"count"`
		Prefix             string           `json:"prefix"`
		StartIndex         int              `json:"start_index"`
		Width              int              `json:"width"`
		DisplayName        *string          `json:"display_name"`
		Note               *string          `json:"note"`
		Tags               []string         `json:"tags"`
		TrafficLimitBytes  domain.OptInt64  `json:"traffic_limit_bytes"`
		SpeedLimitDownKbps domain.OptInt    `json:"speed_limit_down_kbps"`
		SpeedLimitUpKbps   domain.OptInt    `json:"speed_limit_up_kbps"`
		DeviceLimit        domain.OptInt    `json:"device_limit"`
		PlanID             domain.OptString `json:"plan_id"`
		InterfaceID        domain.OptString `json:"interface_id"`
		StartPolicy        string           `json:"start_policy"`
		DurationSeconds    *int64           `json:"duration_seconds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.Users.CreateBulk(r.Context(), req.Prefix, req.Count, req.StartIndex, req.Width, user.Input{
		DisplayName:        req.DisplayName,
		Note:               req.Note,
		Tags:               req.Tags,
		TrafficLimitBytes:  req.TrafficLimitBytes,
		SpeedLimitDownKbps: req.SpeedLimitDownKbps,
		SpeedLimitUpKbps:   req.SpeedLimitUpKbps,
		DeviceLimit:        req.DeviceLimit,
		PlanID:             req.PlanID,
		InterfaceID:        req.InterfaceID,
		StartPolicy:        domain.StartPolicy(req.StartPolicy),
		DurationSeconds:    req.DurationSeconds,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.bulk_created", "", map[string]any{
		"count": len(res.Users), "skipped": res.Skipped, "prefix": req.Prefix,
	})
	items := make([]userDTO, 0, len(res.Users))
	for _, u := range res.Users {
		items = append(items, toUserDTO(u))
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"created": items,
		"skipped": res.Skipped,
	})
}

// bulk-action applies one action to many users with per-item results.
func (s *Server) handleBulkAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action  string           `json:"action"`
		UserIDs []string         `json:"user_ids"`
		Params  bulkActionParams `json:"params"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.UserIDs) == 0 || len(req.UserIDs) > 500 {
		writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", "user_ids must contain 1-500 ids")
		return
	}
	results := make([]bulkItemResult, 0, len(req.UserIDs))
	okCount := 0
	for _, id := range req.UserIDs {
		err := s.applyBulkAction(r, req.Action, id, req.Params)
		res := bulkItemResult{ID: id}
		if err != nil {
			res.OK = false
			res.Error = domain.CodeOf(err)
			res.Message = err.Error()
		} else {
			res.OK = true
			okCount++
		}
		results = append(results, res)
	}
	s.audit(r, "user.bulk_action", "", map[string]any{
		"action": req.Action, "total": len(req.UserIDs), "ok": okCount,
	})
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "ok": okCount})
}

type bulkActionParams struct {
	Reason          string     `json:"reason"`
	Mode            string     `json:"mode"`
	DurationSeconds *int64     `json:"duration_seconds"`
	Exact           *time.Time `json:"exact"`
	RXBytes         *int64     `json:"rx_bytes"`
	TXBytes         *int64     `json:"tx_bytes"`
	// Update patch (action=update).
	DisplayName        *string         `json:"display_name"`
	Note               *string         `json:"note"`
	TrafficLimitBytes  domain.OptInt64 `json:"traffic_limit_bytes"`
	SpeedLimitDownKbps domain.OptInt   `json:"speed_limit_down_kbps"`
	SpeedLimitUpKbps   domain.OptInt   `json:"speed_limit_up_kbps"`
	DeviceLimit        domain.OptInt   `json:"device_limit"`
}

type bulkItemResult struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *Server) applyBulkAction(r *http.Request, action, id string, p bulkActionParams) error {
	ctx := r.Context()
	switch action {
	case "enable":
		_, err := s.Users.SetEnabledStatus(ctx, id, true, "")
		return err
	case "disable":
		_, err := s.Users.SetEnabledStatus(ctx, id, false, domain.DisableReason(p.Reason))
		return err
	case "delete":
		return s.Users.SoftDelete(ctx, id)
	case "renew":
		mode := p.Mode
		if mode == "" {
			mode = "from_expiration"
		}
		_, err := s.Users.Renew(ctx, id, mode, p.DurationSeconds, p.Exact)
		return err
	case "reset_traffic":
		return s.Accounting.ResetTraffic(ctx, id, s.actor(r))
	case "add_traffic":
		rx, tx := int64(0), int64(0)
		if p.RXBytes != nil {
			rx = *p.RXBytes
		}
		if p.TXBytes != nil {
			tx = *p.TXBytes
		}
		return s.Accounting.AddTraffic(ctx, id, rx, tx, s.actor(r))
	case "update":
		_, err := s.Users.Update(ctx, id, user.Input{
			DisplayName:        p.DisplayName,
			Note:               p.Note,
			TrafficLimitBytes:  p.TrafficLimitBytes,
			SpeedLimitDownKbps: p.SpeedLimitDownKbps,
			SpeedLimitUpKbps:   p.SpeedLimitUpKbps,
			DeviceLimit:        p.DeviceLimit,
		})
		return err
	default:
		return domain.E(domain.CodeInvalidRequest, "unknown bulk action %q", action)
	}
}

// actor maps the authenticated token to an accounting Actor.
func (s *Server) actor(r *http.Request) accounting.Actor {
	v := TokenFrom(r.Context())
	if v == nil {
		return accounting.Actor{}
	}
	return accounting.Actor{Type: "token", ID: v.Token.ID}
}
