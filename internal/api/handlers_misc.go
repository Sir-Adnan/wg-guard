package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// handleTrafficSeries returns the user's traffic time series from the
// accounting samples/rollups. granularity: samples (default, ≤ 48 h),
// hourly (≤ 30 d), daily (≤ 365 d) — matching the retention settings.
func (s *Server) handleTrafficSeries(w http.ResponseWriter, r *http.Request) {
	userID := pathID(r, "id")
	if _, err := s.Users.Get(r.Context(), userID); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	q := r.URL.Query()
	granularity := q.Get("granularity")
	if granularity == "" {
		granularity = "samples"
	}
	switch granularity {
	case "samples", "hourly", "daily":
	default:
		writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"granularity must be samples|hourly|daily")
		return
	}
	hours := 48
	if v := q.Get("hours"); v != "" {
		n, err := atoi(v)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", "hours must be an integer")
			return
		}
		hours = n
	}
	maxHours := 48
	if granularity == "hourly" {
		maxHours = 24 * 30
	} else if granularity == "daily" {
		maxHours = 24 * 365
	}
	if hours < 1 || hours > maxHours {
		writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"hours must be 1-"+itoa2(maxHours)+" for "+granularity)
		return
	}

	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	table, bucket := "traffic_samples", "ts"
	valueExpr := "SUM(d.rx_delta), SUM(d.tx_delta)"
	granFilter := ""
	switch granularity {
	case "hourly", "daily":
		// Rollups aggregate the deltas into rx/tx per bucket; the table
		// holds both granularities, so filter explicitly.
		table, bucket = "traffic_rollups", "bucket_start"
		valueExpr = "SUM(d.rx), SUM(d.tx)"
		granFilter = " AND d.granularity = '" + granularity + "'"
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT d.`+bucket+`, `+valueExpr+`
		FROM `+table+` d
		JOIN devices dev ON dev.id = d.device_id
		WHERE dev.user_id = ? AND d.`+bucket+` >= ?`+granFilter+`
		GROUP BY d.`+bucket+` ORDER BY d.`+bucket+``,
		userID, since.Format(time.RFC3339Nano))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	defer rows.Close()
	type point struct {
		TS string `json:"ts"`
		RX int64  `json:"rx"`
		TX int64  `json:"tx"`
	}
	series := []point{}
	for rows.Next() {
		var (
			p      point
			rx, tx int64
		)
		if err := rows.Scan(&p.TS, &rx, &tx); err != nil {
			writeServiceErr(w, r, err)
			return
		}
		p.RX, p.TX = rx, tx
		series = append(series, p)
	}
	if err := rows.Err(); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"granularity": granularity,
		"hours":       hours,
		"series":      series,
	})
}

// --- Plans ---

func (s *Server) handlePlanList(w http.ResponseWriter, r *http.Request) {
	plans, err := s.Plans.List(r.Context())
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	items := make([]planDTO, 0, len(plans))
	for _, p := range plans {
		items = append(items, toPlanDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name               string           `json:"name"`
		TrafficLimitBytes  domain.OptInt64  `json:"traffic_limit_bytes"`
		DurationSeconds    *int64           `json:"duration_seconds"`
		StartPolicy        string           `json:"start_policy"`
		DeviceLimit        domain.OptInt    `json:"device_limit"`
		SpeedLimitDownKbps domain.OptInt    `json:"speed_limit_down_kbps"`
		SpeedLimitUpKbps   domain.OptInt    `json:"speed_limit_up_kbps"`
		InterfaceID        domain.OptString `json:"interface_id"`
		Enabled            *bool            `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := s.Plans.Create(r.Context(), plan.Input{
		Name:               req.Name,
		TrafficLimitBytes:  req.TrafficLimitBytes,
		DurationSeconds:    req.DurationSeconds,
		StartPolicy:        domain.StartPolicy(req.StartPolicy),
		DeviceLimit:        req.DeviceLimit,
		SpeedLimitDownKbps: req.SpeedLimitDownKbps,
		SpeedLimitUpKbps:   req.SpeedLimitUpKbps,
		InterfaceID:        req.InterfaceID,
		Enabled:            req.Enabled,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "plan.created", p.ID, map[string]any{"name": p.Name})
	writeJSON(w, http.StatusCreated, toPlanDTO(p))
}

func (s *Server) handlePlanGet(w http.ResponseWriter, r *http.Request) {
	p, err := s.Plans.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPlanDTO(p))
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		Name               string           `json:"name"`
		TrafficLimitBytes  domain.OptInt64  `json:"traffic_limit_bytes"`
		DurationSeconds    *int64           `json:"duration_seconds"`
		StartPolicy        string           `json:"start_policy"`
		DeviceLimit        domain.OptInt    `json:"device_limit"`
		SpeedLimitDownKbps domain.OptInt    `json:"speed_limit_down_kbps"`
		SpeedLimitUpKbps   domain.OptInt    `json:"speed_limit_up_kbps"`
		InterfaceID        domain.OptString `json:"interface_id"`
		Enabled            *bool            `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := s.Plans.Update(r.Context(), id, plan.Input{
		Name:               req.Name,
		TrafficLimitBytes:  req.TrafficLimitBytes,
		DurationSeconds:    req.DurationSeconds,
		StartPolicy:        domain.StartPolicy(req.StartPolicy),
		DeviceLimit:        req.DeviceLimit,
		SpeedLimitDownKbps: req.SpeedLimitDownKbps,
		SpeedLimitUpKbps:   req.SpeedLimitUpKbps,
		InterfaceID:        req.InterfaceID,
		Enabled:            req.Enabled,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "plan.updated", id, map[string]any{"name": p.Name})
	writeJSON(w, http.StatusOK, toPlanDTO(p))
}

func (s *Server) handlePlanDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if err := s.Plans.Delete(r.Context(), id); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "plan.deleted", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// --- Interfaces ---

func (s *Server) handleIfaceList(w http.ResponseWriter, r *http.Request) {
	ifaces, err := s.Ifaces.List(r.Context())
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	items := make([]ifaceDTO, 0, len(ifaces))
	for _, ifc := range ifaces {
		items = append(items, toIfaceDTO(ifc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleIfaceCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string          `json:"name"`
		ListenPort       int             `json:"listen_port"`
		Subnet           string          `json:"ipv4_subnet"`
		MTU              int             `json:"mtu"`
		Obfuscation      *obfuscationReq `json:"obfuscation"`
		Preset           string          `json:"preset"`
		BackendMode      string          `json:"backend_mode"`
		EndpointOverride string          `json:"endpoint_override"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	in := iface.CreateInput{
		Name: req.Name, ListenPort: req.ListenPort, Subnet: req.Subnet, MTU: req.MTU,
		Preset: req.Preset, BackendMode: domain.BackendMode(req.BackendMode),
		EndpointOverride: req.EndpointOverride,
	}
	if req.Obfuscation != nil {
		in.Obfuscation = req.Obfuscation.toIface(nil)
	}
	ifc, err := s.Ifaces.Create(r.Context(), in)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "interface.created", ifc.ID, map[string]any{"name": ifc.Name})
	s.reconcile(r) // create the link now
	writeJSON(w, http.StatusCreated, toIfaceDTO(ifc))
}

