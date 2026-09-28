package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

// Appearance is a web-only preference, deliberately outside the public REST
// settings contract. A missing or retired saved ID resolves to the built-in
// preset; per-admin overrides never rewrite panel defaults or language.
type appearanceSelection struct {
	Preset string
	Mode   string
}

type appearanceChoice struct {
	ID, Name string
}

type appearancePageData struct {
	Choices        []appearanceChoice
	Effective      appearanceSelection
	EffectiveName  string
	PanelDefault   appearanceSelection
	Personal       string
	IsOwner        bool
	ModeIsPersonal bool
}

func validAppearanceMode(mode string) bool {
	return mode == "light" || mode == "dark" || mode == "system"
}

func (s *Server) panelAppearance(ctx context.Context) appearanceSelection {
	fallback := appearanceSelection{Preset: s.visualPresets.BuiltIn.ID, Mode: "light"}
	if s.DB == nil {
		return fallback
	}
	var saved appearanceSelection
	if err := s.DB.QueryRowContext(ctx, `SELECT preset_id, mode FROM appearance_defaults WHERE id = 1`).Scan(&saved.Preset, &saved.Mode); err != nil {
		return fallback
	}
	saved.Preset = s.visualPresets.resolve(saved.Preset)
	if !validAppearanceMode(saved.Mode) {
		saved.Mode = fallback.Mode
	}
	return saved
}

func themeCookieChoice(r *http.Request) string {
	if c, err := r.Cookie(themeCookie); err == nil && validAppearanceMode(c.Value) {
		return c.Value
	}
	return ""
}

func (s *Server) appearanceFor(r *http.Request) appearanceSelection {
	selection := s.panelAppearance(r.Context())
	if a := adminFrom(r); !strings.HasPrefix(r.URL.Path, "/sub/") && a != nil && a.AppearancePreset != "" && s.visualPresets.known(a.AppearancePreset) {
		selection.Preset = a.AppearancePreset
	}
	if personalMode := themeCookieChoice(r); personalMode != "" {
		selection.Mode = personalMode
	}
	return selection
}

func (s *Server) appearancePageData(r *http.Request) appearancePageData {
	choices := make([]appearanceChoice, 0, len(s.visualPresets.Presets)+1)
	choices = append(choices, appearanceChoice{ID: s.visualPresets.BuiltIn.ID, Name: s.visualPresets.BuiltIn.Name})
	for _, p := range s.visualPresets.Presets {
		choices = append(choices, appearanceChoice{ID: p.ID, Name: p.Name})
	}
	a := adminFrom(r)
	data := appearancePageData{
		Choices: choices, Effective: s.appearanceFor(r), PanelDefault: s.panelAppearance(r.Context()),
		ModeIsPersonal: themeCookieChoice(r) != "", IsOwner: a != nil && a.Role == auth.RoleOwner,
	}
	if a != nil && s.visualPresets.known(a.AppearancePreset) {
		data.Personal = a.AppearancePreset
	}
	data.EffectiveName = s.visualPresets.name(data.Effective.Preset)
	return data
}

func (s *Server) handleAppearancePage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "appearance", "app", s.appearancePageData(r))
}

func (s *Server) handleAppearanceMe(w http.ResponseWriter, r *http.Request) {
	preset := strings.TrimSpace(r.PostFormValue("preset"))
	if !s.visualPresets.known(preset) {
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
	if err := s.Admins.SetAppearancePreset(r.Context(), adminFrom(r).ID, ""); err != nil {
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
	if !s.visualPresets.known(preset) || !validAppearanceMode(mode) || r.PostFormValue("confirm") != "1" {
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
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE appearance_defaults SET preset_id = ?, mode = 'light' WHERE id = 1`, s.visualPresets.BuiltIn.ID); err != nil {
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
