package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

// Subscription links: admin lifecycle handlers + the public customer-facing
// page. The token is a capability secret — it must never appear in logs
// (logError masks /sub/ paths) and public endpoints are rate-limited per IP.

// subBaseURL returns the public base for subscription links: the
// subscription.base_url setting when set, else this panel's own origin
// (scheme from TLS/proxy headers, host from the request).
func (s *Server) subBaseURL(r *http.Request) string {
	if base, err := s.Settings.GetString(r.Context(), "subscription.base_url"); err == nil && base != "" {
		return strings.TrimRight(base, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
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
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if _, err := s.Links.Regenerate(r.Context(), u.ID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.sub_regenerated", u.ID, nil)
	s.redirectToast(w, r, "/users/"+u.ID, "sub.toast.regenerated")
}

func (s *Server) handleSubRevoke(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if _, err := s.Links.SetRevoked(r.Context(), u.ID, true); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.sub_revoked", u.ID, nil)
	s.redirectToast(w, r, "/users/"+u.ID, "sub.toast.revoked")
}

func (s *Server) handleSubRestore(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if _, err := s.Links.SetRevoked(r.Context(), u.ID, false); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.sub_restored", u.ID, nil)
	s.redirectToast(w, r, "/users/"+u.ID, "sub.toast.restored")
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
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	data := subPageData{Lang: "fa"}
	if lang := r.URL.Query().Get("lang"); lang == "en" {
		data.Lang = "en"
	}
	if v, err := s.Settings.GetInt(ctx, "accounting.online_window_seconds"); err == nil && v > 0 {
		data.OnlineWindow = int64(v)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(data.OnlineWindow) * time.Second)
	if devs, err := s.Devices.ListForUser(ctx, u.ID); err == nil {
		data.Devices = make([]subDevice, 0, len(devs))
		for _, d := range devs {
			row := subDevice{D: d, Online: d.Enabled && d.LastHandshake != nil && d.LastHandshake.After(cutoff)}
			data.Devices = append(data.Devices, row)
		}
	}
	if p, err := s.Plans.Get(ctx, deref(u.PlanID)); err == nil && u.PlanID != nil {
		data.PlanName = p.Name
	}
	data.U = u
	data.Used = u.TrafficUsedRX + u.TrafficUsedTX
	data.Base = "/sub/" + url.PathEscape(r.PathValue("token"))
	s.renderSub(w, r, "sub", data)
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
		http.NotFound(w, r)
		return
	}
	png, err := clientconf.QR(text)
	if err != nil {
		s.writeQRError(w, r, err)
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
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
		http.NotFound(w, r)
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
			s.logError(r, "sub resolve", err)
		}
		http.NotFound(w, r)
		return "", false
	}
	return userID, true
}

// subRateLimited applies the fixed-window public rate limit (per IP).
func (s *Server) subRateLimited(w http.ResponseWriter, r *http.Request) bool {
	ip := clientIP(r)
	now := time.Now()
	if s.subRL.blocked(ip, now) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return true
	}
	s.subRL.fail(ip, now)
	s.subRL.trim(now)
	return false
}

// renderSub executes a customer-facing page inside the standalone sub layout
// with a ?lang= override (customers have no admin session/cookie).
func (s *Server) renderSub(w http.ResponseWriter, r *http.Request, page string, data any) {
	pt, ok := s.pages[page]
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	v := s.newView(r)
	v.Data = data
	v.Locale = i18n.Normalize(r.URL.Query().Get("lang"))
	v.Dir = v.Locale.Dir()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pt.t.ExecuteTemplate(w, "sub", v); err != nil && s.Log != nil {
		s.Log.Error("web: render sub", "page", page, "err", err)
	}
}

// subPageData feeds the public subscription page.
type subPageData struct {
	U            *user.User
	Used         int64  // RX+TX precomputed for the meter
	Base         string // /sub/{token} prefix for absolute asset URLs
	Lang         string
	PlanName     string
	Devices      []subDevice
	OnlineWindow int64
}

// subDevice is one device card on the public page.
type subDevice struct {
	D      *device.Device
	Online bool
}
