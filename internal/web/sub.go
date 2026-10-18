package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/web/theme"
)

// Subscription links: admin lifecycle handlers + the public customer-facing
// page. The token is a capability secret — it must never appear in logs
// (logError masks /sub/ paths) and public endpoints are rate-limited per IP.

// subBaseURL returns the public base for subscription links: the
// subscription.base_url setting when set, else this panel's own origin
// (scheme from TLS/proxy headers, host from the request).
func (s *Server) subBaseURL(r *http.Request) string {
	if s.DomainPolicy != nil {
		if policy, err := s.DomainPolicy(); err == nil {
			return policy.SubscriptionOrigin()
		}
	}
	if base, err := s.Settings.GetString(r.Context(), "subscription.base_url"); err == nil && base != "" {
		return strings.TrimRight(base, "/")
	}
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// subURLFor builds the customer-facing URL for a link token.
func (s *Server) subURLFor(r *http.Request, token string) string {
	return s.subBaseURL(r) + "/sub/" + url.PathEscape(token)
}

// --- admin: lifecycle ---------------------------------------------------------

func (s *Server) handleSubCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if _, err := s.Links.Ensure(r.Context(), u.ID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.sub_created", u.ID, nil)
	s.redirectToast(w, r, "/users/"+u.ID, "sub.toast.created")
}

func (s *Server) handleSubRegenerate(w http.ResponseWriter, r *http.Request) {
	s.rotateSubscriptionAccess(w, r)
}

func (s *Server) handleSubRevoke(w http.ResponseWriter, r *http.Request) {
	s.rotateSubscriptionAccess(w, r)
}

// rotateSubscriptionAccess is the security meaning of "revoke": replace the
// public capability and every device key in one transaction, then reconcile
// the canonical runtime before reporting success. Old links, configs and QR
// payloads therefore cannot authenticate after a successful response.
func (s *Server) rotateSubscriptionAccess(w http.ResponseWriter, r *http.Request) {
	if !canOperate(r, auth.ScopeDevicesWrite) {
		s.redirectToast(w, r, "/", "common.denied")
		return
	}
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	_, rotatedCount, err := s.Links.RotateAccess(r.Context(), s.Devices, u.ID)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.sub_revoked", u.ID, map[string]any{"devices_rotated": rotatedCount})
	if err := s.runReconcile(r); err != nil {
		// The new database state is intentionally retained: rolling credentials
		// back after a requested revocation would silently make leaked configs
		// valid again. The normal reconciler can safely retry this desired state.
		s.actionFailed(w, r, err)
		return
	}
	s.redirectToast(w, r, "/users/"+u.ID, "sub.toast.replaced")
}

func (s *Server) handleSubRestore(w http.ResponseWriter, r *http.Request) {
	// Compatibility route for links revoked by older builds. Never revive the
	// old capability; issue wholly new access with the same fail-closed flow.
	s.rotateSubscriptionAccess(w, r)
}

// --- public: subscription page --------------------------------------------------

// handleSubPage serves the customer-facing subscription page. Any token
// problem (unknown, revoked, deleted user) is the same 404 — no oracle.
func (s *Server) handleSubPage(w http.ResponseWriter, r *http.Request) {
	if s.subRateLimited(w, r) {
		return
	}
	userID, ok := s.resolveSubToken(w, r)
	if !ok {
		return
	}
	u, err := s.Users.Get(r.Context(), userID)
	if err != nil {
		s.surfaceError(w, r, http.StatusNotFound, "publicsub.unavailable", "sub")
		return
	}
	ctx := r.Context()
	data := subPageData{Lang: "fa", OnlineWindow: 180, PlanKnown: u.TemplateID == nil}
	if lang := r.URL.Query().Get("lang"); lang == "en" {
		data.Lang = "en"
	}
	if v, err := s.Settings.GetInt(ctx, "accounting.online_window_seconds"); err == nil && v > 0 {
		data.OnlineWindow = int64(v)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(data.OnlineWindow) * time.Second)
	if devs, err := s.Devices.ListForUser(ctx, u.ID); err == nil {
		data.DevicesKnown = true
		data.Devices = make([]subDevice, 0, len(devs))
		for _, d := range devs {
			row := subDevice{D: safeDeviceViews([]*device.Device{d})[0], Online: d.Enabled && u.Status.PeerWanted() && d.LastHandshake != nil && d.LastHandshake.After(cutoff)}
			data.Devices = append(data.Devices, row)
		}
	}
	if p, err := s.Plans.Get(ctx, deref(u.TemplateID)); err == nil && u.TemplateID != nil {
		data.PlanName = p.Name
		data.PlanKnown = true
	}
	data.U = &subUserView{Username: u.Username, Status: u.Status,
		TrafficLimitBytes: u.TrafficLimitBytes, TrafficUsedRX: u.TrafficUsedRX, TrafficUsedTX: u.TrafficUsedTX,
		ExpiresAt: u.ExpiresAt, DurationSeconds: u.DurationSeconds}
	data.Used = u.TrafficUsedRX + u.TrafficUsedTX
	data.Base = "/sub/" + url.PathEscape(r.PathValue("token"))
	data.Layout = s.subLayout(ctx)
	data.Platforms = subPlatforms
	data.usageAndTime(time.Now())
	data.Attention = data.attention()
	setSubscriptionUserinfo(w, u.TrafficUsedRX, u.TrafficUsedTX, u.TrafficLimitBytes, u.ExpiresAt)
	s.renderSub(w, r, "sub", data)
}

// subLayouts are the public page designs in display order; the first is the
// default. A design is a registry entry here (and in the settings options), a
// hero template "sub_hero_<id>" and a stylesheet 40-pages/sub-<id>.css.
var subLayouts = []string{"ticket", "card", "minimal", "portal"}

// subLayout is the installation-wide design; an unknown stored value falls
// back to the default rather than failing the public page.
func (s *Server) subLayout(ctx context.Context) string {
	if layout, err := s.Settings.GetString(ctx, "subscription.layout"); err == nil && validSubLayout(layout) {
		return layout
	}
	return subLayouts[0]
}

func validSubLayout(layout string) bool {
	for _, l := range subLayouts {
		if l == layout {
			return true
		}
	}
	return false
}

// Thresholds for the public page's "running low" notice. They only warn: the
// enforced limits stay in accounting.
const (
	subLowDataPercent = 90 // quota used, inclusive
	subLowDays        = 3  // whole days left, inclusive
)

// attention names the renewal notice for a usable subscription that is about
// to run out: "data_low", "time_low", "both_low" or "" (nothing to say).
func (d *subPageData) attention() string {
	if d.U == nil || !d.U.Status.PeerWanted() {
		return ""
	}
	data := d.Remaining != nil && d.UsagePercent >= subLowDataPercent
	days := d.DaysLeft != nil && *d.DaysLeft <= subLowDays
	switch {
	case data && days:
		return "both_low"
	case data:
		return "data_low"
	case days:
		return "time_low"
	}
	return ""
}

// usageAndTime derives the meters: data used of the quota and time elapsed
// of the subscription period, plus whole days left (rounded up).
func (d *subPageData) usageAndTime(now time.Time) {
	if d.U == nil {
		return
	}
	if d.U.TrafficLimitBytes != nil && *d.U.TrafficLimitBytes > 0 {
		d.UsagePercent = int(min(100, d.Used*100 / *d.U.TrafficLimitBytes))
		left := *d.U.TrafficLimitBytes - d.Used
		d.Remaining = &left
		if left < 0 {
			zero := int64(0)
			d.Remaining = &zero
		}
	}
	if d.U.ExpiresAt == nil {
		return
	}
	remaining := d.U.ExpiresAt.Sub(now)
	days := int64(0)
	if remaining > 0 {
		days = int64((remaining + 24*time.Hour - time.Second) / (24 * time.Hour))
	}
	d.DaysLeft = &days
	if d.U.DurationSeconds != nil && *d.U.DurationSeconds > 0 {
		total := time.Duration(*d.U.DurationSeconds) * time.Second
		elapsed := total - max(remaining, 0)
		d.TimePercent = int(min(100, max(0, int64(elapsed*100/total))))
		d.TimeKnown = true
		d.TimeMeter = meterClass(int64(d.TimePercent), 100)
	}
}

// setSubscriptionUserinfo emits the de-facto subscription header understood by
// common VPN clients (upload/download/total bytes, expiry as Unix seconds).
// It carries only what the token holder can already see on the page.
func setSubscriptionUserinfo(w http.ResponseWriter, rx, tx int64, limit *int64, expires *time.Time) {
	total, expire := int64(0), int64(0)
	if limit != nil {
		total = *limit
	}
	if expires != nil {
		expire = expires.Unix()
	}
	// RX is client→server (upload), TX is server→client (download).
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", rx, tx, total, expire))
	w.Header().Set("Profile-Update-Interval", "24")
}

// subInfo is the machine-readable subscription (GET /sub/{token}/info).
// Field names are a public contract (docs/architecture/api.md).
type subInfo struct {
	Username      string          `json:"username"`
	Status        string          `json:"status"`
	Plan          string          `json:"plan,omitempty"`
	UsedBytes     int64           `json:"used_bytes"`
	UploadBytes   int64           `json:"upload_bytes"`
	DownloadBytes int64           `json:"download_bytes"`
	LimitBytes    *int64          `json:"limit_bytes"`
	ExpiresAt     *time.Time      `json:"expires_at"`
	DaysLeft      *int64          `json:"days_left"`
	Devices       []subInfoDevice `json:"devices"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type subInfoDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Address    string `json:"address"`
	Enabled    bool   `json:"enabled"`
	Online     bool   `json:"online"`
	ConfigURL  string `json:"config_url"`
	QRURL      string `json:"qr_url"`
	AmneziaURL string `json:"amneziavpn_url"`
}

// handleSubInfo serves the subscription as JSON for apps and bots that
// refresh it automatically. Same token gate and rate limit as the page.
func (s *Server) handleSubInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.subRateLimited(w, r) {
		return
	}
	userID, ok := s.resolveSubToken(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	u, err := s.Users.Get(ctx, userID)
	if err != nil {
		writeSubJSONError(w, http.StatusNotFound)
		return
	}
	base := s.subBaseURL(r) + "/sub/" + url.PathEscape(r.PathValue("token"))
	info := subInfo{Username: u.Username, Status: string(u.Status), UploadBytes: u.TrafficUsedRX, DownloadBytes: u.TrafficUsedTX,
		UsedBytes: u.TrafficUsedRX + u.TrafficUsedTX, LimitBytes: u.TrafficLimitBytes, ExpiresAt: u.ExpiresAt,
		Devices: []subInfoDevice{}, UpdatedAt: time.Now().UTC()}
	d := subPageData{U: &subUserView{TrafficLimitBytes: u.TrafficLimitBytes, ExpiresAt: u.ExpiresAt, DurationSeconds: u.DurationSeconds}, Used: info.UsedBytes}
	d.usageAndTime(time.Now())
	info.DaysLeft = d.DaysLeft
	if u.TemplateID != nil {
		if p, err := s.Plans.Get(ctx, *u.TemplateID); err == nil {
			info.Plan = p.Name
		}
	}
	window := int64(180)
	if v, err := s.Settings.GetInt(ctx, "accounting.online_window_seconds"); err == nil && v > 0 {
		window = int64(v)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(window) * time.Second)
	devs, err := s.Devices.ListForUser(ctx, u.ID)
	if err != nil {
		writeSubJSONError(w, http.StatusServiceUnavailable)
		return
	}
	for _, dv := range devs {
		prefix := base + "/devices/" + url.PathEscape(dv.ID)
		info.Devices = append(info.Devices, subInfoDevice{ID: dv.ID, Name: dv.Name, Address: dv.IPv4, Enabled: dv.Enabled,
			Online:    dv.Enabled && u.Status.PeerWanted() && dv.LastHandshake != nil && dv.LastHandshake.After(cutoff),
			ConfigURL: prefix + "/config", QRURL: prefix + "/qr", AmneziaURL: prefix + "/amneziavpn?format=text"})
	}
	setSubscriptionUserinfo(w, u.TrafficUsedRX, u.TrafficUsedTX, u.TrafficLimitBytes, u.ExpiresAt)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(info)
}

func writeSubJSONError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"unavailable"}` + "\n"))
}

