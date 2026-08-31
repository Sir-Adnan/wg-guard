package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// settingsData feeds the panel settings page: the operator-facing, non-secret
// knobs only. Phase 6 material (admins, tokens, webhooks, backups, audit) is
// deliberately out of scope here.
type settingsData struct {
	Error string // localized message
	Field string // field that failed (best effort)

	QuotaPresets     string
	DurPresets       string
	DefaultQuotaGB   int
	DefaultDurMonths int
	DefaultDeviceLim int
	DefaultIfaceID   string
	SubBaseURL       string
	FilenamePrefix   string
	FilenameSuffix   string

	Ifaces []*ifaceRef
}

// handleSettingsPage renders the settings form from the live registry.
func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	d := s.loadSettingsData(r)
	_ = s.render(w, r, "settings", "app", d)
}

func (s *Server) loadSettingsData(r *http.Request) settingsData {
	ctx := r.Context()
	d := settingsData{Ifaces: s.ifacesForForm(r)}
	if v, err := s.Settings.GetStringList(ctx, "users.quota_presets_gb"); err == nil {
		d.QuotaPresets = strings.Join(v, ", ")
	}
	if v, err := s.Settings.GetStringList(ctx, "users.duration_presets_months"); err == nil {
		d.DurPresets = strings.Join(v, ", ")
	}
	d.DefaultQuotaGB, _ = s.Settings.GetInt(ctx, "users.default_quota_gb")
	d.DefaultDurMonths, _ = s.Settings.GetInt(ctx, "users.default_duration_months")
	d.DefaultDeviceLim, _ = s.Settings.GetInt(ctx, "users.default_device_limit")
	d.DefaultIfaceID, _ = s.Settings.GetString(ctx, "users.default_iface_id")
	d.SubBaseURL, _ = s.Settings.GetString(ctx, "subscription.base_url")
	d.FilenamePrefix, _ = s.Settings.GetString(ctx, "downloads.filename_prefix")
	d.FilenameSuffix, _ = s.Settings.GetString(ctx, "downloads.filename_suffix")
	return d
}

// handleSettingsSave applies the settings form. Every value goes through the
// registry validators; the first failure re-renders with a field error and
// the submitted values so nothing is silently dropped.
func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	setStrList := func(key, field string) ([]string, bool) {
		parts := strings.Split(r.PostFormValue(field), ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		if err := s.Settings.Set(r.Context(), key, out); err != nil {
			s.settingsSaveError(w, r, field, err)
			return nil, false
		}
		return out, true
	}
	setInt := func(key, field string) bool {
		raw := strings.TrimSpace(r.PostFormValue(field))
		n := 0
		if raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				s.settingsSaveError(w, r, field,
					domain.E(domain.CodeInvalidRequest, "not a number"))
				return false
			}
			n = v
		}
		if err := s.Settings.Set(r.Context(), key, n); err != nil {
			s.settingsSaveError(w, r, field, err)
			return false
		}
		return true
	}

	if _, ok := setStrList("users.quota_presets_gb", "quota_presets"); !ok {
		return
	}
	if _, ok := setStrList("users.duration_presets_months", "dur_presets"); !ok {
		return
	}
	if !setInt("users.default_quota_gb", "default_quota_gb") {
		return
	}
	if !setInt("users.default_duration_months", "default_dur_months") {
		return
	}
	if !setInt("users.default_device_limit", "default_device_lim") {
		return
	}
	ifaceID := strings.TrimSpace(r.PostFormValue("default_iface_id"))
	if err := s.Settings.Set(r.Context(), "users.default_iface_id", ifaceID); err != nil {
		s.settingsSaveError(w, r, "default_iface_id", err)
		return
	}
	base := strings.TrimSpace(r.PostFormValue("sub_base_url"))
	if err := s.Settings.Set(r.Context(), "subscription.base_url", base); err != nil {
		s.settingsSaveError(w, r, "sub_base_url", err)
		return
	}
	fnPrefix := strings.TrimSpace(r.PostFormValue("filename_prefix"))
	if err := s.Settings.Set(r.Context(), "downloads.filename_prefix", fnPrefix); err != nil {
		s.settingsSaveError(w, r, "filename_prefix", err)
		return
	}
	fnSuffix := strings.TrimSpace(r.PostFormValue("filename_suffix"))
	if err := s.Settings.Set(r.Context(), "downloads.filename_suffix", fnSuffix); err != nil {
		s.settingsSaveError(w, r, "filename_suffix", err)
		return
	}
	s.audit(r, "settings.updated", "", nil)
	s.redirectToast(w, r, "/settings", "settings.toast.saved")
}

// settingsSaveError re-renders the form with the submitted values and a
// localized error banner; the offending field gets the invalid marker.
func (s *Server) settingsSaveError(w http.ResponseWriter, r *http.Request, field string, err error) {
	switch domain.CodeOf(err) {
	case domain.CodeInvalidRequest, domain.CodeSettingUnknown, domain.CodeSettingInvalid:
		d := s.submittedSettingsData(r)
		d.Field = field
		d.Error = s.humanizeDomainError(r, err)
		_ = s.render(w, r, "settings", "app", d)
		return
	}
	s.logError(r, "settings save", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *Server) submittedSettingsData(r *http.Request) settingsData {
	d := settingsData{Ifaces: s.ifacesForForm(r)}
	d.QuotaPresets = r.PostFormValue("quota_presets")
	d.DurPresets = r.PostFormValue("dur_presets")
	d.DefaultQuotaGB, _ = strconv.Atoi(strings.TrimSpace(r.PostFormValue("default_quota_gb")))
	d.DefaultDurMonths, _ = strconv.Atoi(strings.TrimSpace(r.PostFormValue("default_dur_months")))
	d.DefaultDeviceLim, _ = strconv.Atoi(strings.TrimSpace(r.PostFormValue("default_device_lim")))
	d.DefaultIfaceID = strings.TrimSpace(r.PostFormValue("default_iface_id"))
	d.SubBaseURL = strings.TrimSpace(r.PostFormValue("sub_base_url"))
	d.FilenamePrefix = strings.TrimSpace(r.PostFormValue("filename_prefix"))
	d.FilenameSuffix = strings.TrimSpace(r.PostFormValue("filename_suffix"))
	return d
}
