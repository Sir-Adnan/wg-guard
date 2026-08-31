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
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/hoststats"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
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

	// ClientConf renders client configs + QR (shared with the REST API).
	ClientConf *clientconf.Renderer

	// Links serves the per-user subscription links (public /sub/ surface).
	Links *subscription.Service

	// Host reads host metrics for the dashboard (nil on platforms without
	// support — the card is hidden). Wired from serve.
	Host *hoststats.Reader

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
	subRL   *ipLimiter // public /sub/ surface: request-rate window per IP
}

// New builds the panel: parse templates once, hash assets once.
func New(d Deps) (*Server, error) {
	s := &Server{
		Deps:    d,
		loginRL: newIPLimiter(),
		subRL:   newRateLimiter(time.Minute, 60),
	}
	if s.ClientConf == nil {
		s.ClientConf = &clientconf.Renderer{
			Devices: d.Devices, Ifaces: d.Ifaces, Settings: d.Settings,
		}
	}
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

	// --- public subscription pages (token-gated, rate-limited) ---
	mux.HandleFunc("GET /sub/{token}", s.handleSubPage)
	mux.HandleFunc("GET /sub/{token}/devices/{deviceID}/qr", s.handleSubDeviceQR)
	mux.HandleFunc("GET /sub/{token}/devices/{deviceID}/config", s.handleSubDeviceConfig)

	// --- app pages ---
	mux.HandleFunc("GET /{$}", s.requireAuth(s.handleDashboard))
	mux.HandleFunc("GET /dashboard", s.requireAuth(s.handleDashboard))
	mux.HandleFunc("GET /dashboard/live", s.requireAuth(s.handleDashboardLive))
	mux.HandleFunc("GET /dashboard/chart", s.requireAuth(s.handleDashboardChart))

	// --- users ---
	mux.HandleFunc("GET /users", s.requireAuth(s.handleUserList))
	mux.HandleFunc("GET /users/new", s.requireAuth(s.handleUserNew))
	mux.HandleFunc("POST /users", s.requireAuth(s.handleUserCreate))
	mux.HandleFunc("POST /users/bulk", s.requireAuth(s.handleBulkCreate))
	mux.HandleFunc("POST /users/bulk-action", s.requireAuth(s.handleBulkAction))
	mux.HandleFunc("GET /users/{id}", s.requireAuth(s.handleUserDetail))
	mux.HandleFunc("GET /users/{id}/edit", s.requireAuth(s.handleUserEditPage))
	mux.HandleFunc("POST /users/{id}/edit", s.requireAuth(s.handleUserUpdate))
	mux.HandleFunc("POST /users/{id}/enable", s.requireAuth(s.handleUserEnable))
	mux.HandleFunc("POST /users/{id}/disable", s.requireAuth(s.handleUserDisable))
	mux.HandleFunc("POST /users/{id}/delete", s.requireAuth(s.handleUserDelete))
	mux.HandleFunc("POST /users/{id}/restore", s.requireAuth(s.handleUserRestore))
	mux.HandleFunc("POST /users/{id}/renew", s.requireAuth(s.handleUserRenew))
	mux.HandleFunc("POST /users/{id}/traffic/add", s.requireAuth(s.handleUserTrafficAdd))
	mux.HandleFunc("POST /users/{id}/traffic/reset", s.requireAuth(s.handleUserTrafficReset))
	mux.HandleFunc("POST /users/{id}/sub/create", s.requireAuth(s.handleSubCreate))
	mux.HandleFunc("POST /users/{id}/sub/regenerate", s.requireAuth(s.handleSubRegenerate))
	mux.HandleFunc("POST /users/{id}/sub/revoke", s.requireAuth(s.handleSubRevoke))
	mux.HandleFunc("POST /users/{id}/sub/restore", s.requireAuth(s.handleSubRestore))
	mux.HandleFunc("POST /users/{id}/devices", s.requireAuth(s.handleDeviceCreate))

	// --- devices ---
	mux.HandleFunc("POST /devices/{id}/enable", s.requireAuth(s.handleDeviceEnable))
	mux.HandleFunc("POST /devices/{id}/disable", s.requireAuth(s.handleDeviceDisable))
	mux.HandleFunc("POST /devices/{id}/regenerate", s.requireAuth(s.handleDeviceRegenerate))
	mux.HandleFunc("POST /devices/{id}/delete", s.requireAuth(s.handleDeviceDelete))
	mux.HandleFunc("GET /devices/{id}/config", s.requireAuth(s.handleDeviceConfig))
	mux.HandleFunc("GET /devices/{id}/qr", s.requireAuth(s.handleDeviceQR))

	// --- plans ---
	mux.HandleFunc("GET /plans", s.requireAuth(s.handlePlanList))
	mux.HandleFunc("GET /plans/new", s.requireAuth(s.handlePlanNew))
	mux.HandleFunc("POST /plans", s.requireAuth(s.handlePlanCreate))
	mux.HandleFunc("GET /plans/{id}/edit", s.requireAuth(s.handlePlanEditPage))
	mux.HandleFunc("POST /plans/{id}/edit", s.requireAuth(s.handlePlanUpdate))
	mux.HandleFunc("POST /plans/{id}/enable", s.requireAuth(s.handlePlanEnable))
	mux.HandleFunc("POST /plans/{id}/disable", s.requireAuth(s.handlePlanDisable))
	mux.HandleFunc("POST /plans/{id}/delete", s.requireAuth(s.handlePlanDelete))

	// --- interfaces ---
	mux.HandleFunc("GET /interfaces", s.requireAuth(s.handleIfaceList))
	mux.HandleFunc("GET /interfaces/new", s.requireAuth(s.handleIfaceNew))
	mux.HandleFunc("POST /interfaces", s.requireAuth(s.handleIfaceCreate))
	mux.HandleFunc("GET /interfaces/{id}/edit", s.requireAuth(s.handleIfaceEditPage))
	mux.HandleFunc("POST /interfaces/{id}/edit", s.requireAuth(s.handleIfaceUpdate))
	mux.HandleFunc("POST /interfaces/{id}/enable", s.requireAuth(s.handleIfaceEnable))
	mux.HandleFunc("POST /interfaces/{id}/disable", s.requireAuth(s.handleIfaceDisable))
	mux.HandleFunc("POST /interfaces/{id}/delete", s.requireAuth(s.handleIfaceDelete))

	// --- settings (panel knobs; Phase 6 adds the ops screens) ---
	mux.HandleFunc("GET /settings", s.requireAuth(s.handleSettingsPage))
	mux.HandleFunc("POST /settings", s.requireAuth(s.handleSettingsSave))

	h := http.Handler(mux)
	h = s.requireCSRF(h)
	h = s.sessionMiddleware(h)
	h = securityHeaders(h)
	h = bodyCap(h)
	return h
}