// handleSubDeviceAmnezia hands one device to AmneziaVPN: a redirect to the
// vpn:// key (opens the app where the scheme is registered, e.g. Android), or
// the key as text with ?format=text for the copy button and for apps.
func (s *Server) handleSubDeviceAmnezia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.subRateLimited(w, r) {
		return
	}
	d, ok := s.subDevice(w, r)
	if !ok {
		return
	}
	text, err := s.ClientConf.Render(r.Context(), d.ID)
	if err != nil {
		s.surfaceError(w, r, http.StatusNotFound, "publicsub.download_unavailable", "sub")
		return
	}
	key, err := clientconf.AmneziaVPNKey(text)
	if err != nil {
		s.surfaceError(w, r, http.StatusNotFound, "publicsub.download_unavailable", "sub")
		return
	}
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(key))
		return
	}
	w.Header().Set("Location", key)
	w.WriteHeader(http.StatusSeeOther)
}

// handleSubDeviceQR streams one device's client config as a PNG, gated by
// the subscription token (device must belong to the link's user).
func (s *Server) handleSubDeviceQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.subRateLimited(w, r) {
		return
	}
	d, ok := s.subDevice(w, r)
	if !ok {
		return
	}
	text, err := s.ClientConf.Render(r.Context(), d.ID)
	if err != nil {
		s.surfaceError(w, r, http.StatusNotFound, "publicsub.download_unavailable", "sub")
		return
	}
	png, err := clientconf.QR(text)
	if err != nil {
		status := http.StatusNotFound
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			status = http.StatusBadRequest
		}
		s.surfaceError(w, r, status, "publicsub.download_unavailable", "sub")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `inline; filename="`+
		strings.TrimSuffix(s.configFilename(r, d), ".conf")+`.png"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// handleSubDeviceConfig streams one device's client .conf, same gating.
func (s *Server) handleSubDeviceConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.subRateLimited(w, r) {
		return
	}
	d, ok := s.subDevice(w, r)
	if !ok {
		return
	}
	text, err := s.ClientConf.Render(r.Context(), d.ID)
	if err != nil {
		s.surfaceError(w, r, http.StatusNotFound, "publicsub.download_unavailable", "sub")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+s.configFilename(r, d)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(text))
}

// subDevice resolves token+device for the public endpoints, enforcing that
// the device belongs to the link's user.
func (s *Server) subDevice(w http.ResponseWriter, r *http.Request) (*device.Device, bool) {
	userID, ok := s.resolveSubToken(w, r)
	if !ok {
		return nil, false
	}
	d, err := s.Devices.Get(r.Context(), r.PathValue("deviceID"))
	if err != nil || d.UserID != userID {
		s.surfaceError(w, r, http.StatusNotFound, "publicsub.unavailable", "sub")
		return nil, false
	}
	return d, true
}

// resolveSubToken maps the path token onto a live user, or writes 404.
func (s *Server) resolveSubToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, err := s.Links.Resolve(r.Context(), r.PathValue("token"))
	if err != nil {
		if domain.CodeOf(err) != domain.CodeUserNotFound {
			// Storage failure: log without the path (token is in it).
			s.logError(r, "sub resolve unavailable", nil)
		}
		s.surfaceError(w, r, http.StatusNotFound, "publicsub.unavailable", "sub")
		return "", false
	}
	return userID, true
}

// subRateLimited applies the fixed-window public rate limit (per IP).
func (s *Server) subRateLimited(w http.ResponseWriter, r *http.Request) bool {
	if !s.subRL.allow(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		s.surfaceError(w, r, http.StatusTooManyRequests, "publicsub.rate_limited", "sub")
		return true
	}
	return false
}

// renderSub executes a customer-facing page inside the standalone sub layout
// with a ?lang= override (customers have no admin session/cookie).
func (s *Server) renderSub(w http.ResponseWriter, r *http.Request, page string, data subPageData) {
	s.renderSubWith(w, r, page, data, nil)
}

// renderSubWith renders a public page; a non-nil appearance replaces the
// visitor's resolved preset and mode (the owner's unsaved preview).
func (s *Server) renderSubWith(w http.ResponseWriter, r *http.Request, page string, data subPageData, appearance *appearanceSelection) {
	pt, ok := s.pages[page]
	if !ok {
		s.surfaceError(w, r, http.StatusInternalServerError, "common.error_generic", "sub")
		return
	}
	v := s.newView(r)
	v.Admin = nil
	v.CSRF = ""
	if appearance != nil {
		v.Theme, v.Preset, v.UI = appearance.Mode, appearance.Preset, theme.UI{}
		if p := s.themes.Get(v.Preset); p != nil {
			v.UI = p.UI
		}
	}
	v.Data = data
	v.SubLayout = data.Layout
	v.Locale = i18n.Normalize(r.URL.Query().Get("lang"))
	v.Dir = v.Locale.Dir()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pt.t.ExecuteTemplate(w, "sub", v); err != nil {
		s.logError(r, "public subscription render failed", nil)
	}
}

// subPageData feeds the public subscription page.
type subPageData struct {
	U            *subUserView
	Used         int64  // RX+TX precomputed for the meter
	Base         string // /sub/{token} prefix for absolute asset URLs
	Layout       string // installation-wide public-page composition
	Lang         string
	PlanName     string
	PlanKnown    bool
	DevicesKnown bool
	Devices      []subDevice
	OnlineWindow int64
	Platforms    []subPlatform
	UsagePercent int    // 0–100 of the quota; 0 when unlimited
	Remaining    *int64 // bytes left of the quota (nil = unlimited)
	DaysLeft     *int64 // whole days until expiry (nil = no expiry)
	TimePercent  int    // 0–100 of the subscription period elapsed
	TimeKnown    bool
	TimeMeter    string // meter width/tone class for the time bar
	Attention    string // renewal notice key suffix; see attention
	Preview      bool   // sample data for the Appearance preview: actions are inert
}

// subDevice is one device card on the public page.
type subDevice struct {
	D      *deviceView
	Online bool
}

// Public templates receive only customer-facing account fields and keyless
// device summaries. Administrative notes and encrypted key carriers stay out.
type subUserView struct {
	Username                     string
	Status                       domain.UserStatus
	TrafficLimitBytes            *int64
	TrafficUsedRX, TrafficUsedTX int64
	ExpiresAt                    *time.Time
	DurationSeconds              *int64
}
