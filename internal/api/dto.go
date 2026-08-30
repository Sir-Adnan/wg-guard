package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// DTOs are the wire shapes of the API (a V1 compatibility contract —
// additive only). They deliberately mirror service types field by field:
// service structs never reach the wire directly, so encrypted key envelopes
// and internal fields cannot leak by accident.

func jsonTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func optI64(o *int64) *int64 { return o }

func optInt(o *int) *int { return o }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON parses a request body (already size-capped by middleware).
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body required")
		return false
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeErr(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// --- User ---

type userDTO struct {
	ID                 string         `json:"id"`
	Username           string         `json:"username"`
	DisplayName        string         `json:"display_name"`
	Note               string         `json:"note"`
	Tags               []string       `json:"tags"`
	Status             string         `json:"status"`
	DisableReason      *string        `json:"disable_reason"`
	TrafficLimitBytes  *int64         `json:"traffic_limit_bytes"`
	TrafficUsedRX      int64          `json:"traffic_used_rx"`
	TrafficUsedTX      int64          `json:"traffic_used_tx"`
	TrafficUsedTotal   int64          `json:"traffic_used_total"`
	SpeedLimitDownKbps *int           `json:"speed_limit_down_kbps"`
	SpeedLimitUpKbps   *int           `json:"speed_limit_up_kbps"`
	DeviceLimit        *int           `json:"device_limit"`
	PlanID             *string        `json:"plan_id"`
	InterfaceID        *string        `json:"interface_id"`
	StartPolicy        string         `json:"start_policy"`
	DurationSeconds    *int64         `json:"duration_seconds"`
	ActivatedAt        *string        `json:"activated_at"`
	ExpiresAt          *string        `json:"expires_at"`
	LastActivityAt     *string        `json:"last_activity_at"`
	Enabled            bool           `json:"enabled"`
	Metadata           map[string]any `json:"metadata"`
	Deleted            bool           `json:"deleted"`
	CreatedAt          *string        `json:"created_at"`
	UpdatedAt          *string        `json:"updated_at"`
}

func toUserDTO(u *user.User) userDTO {
	reason := func(r *user.User) *string {
		if r.DisableReason == nil {
			return nil
		}
		s := string(*r.DisableReason)
		return &s
	}(u)
	meta := u.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	return userDTO{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Note: u.Note,
		Tags: u.Tags, Status: string(u.Status), DisableReason: reason,
		TrafficLimitBytes: u.TrafficLimitBytes,
		TrafficUsedRX:     u.TrafficUsedRX, TrafficUsedTX: u.TrafficUsedTX,
		TrafficUsedTotal:   u.TrafficUsedRX + u.TrafficUsedTX,
		SpeedLimitDownKbps: u.SpeedLimitDownKbps, SpeedLimitUpKbps: u.SpeedLimitUpKbps,
		DeviceLimit: u.DeviceLimit, PlanID: u.PlanID, InterfaceID: u.InterfaceID,
		StartPolicy: string(u.StartPolicy), DurationSeconds: u.DurationSeconds,
		ActivatedAt: jsonTime(u.ActivatedAt), ExpiresAt: jsonTime(u.ExpiresAt),
		LastActivityAt: jsonTime(u.LastActivityAt),
		Enabled:        u.Enabled, Metadata: meta, Deleted: u.DeletedAt != nil,
		CreatedAt: jsonTime(&u.CreatedAt), UpdatedAt: jsonTime(&u.UpdatedAt),
	}
}

// userCreateReq is the POST /users body. Limit fields are domain.Opt values
// (tri-state): absent = default/unlimited, null = clear to unlimited, value =
// set. domain.Opt* carries the UnmarshalJSON contract.
type userCreateReq struct {
	Username           string           `json:"username"`
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
	Enabled            *bool            `json:"enabled"`
	Metadata           map[string]any   `json:"metadata"`
}

// userPatchReq is the PATCH /users/{id} body (username is immutable).
type userPatchReq struct {
	DisplayName        *string          `json:"display_name"`
	Note               *string          `json:"note"`
	Tags               []string         `json:"tags"`
	TrafficLimitBytes  domain.OptInt64  `json:"traffic_limit_bytes"`
	SpeedLimitDownKbps domain.OptInt    `json:"speed_limit_down_kbps"`
	SpeedLimitUpKbps   domain.OptInt    `json:"speed_limit_up_kbps"`
	DeviceLimit        domain.OptInt    `json:"device_limit"`
	PlanID             domain.OptString `json:"plan_id"`
	InterfaceID        domain.OptString `json:"interface_id"`
	DurationSeconds    *int64           `json:"duration_seconds"`
	Enabled            *bool            `json:"enabled"`
	Metadata           map[string]any   `json:"metadata"`
}

// --- Device ---

type deviceDTO struct {
	ID            string  `json:"id"`
	UserID        string  `json:"user_id"`
	InterfaceID   string  `json:"interface_id"`
	Name          string  `json:"name"`
	IPv4          string  `json:"ipv4_address"`
	PublicKey     string  `json:"public_key"`
	Enabled       bool    `json:"enabled"`
	LastHandshake *string `json:"last_handshake_at"`
	LastEndpoint  string  `json:"last_endpoint"`
	RXBytes       uint64  `json:"rx_bytes"`
	TXBytes       uint64  `json:"tx_bytes"`
	CreatedAt     *string `json:"created_at"`
	UpdatedAt     *string `json:"updated_at"`
}

func toDeviceDTO(d *device.Device) deviceDTO {
	return deviceDTO{
		ID: d.ID, UserID: d.UserID, InterfaceID: d.InterfaceID, Name: d.Name,
		IPv4: d.IPv4, PublicKey: d.PublicKey, Enabled: d.Enabled,
		LastHandshake: jsonTime(d.LastHandshake), LastEndpoint: d.LastEndpoint,
		RXBytes: d.RXBytes, TXBytes: d.TXBytes,
		CreatedAt: jsonTime(&d.CreatedAt), UpdatedAt: jsonTime(&d.UpdatedAt),
	}
}

// --- Plan ---

type planDTO struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	TrafficLimitBytes  *int64  `json:"traffic_limit_bytes"`
	DurationSeconds    *int64  `json:"duration_seconds"`
	StartPolicy        string  `json:"start_policy"`
	DeviceLimit        *int    `json:"device_limit"`
	SpeedLimitDownKbps *int    `json:"speed_limit_down_kbps"`
	SpeedLimitUpKbps   *int    `json:"speed_limit_up_kbps"`
	InterfaceID        *string `json:"interface_id"`
	Enabled            bool    `json:"enabled"`
	CreatedAt          *string `json:"created_at"`
	UpdatedAt          *string `json:"updated_at"`
}

