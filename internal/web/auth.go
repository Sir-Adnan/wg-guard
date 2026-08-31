package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
)

// loginData carries the login page state (banner + prefilled next target).
type loginData struct {
	Error    string // i18n key
	Created  bool   // just finished onboarding
	Expired  bool   // redirected here after a session expiry
	Next     string
	Endpoint string // node endpoint hint, "" during onboarding-first runs
}

// handleLoginPage renders the sign-in form. Language selection happens via
// ?lang= (GET → set cookie → redirect clean), which keeps the anonymous
// surface free of state-changing endpoints.
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("lang") != "" {
		if l := i18n.Normalize(r.URL.Query().Get("lang")); r.URL.Query().Get("lang") == string(l) {
			http.SetCookie(w, &http.Cookie{
				Name: localeCookie, Value: string(l), Path: "/",
				MaxAge: 31536000, SameSite: http.SameSiteLaxMode,
			})
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if adminFrom(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	has, err := s.Admins.HasOwner(r.Context())
	if err == nil && !has {
		http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	data := loginData{
		Created: q.Get("created") == "1",
		Expired: q.Get("expired") == "1",
		Next:    safeNext(q.Get("next")),
	}
	switch q.Get("e") {
	case "rate":
		data.Error = "login.rate_limited"
	default:
		data.Error = ""
	}
	_ = s.render(w, r, "login", "auth", data)
}

// handleLoginSubmit verifies credentials and mints the session cookie.
func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))
	ip := clientIP(r)
	now := time.Now()

	if s.loginRL.blocked(ip, now) {
		s.loginRL.fail(ip, now)
		s.audit(r, "auth.login_failed", username, nil)
		w.Header().Set("X-WG-Error", "login.rate_limited")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = s.render(w, r, "login", "auth", loginData{Error: "login.rate_limited", Next: next})
		return
	}
	if username == "" || password == "" {
		s.renderLoginError(w, r, "login.invalid", next)
		return
	}

	a, err := s.Admins.Authenticate(r.Context(), username, password)
	if err != nil {
		s.loginRL.fail(ip, now)
		s.renderLoginError(w, r, "login.invalid", next)
		return
	}

	s.auditLogin(r, a.ID, a.Username, nil)
	s.loginAfter(w, r, a.ID, next)
}