func (s *Server) handleIfaceGet(w http.ResponseWriter, r *http.Request) {
	ifc, err := s.Ifaces.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toIfaceDTO(ifc))
}

func (s *Server) handleIfaceUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		MTU              *int            `json:"mtu"`
		Enabled          *bool           `json:"enabled"`
		EndpointOverride *string         `json:"endpoint_override"`
		Obfuscation      *obfuscationReq `json:"obfuscation"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var obfuscation *iface.Obfuscation
	if req.Obfuscation != nil {
		current, err := s.Ifaces.Get(r.Context(), id)
		if err != nil {
			writeServiceErr(w, r, err)
			return
		}
		mapped := req.Obfuscation.toIface(&current.Obfuscation)
		obfuscation = &mapped
	}
	ifc, err := s.Ifaces.Update(r.Context(), id, iface.UpdateInput{
		MTU: req.MTU, Enabled: req.Enabled,
		EndpointOverride: req.EndpointOverride, Obfuscation: obfuscation,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "interface.updated", id, map[string]any{"name": ifc.Name})
	s.reconcile(r) // param drift applies / link recreation happens here
	writeJSON(w, http.StatusOK, toIfaceDTO(ifc))
}

func (s *Server) handleIfaceDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if err := s.Ifaces.Delete(r.Context(), id); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "interface.deleted", id, nil)
	s.reconcile(r)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// --- Settings ---

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	type settingOut struct {
		Key       string `json:"key"`
		Category  string `json:"category"`
		Kind      string `json:"kind"`
		Value     any    `json:"value"`
		Secret    bool   `json:"secret"`
		SecretSet bool   `json:"secret_set"`
	}
	items := []settingOut{}
	for _, def := range s.Settings.Definitions() {
		out := settingOut{Key: def.Key, Category: def.Category, Kind: def.Kind.String(), Secret: def.Secret}
		if def.Secret {
			// Secrets are never returned; the caller learns only whether one
			// is set (security.md).
			v, err := s.Settings.GetSecret(r.Context(), def.Key)
			out.SecretSet = err == nil && v != ""
			out.Value = nil
		} else {
			v, err := s.Settings.Get(r.Context(), def.Key)
			if err != nil {
				continue
			}
			out.Value = v
		}
		items = append(items, out)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req) == 0 || len(req) > 100 {
		writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", "provide 1-100 setting keys")
		return
	}
	updated := make([]string, 0, len(req))
	for key, value := range req {
		if err := s.Settings.Set(r.Context(), key, value); err != nil {
			writeServiceErr(w, r, err)
			return
		}
		updated = append(updated, key)
	}
	s.audit(r, "settings.updated", "settings", map[string]any{"keys": updated})
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

// --- Webhooks ---

func (s *Server) handleWebhookList(w http.ResponseWriter, r *http.Request) {
	eps, err := s.Webhooks.List(r.Context())
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	items := make([]webhookEndpointDTO, 0, len(eps))
	for _, ep := range eps {
		items = append(items, webhookEndpointDTO{
			ID: ep.ID, URL: ep.URL, Enabled: ep.Enabled, Events: ep.Events,
			CreatedAt: jsonTime(&ep.CreatedAt), Stats: ep.Stats,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ep, secret, err := s.Webhooks.Create(r.Context(), req.URL, req.Events, req.Secret)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "webhook.created", ep.ID, map[string]any{"url_host": hostOf(ep.URL)})
	resp := map[string]any{"id": ep.ID, "url": ep.URL, "enabled": ep.Enabled, "events": ep.Events}
	if secret != "" {
		// Generated secret: returned exactly once, never stored in plaintext
		// and never logged (webhooks.md).
		resp["secret"] = secret
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleWebhookGet(w http.ResponseWriter, r *http.Request) {
	ep, err := s.Webhooks.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, webhookEndpointDTO{
		ID: ep.ID, URL: ep.URL, Enabled: ep.Enabled, Events: ep.Events,
		CreatedAt: jsonTime(&ep.CreatedAt),
	})
}

func (s *Server) handleWebhookUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		URL     *string  `json:"url"`
		Events  []string `json:"events"`
		Enabled *bool    `json:"enabled"`
		Secret  *string  `json:"secret"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ep, secret, err := s.Webhooks.Update(r.Context(), id, webhook.EndpointUpdate{
		URL: req.URL, Events: req.Events, Enabled: req.Enabled, Secret: req.Secret,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "webhook.updated", id, nil)
	resp := map[string]any{"id": ep.ID, "url": ep.URL, "enabled": ep.Enabled, "events": ep.Events}
	if secret != "" {
		resp["secret"] = secret
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleWebhookDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if err := s.Webhooks.Delete(r.Context(), id); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "webhook.deleted", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleWebhookRedeliver(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		DeliveryID string `json:"delivery_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.Webhooks.Redeliver(r.Context(), id, req.DeliveryID); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "webhook.redelivered", id, map[string]any{"delivery_id": req.DeliveryID})
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

func hostOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// atoi/itoa2: small query-param helpers (strconv is fine but the aliases
// keep handler code terse and grep-able).
func atoi(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, invalidRequestErr("empty integer")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, invalidRequestErr("not an integer: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func itoa2(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