func toPlanDTO(p *plan.Plan) planDTO {
	return planDTO{
		ID: p.ID, Name: p.Name, TrafficLimitBytes: p.TrafficLimitBytes,
		DurationSeconds: p.DurationSeconds, StartPolicy: string(p.StartPolicy),
		DeviceLimit: p.DeviceLimit, SpeedLimitDownKbps: p.SpeedLimitDownKbps,
		SpeedLimitUpKbps: p.SpeedLimitUpKbps, InterfaceID: p.InterfaceID,
		Enabled: p.Enabled, CreatedAt: jsonTime(&p.CreatedAt), UpdatedAt: jsonTime(&p.UpdatedAt),
	}
}

// --- Interface ---

type ifaceDTO struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	ListenPort       int               `json:"listen_port"`
	Subnet           string            `json:"ipv4_subnet"`
	MTU              int               `json:"mtu"`
	PublicKey        string            `json:"public_key"`
	Obfuscation      iface.Obfuscation `json:"obfuscation"`
	Preset           string            `json:"preset"`
	Enabled          bool              `json:"enabled"`
	BackendMode      string            `json:"backend_mode"`
	EndpointOverride string            `json:"endpoint_override"`
	CreatedAt        *string           `json:"created_at"`
	UpdatedAt        *string           `json:"updated_at"`
}

func toIfaceDTO(i *iface.Interface) ifaceDTO {
	return ifaceDTO{
		ID: i.ID, Name: i.Name, ListenPort: i.ListenPort, Subnet: i.Subnet, MTU: i.MTU,
		PublicKey: i.PublicKey, Obfuscation: i.Obfuscation, Preset: i.Preset,
		Enabled: i.Enabled, BackendMode: string(i.BackendMode),
		EndpointOverride: i.EndpointOverride,
		CreatedAt:        jsonTime(&i.CreatedAt), UpdatedAt: jsonTime(&i.UpdatedAt),
	}
}

// --- Webhook endpoint ---

type webhookEndpointDTO struct {
	ID        string        `json:"id"`
	URL       string        `json:"url"`
	Enabled   bool          `json:"enabled"`
	Events    []string      `json:"events"`
	CreatedAt *string       `json:"created_at"`
	Stats     webhook.Stats `json:"stats"`
}
