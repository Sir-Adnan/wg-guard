package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/web/theme"
)

// Appearance is a web-only preference, deliberately outside the public REST
// settings contract. A missing or retired saved ID resolves to the built-in
// preset; per-admin overrides never rewrite panel defaults or language.
type appearanceSelection struct {
	Preset string
	Mode   string
	Digits i18n.DigitStyle
}

type appearancePageData struct {
	Choices        []presetChoice
	Effective      appearanceSelection
	EffectiveName  string
	PanelDefault   appearanceSelection
	SubPreset      string // blank follows the panel default
	SubMode        string // blank follows the panel default
	SubLayouts     []string
	SubLayout      string // the saved public page design
	Personal       string
	IsOwner        bool
	ModeIsPersonal bool
	PersonalDigits string
}

// appearanceDefaults is the installation row: the panel default and the
// public subscription page's own (optional) preset and mode.
type appearanceDefaults struct {
	appearanceSelection
	SubPreset, SubMode string
}

func validAppearanceMode(mode string) bool {
	return mode == "light" || mode == "dark" || mode == "system"
}

func (s *Server) installationAppearance(ctx context.Context) appearanceDefaults {
	out := appearanceDefaults{appearanceSelection: appearanceSelection{Preset: theme.DefaultID, Mode: "light", Digits: i18n.LatinDigits}}
	if s.DB == nil {
		return out
	}
	var saved appearanceDefaults
	if err := s.DB.QueryRowContext(ctx, `SELECT preset_id, mode, digits, sub_preset_id, sub_mode FROM appearance_defaults WHERE id = 1`).
		Scan(&saved.Preset, &saved.Mode, &saved.Digits, &saved.SubPreset, &saved.SubMode); err != nil {
		return out
	}
	out.Preset = s.themes.Resolve(saved.Preset)
	if validAppearanceMode(saved.Mode) {
		out.Mode = saved.Mode
	}
	if saved.Digits.Valid() {
		out.Digits = saved.Digits
	}
	if s.themes.Known(saved.SubPreset) {
		out.SubPreset = saved.SubPreset
	}
	if validAppearanceMode(saved.SubMode) {
		out.SubMode = saved.SubMode
	}
	return out
}

func (s *Server) panelAppearance(ctx context.Context) appearanceSelection {
	return s.installationAppearance(ctx).appearanceSelection
}

func themeCookieChoice(r *http.Request) string {
	if c, err := r.Cookie(themeCookie); err == nil && validAppearanceMode(c.Value) {
		return c.Value
	}
	return ""
}

// subPresetCookie is a subscription visitor's own preset choice. It is a
// presentation preference only and honored solely for allowlisted IDs.
const subPresetCookie = "wg_sub_preset"

func (s *Server) appearanceFor(r *http.Request) appearanceSelection {
	defaults := s.installationAppearance(r.Context())
	selection := defaults.appearanceSelection
	if strings.HasPrefix(r.URL.Path, "/sub/") {
		if defaults.SubPreset != "" {
			selection.Preset = defaults.SubPreset
		}
		if defaults.SubMode != "" {
			selection.Mode = defaults.SubMode
		}
		if c, err := r.Cookie(subPresetCookie); err == nil && s.themes.Known(c.Value) {
			selection.Preset = c.Value
		}
		if personalMode := themeCookieChoice(r); personalMode != "" {
			selection.Mode = personalMode
		}
		return selection
	}
	a := adminFrom(r)
	if a != nil && a.AppearancePreset != "" && s.themes.Known(a.AppearancePreset) {
		selection.Preset = a.AppearancePreset
	}
	if personalMode := themeCookieChoice(r); personalMode != "" {
		selection.Mode = personalMode
	}
	if a != nil && i18n.DigitStyle(a.AppearanceDigits).Valid() {
		selection.Digits = i18n.DigitStyle(a.AppearanceDigits)
	}
	return selection
}

