// Package web is the admin panel: server-rendered html/template pages with
// htmx partial swaps, session cookie auth, per-request CSRF tokens, and a
// strict content security policy. Handlers call domain services directly —
// the REST API is never in the request path (docs/architecture/
// project-structure.md: api/web → services).
//
// Security posture (docs/operations/security.md): session tokens are hashed
// at rest by internal/auth; the CSRF token is HMAC-derived from the session
// token so it needs no storage; scripts/styles are same-origin only (no
// inline script/style survive CSP); secrets never reach templates.
package web

import (
	"log/slog"
	"net/http"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

// Deps wires the services the panel renders. The same instances the REST
// API uses are passed in — one business layer, two surfaces.
type Deps struct {
	DB         *database.DB
	Sessions   *auth.SessionStore
	Admins     *admin.Service
	Settings   *settings.Registry
	Ring       *secrets.KeyRing
	Audit      *audit.Service
	Users      *user.Service
	Devices    *device.Service
	Plans      *plan.Service
	Ifaces     *iface.Service
	Accounting *accounting.Service
	Log        *slog.Logger

	// Reconciler runs after structural mutations (see api.Server).
	Reconciler accounting.Reconciler

	Version      string
	TLSMode      config.TLSMode
	NodeID       string
	ToolsVersion string
}

// Server is the admin panel.
type Server struct {
	Deps
	assets  assetSet
	pages   map[string]*pageTemplate
	loginRL *ipLimiter
}

// New builds the panel: parse templates once, hash assets once.
func New(d Deps) (*Server, error) {
	s := &Server{Deps: d, loginRL: newIPLimiter()}
	if err := s.initAssets(); err != nil {
		return nil, err
	}
	if err := s.initTemplates(); err != nil {
		return nil, err
	}
	return s, nil
}

// Handler returns the panel mux. It is mounted at "/" by serve; the REST
// API, health endpoints and /metrics keep their own root-level patterns.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/", s.handleAssets)

	// --- auth (public) ---
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("GET /onboarding", s.handleOnboardingPage)
	mux.HandleFunc("POST /onboarding", s.handleOnboardingSubmit)

	// --- preferences (session) ---
	mux.HandleFunc("POST /prefs/locale", s.requireAuth(s.handleLocaleSet))
	mux.HandleFunc("POST /logout", s.requireAuth(s.handleLogout))

	// --- app pages ---
	mux.HandleFunc("GET /{$}", s.requireAuth(s.handleDashboard))
	mux.HandleFunc("GET /dashboard", s.requireAuth(s.handleDashboard))

	h := http.Handler(mux)
	h = s.requireCSRF(h)
	h = s.sessionMiddleware(h)
	h = securityHeaders(h)
	h = bodyCap(h)
	return h
}