// loginAfter mints the session, sets the cookie and redirects.
func (s *Server) loginAfter(w http.ResponseWriter, r *http.Request, adminID, next string) {
	token, expires, err := s.Sessions.Create(r.Context(), adminID, clientIP(r))
	if err != nil {
		s.logError(r, "session create", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.setSessionCookie(w, token, expires)
	target := next
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) renderLoginError(w http.ResponseWriter, r *http.Request, key, next string) {
	s.audit(r, "auth.login_failed", r.PostFormValue("username"), nil)
	w.WriteHeader(http.StatusUnauthorized)
	_ = s.render(w, r, "login", "auth", loginData{Error: key, Next: next})
}

// handleLogout revokes the session and clears the cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok, _ := r.Context().Value(ctxSession).(string); tok != "" {
		_ = s.Sessions.Revoke(r.Context(), tok)
	}
	if a := adminFrom(r); a != nil {
		s.audit(r, "auth.logout", a.Username, nil)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- onboarding ---------------------------------------------------------------

type onboardData struct {
	Error        string // raw message (already localized by the handler)
	Username     string
	Endpoint     string
	MinPass      int
	PasswordHint string // localized hint with the real minimum filled in
	Completed    bool
}

func (s *Server) handleOnboardingPage(w http.ResponseWriter, r *http.Request) {
	if q := r.URL.Query().Get("lang"); q != "" {
		if l := i18n.Normalize(q); q == string(l) {
			http.SetCookie(w, &http.Cookie{
				Name: localeCookie, Value: string(l), Path: "/",
				MaxAge: 31536000, SameSite: http.SameSiteLaxMode,
			})
		}
		http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
		return
	}
	has, err := s.Admins.HasOwner(r.Context())
	if err == nil && has {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if adminFrom(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = s.render(w, r, "onboarding", "auth", onboardData{
		MinPass:      auth.MinPasswordLength,
		PasswordHint: s.t(r, "onboard.password_hint", auth.MinPasswordLength),
	})
}

// handleOnboardingSubmit creates the owner account (once) and signs in.
func (s *Server) handleOnboardingSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip := clientIP(r)
	now := time.Now()
	view := func(data onboardData, status int) {
		if data.Error != "" {
			s.audit(r, "onboarding.rejected", "", nil)
		}
		w.WriteHeader(status)
		_ = s.render(w, r, "onboarding", "auth", data)
	}

	if s.loginRL.blocked(ip, now) {
		view(onboardData{
			Error:        s.t(r, "login.rate_limited"),
			MinPass:      auth.MinPasswordLength,
			PasswordHint: s.t(r, "onboard.password_hint", auth.MinPasswordLength),
		}, http.StatusTooManyRequests)
		return
	}

	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("password_confirm")
	endpoint := strings.TrimSpace(r.PostFormValue("endpoint"))

	data := onboardData{
		Username:     username,
		Endpoint:     endpoint,
		MinPass:      auth.MinPasswordLength,
		PasswordHint: s.t(r, "onboard.password_hint", auth.MinPasswordLength),
	}

	if endpoint != "" {
		if err := s.Settings.Validate("node.endpoint", endpoint); err != nil {
			data.Error = s.t(r, "common.error_validation")
			view(data, http.StatusUnprocessableEntity)
			return
		}
	}
	if len(password) < auth.MinPasswordLength {
		data.Error = s.t(r, "onboard.password_short")
		view(data, http.StatusUnprocessableEntity)
		return
	}
	if password != confirm {
		data.Error = s.t(r, "onboard.password_mismatch")
		view(data, http.StatusUnprocessableEntity)
		return
	}

	created, err := s.Admins.BootstrapOwner(r.Context(), username, password)
	if err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			// username shape violation — show as validation error
			data.Error = s.t(r, "common.error_validation")
			view(data, http.StatusUnprocessableEntity)
			return
		}
		s.logError(r, "onboarding owner create", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !created {
		data.Error = s.t(r, "onboard.already_done")
		view(data, http.StatusConflict)
		return
	}

	if endpoint != "" {
		if err := s.Settings.Set(r.Context(), "node.endpoint", endpoint); err != nil {
			// Owner exists; endpoint stays default and is fixable in Settings.
			s.logError(r, "onboarding endpoint set", err)
		}
	}
	s.audit(r, "onboarding.owner_created", username, nil)

	a, err := s.Admins.Authenticate(r.Context(), username, password)
	if err != nil {
		// Account exists; send to login instead of failing opaque.
		http.Redirect(w, r, "/login?created=1", http.StatusSeeOther)
		return
	}
	s.auditLogin(r, a.ID, a.Username, map[string]any{"onboarding": true})
	s.loginAfter(w, r, a.ID, "/")
}

// handleLocaleSet stores the signed-in admin's language preference.
func (s *Server) handleLocaleSet(w http.ResponseWriter, r *http.Request) {
	a := adminFrom(r)
	if a == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	l := r.PostFormValue("locale")
	if err := s.Admins.SetLocale(r.Context(), a.ID, l); err != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: localeCookie, Value: l, Path: "/", MaxAge: 31536000, SameSite: http.SameSiteLaxMode,
	})
	back := safeNext(r.PostFormValue("next"))
	if back == "" {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// --- shared handler helpers ----------------------------------------------------

func (s *Server) audit(r *http.Request, action, target string, meta map[string]any) {
	if s.Audit == nil {
		return
	}
	actorType, actorID := audit.ActorSystem, ""
	if a := adminFrom(r); a != nil {
		actorType, actorID = audit.ActorAdmin, a.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Entry{
		ActorType: actorType, ActorID: actorID,
		Action: action, Target: target, SourceIP: clientIP(r), Metadata: meta,
	})
}

// auditLogin records a login/logout event for an account resolved before a
// session exists (login, onboarding) — the actor is known from the service.
func (s *Server) auditLogin(r *http.Request, adminID, username string, meta map[string]any) {
	if s.Audit == nil {
		return
	}
	_ = s.Audit.Record(r.Context(), audit.Entry{
		ActorType: audit.ActorAdmin, ActorID: adminID,
		Action: "auth.login", Target: username, SourceIP: clientIP(r), Metadata: meta,
	})
}

func (s *Server) logError(r *http.Request, what string, err error) {
	if s.Log != nil && !errors.Is(err, http.ErrAbortHandler) {
		// Subscription links carry a capability token in the path — the
		// path is masked before it reaches any log sink.
		path := r.URL.Path
		if strings.HasPrefix(path, "/sub/") {
			path = "/sub/{token}"
		}
		s.Log.Error("web: "+what, "err", err, "path", path)
	}
}

// t translates for the request's effective locale (anonymous: cookie).
func (s *Server) t(r *http.Request, key string, args ...any) string {
	return i18n.T(s.localeFor(r), key, args...)
}