func (s *Server) appearancePageData(r *http.Request) appearancePageData {
	a := adminFrom(r)
	defaults := s.installationAppearance(r.Context())
	data := appearancePageData{
		Choices: s.presetChoices(), Effective: s.appearanceFor(r), PanelDefault: defaults.appearanceSelection,
		SubPreset: defaults.SubPreset, SubMode: defaults.SubMode,
		SubLayouts: subLayouts, SubLayout: s.subLayout(r.Context()),
		ModeIsPersonal: themeCookieChoice(r) != "", IsOwner: a != nil && a.Role == auth.RoleOwner,
	}
	if a != nil && s.themes.Known(a.AppearancePreset) {
		data.Personal = a.AppearancePreset
	}
	if a != nil {
		data.PersonalDigits = a.AppearanceDigits
	}
	data.EffectiveName = s.presetName(data.Effective.Preset)
	return data
}

// handleAppearanceSubscription sets the subscription page's design, preset
// and mode. Blank preset and mode follow the panel default; an absent design
// keeps the saved one. Everything is validated before anything is written.
func (s *Server) handleAppearanceSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireAppearanceOwner(w, r) {
		return
	}
	preset, mode := strings.TrimSpace(r.PostFormValue("sub_preset")), r.PostFormValue("sub_mode")
	layout := r.PostFormValue("sub_layout")
	if (preset != "" && !s.themes.Known(preset)) || (mode != "" && !validAppearanceMode(mode)) || (layout != "" && !validSubLayout(layout)) {
		s.surfaceError(w, r, http.StatusBadRequest, "appearance.invalid", "app")
		return
	}
	if layout != "" {
		if err := s.Settings.Set(r.Context(), "subscription.layout", layout); err != nil {
			s.actionFailed(w, r, err)
			return
		}
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE appearance_defaults SET sub_preset_id = ?, sub_mode = ? WHERE id = 1`, preset, mode); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "appearance.subscription_changed", "panel", map[string]any{"layout": layout, "preset": preset, "mode": mode})
	s.redirectToast(w, r, "/appearance#appearance-subscription", "appearance.sub.saved")
}

func (s *Server) handleAppearanceDigits(w http.ResponseWriter, r *http.Request) {
	if err := s.Admins.SetAppearanceDigits(r.Context(), adminFrom(r).ID, r.PostFormValue("digits")); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.redirectToast(w, r, "/appearance#appearance-numbers", "appearance.saved")
}

func (s *Server) handleAppearanceDefaultDigits(w http.ResponseWriter, r *http.Request) {
	if !s.requireAppearanceOwner(w, r) {
		return
	}
	digits := i18n.DigitStyle(r.PostFormValue("digits"))
	if !digits.Valid() {
		s.surfaceError(w, r, http.StatusBadRequest, "appearance.invalid", "app")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE appearance_defaults SET digits=? WHERE id=1`, digits); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "appearance.digits_changed", "panel", map[string]any{"digits": digits})
	s.redirectToast(w, r, "/appearance#appearance-numbers", "appearance.default_saved")
}

func (s *Server) handleAppearancePage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "appearance", "app", s.appearancePageData(r))
}

func (s *Server) handleAppearanceMe(w http.ResponseWriter, r *http.Request) {
	preset := strings.TrimSpace(r.PostFormValue("preset"))
	if !s.themes.Known(preset) {
		s.surfaceError(w, r, http.StatusBadRequest, "appearance.invalid", "app")
		return
	}
	if err := s.Admins.SetAppearancePreset(r.Context(), adminFrom(r).ID, preset); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.redirectToast(w, r, "/appearance", "appearance.saved")
}

