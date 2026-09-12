package web

import (
	"net/http"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
)

type surfaceErrorData struct {
	Status                                     int
	MessageKey, HintKey, ActionHref, ActionKey string
	Fragment                                   bool
}

// Surface errors keep the HTTP failure and never render internal error values.
// Public capability routes use their own locale and never expose admin chrome.
func (s *Server) surfaceError(w http.ResponseWriter, r *http.Request, status int, key, layout string) {
	v := s.newView(r)
	d := surfaceErrorData{Status: status, MessageKey: key, HintKey: "error.request_hint", Fragment: isHX(r)}
	if layout == "sub" {
		v.Admin, v.CSRF = nil, ""
		v.Locale = i18n.Normalize(r.URL.Query().Get("lang"))
		d.HintKey = "error.public_hint"
	} else {
		if v.Admin != nil {
			layout, d.ActionHref, d.ActionKey = "app", "/", "common.home"
		} else {
			layout, d.ActionHref, d.ActionKey = "auth", "/login", "login.title"
			if lang := r.URL.Query().Get("lang"); lang == "fa" || lang == "en" {
				v.Locale = i18n.Normalize(lang)
			}
		}
		if status == http.StatusForbidden {
			d.HintKey = "error.access_hint"
		}
		if status >= 500 {
			d.HintKey = "error.server_hint"
		}
	}
	v.Dir, v.Data = v.Locale.Dir(), d
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	block := layout
	if d.Fragment {
		block = "surface_error"
	}
	if err := s.pages["error"].t.ExecuteTemplate(w, block, v); err != nil {
		s.logError(r, "error surface render", nil)
	}
}