func (s *Server) handleAppearanceMeReset(w http.ResponseWriter, r *http.Request) {
	if err := s.Admins.ResetAppearance(r.Context(), adminFrom(r).ID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	// The existing theme cookie is a personal mode override. Clearing it
	// restores the panel mode without touching the account's language.
	http.SetCookie(w, &http.Cookie{Name: themeCookie, Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode})
	s.redirectToast(w, r, "/appearance", "appearance.reset_personal_done")
}

func (s *Server) handleAppearanceDefault(w http.ResponseWriter, r *http.Request) {
	if !s.requireAppearanceOwner(w, r) {
		return
	}
	preset, mode := strings.TrimSpace(r.PostFormValue("preset")), r.PostFormValue("mode")
	if !s.themes.Known(preset) || !validAppearanceMode(mode) || r.PostFormValue("confirm") != "1" {
		s.surfaceError(w, r, http.StatusBadRequest, "appearance.invalid", "app")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE appearance_defaults SET preset_id = ?, mode = ? WHERE id = 1`, preset, mode); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "appearance.default_changed", "panel", map[string]any{"preset": preset, "mode": mode})
	s.redirectToast(w, r, "/appearance", "appearance.default_saved")
}

func (s *Server) handleAppearanceDefaultReset(w http.ResponseWriter, r *http.Request) {
	if !s.requireAppearanceOwner(w, r) {
		return
	}
	if r.PostFormValue("confirm") != "1" {
		s.surfaceError(w, r, http.StatusBadRequest, "appearance.invalid", "app")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE appearance_defaults SET preset_id = ?, mode = 'light', digits = 'latin', sub_preset_id = '', sub_mode = '' WHERE id = 1`, theme.DefaultID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	if err := s.Settings.Set(r.Context(), "subscription.layout", subLayouts[0]); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "appearance.default_reset", "panel", nil)
	s.redirectToast(w, r, "/appearance", "appearance.default_reset_done")
}

func (s *Server) requireAppearanceOwner(w http.ResponseWriter, r *http.Request) bool {
	a := adminFrom(r)
	if a == nil || a.Role != auth.RoleOwner {
		s.surfaceError(w, r, http.StatusForbidden, "common.denied", "app")
		return false
	}
	return true
}

// handleAppearanceSubscriptionPreview renders the public page with sample data
// so the owner can compare designs before saving. Query values (layout, preset,
// mode) preview unsaved choices; absent or unknown ones use the saved
// subscription defaults.
// Nothing about real users or devices is read.
func (s *Server) handleAppearanceSubscriptionPreview(w http.ResponseWriter, r *http.Request) {
	if !s.requireAppearanceOwner(w, r) {
		return
	}
	q := r.URL.Query()
	defaults := s.installationAppearance(r.Context())
	selection := defaults.appearanceSelection
	if defaults.SubPreset != "" {
		selection.Preset = defaults.SubPreset
	}
	if defaults.SubMode != "" {
		selection.Mode = defaults.SubMode
	}
	// A present but blank value previews "follow the panel default".
	if q.Has("preset") {
		if preset := q.Get("preset"); preset == "" {
			selection.Preset = defaults.Preset
		} else if s.themes.Known(preset) {
			selection.Preset = preset
		}
	}
	if q.Has("mode") {
		if mode := q.Get("mode"); mode == "" {
			selection.Mode = defaults.Mode
		} else if validAppearanceMode(mode) {
			selection.Mode = mode
		}
	}
	data := sampleSubPage(i18n.Normalize(q.Get("lang")), time.Now())
	data.Layout = s.subLayout(r.Context())
	if layout := q.Get("layout"); validSubLayout(layout) {
		data.Layout = layout
	}
	s.renderSubWith(w, r, "sub", data, &selection)
}

// sampleSubPage is a plausible, clearly fictional subscription: an active plan
// partly used, two devices (one online) and every setup guide.
func sampleSubPage(locale i18n.Locale, now time.Time) subPageData {
	const gb = int64(1_000_000_000)
	limit, duration := 50*gb, int64(30*24*3600)
	expires := now.Add(19 * 24 * time.Hour)
	data := subPageData{
		Lang: string(locale), Base: "#", Preview: true, DevicesKnown: true, PlanKnown: true, OnlineWindow: 180,
		PlanName:  i18n.T(locale, "appearance.sub.sample_plan"),
		Platforms: subPlatforms,
		U: &subUserView{Username: "sara", Status: domain.UserActive, TrafficLimitBytes: &limit,
			TrafficUsedRX: 3_100_000_000, TrafficUsedTX: 15_300_000_000, ExpiresAt: &expires, DurationSeconds: &duration},
		Devices: []subDevice{
			{D: &deviceView{ID: "sample-phone", Name: "iPhone", IPv4: "10.8.0.2/32", Enabled: true}, Online: true},
			{D: &deviceView{ID: "sample-laptop", Name: "Laptop", IPv4: "10.8.0.3/32", Enabled: true}},
		},
	}
	data.Used = data.U.TrafficUsedRX + data.U.TrafficUsedTX
	data.usageAndTime(now)
	data.Attention = data.attention()
	return data
}
