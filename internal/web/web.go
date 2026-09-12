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
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// Deps wires the services the panel renders. The same instances the REST
// API uses are passed in — one business layer, two surfaces.
type Deps struct {
	DB       *database.DB
	Sessions *auth.SessionStore
	Admins   *admin.Service
	Settings *settings.Registry
	Ring     *secrets.KeyRing
	Audit    *audit.Service
	Users    *user.Service
	Devices  *device.Service
	Plans    *plan.Service
	Ifaces   *iface.Service
	// ProfileGenerator is the canonical server-side profile preview seam.
	// It defaults to Ifaces.GenerateProfile; tests may replace it to exercise
	// entropy failures without weakening the production generator.
	ProfileGenerator func(iface.ProfilePolicy) (iface.Obfuscation, error)
	Accounting       *accounting.Service
	Log              *slog.Logger

	// Reconciler runs the complete serialized network pass after structural
	// mutations (see api.Server).
	Reconciler accounting.Reconciler

	// ClientConf renders client configs + QR (shared with the REST API).
	ClientConf *clientconf.Renderer

	// Links serves the per-user subscription links (public /sub/ surface).
	Links *subscription.Service

	// Backup is the archive engine (panel + CLI only — ADR-0007). Wired
	// from serve; nil in tests that don't exercise the ops screens.
	Backup *backup.Service

	// Tokens and Webhooks are the same instances the REST API uses — one
	// business layer, two surfaces.
	Tokens   *token.Service
	Webhooks *webhook.Service

	// Telemetry is the scheduler-owned immutable live history. Dashboard
	// requests never invoke its source.
	Telemetry *telemetry.Sampler

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
	if s.ProfileGenerator == nil && d.Ifaces != nil {
		s.ProfileGenerator = d.Ifaces.GenerateProfile
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
	mux.HandleFunc("GET /", s.handleNotFound)
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
	mux.HandleFunc("GET /users/bulk", s.requirePermission(auth.ScopeUsersBulk, s.handleUserBulkPage))
	mux.HandleFunc("GET /users", s.requirePermission(auth.ScopeUsersRead, s.handleUserList))
	mux.HandleFunc("GET /users/new", s.requirePermission(auth.ScopeUsersCreate, s.handleUserNew))
	mux.HandleFunc("POST /users", s.requirePermission(auth.ScopeUsersCreate, s.handleUserCreate))
	mux.HandleFunc("POST /users/bulk", s.requirePermission(auth.ScopeUsersBulk, s.handleBulkCreate))
	mux.HandleFunc("POST /users/bulk-action", s.requirePermission(auth.ScopeUsersBulk, s.handleBulkAction))
	mux.HandleFunc("GET /users/{id}", s.requirePermission(auth.ScopeUsersRead, s.handleUserDetail))
	mux.HandleFunc("GET /users/{id}/edit", s.requirePermission(auth.ScopeUsersUpdate, s.handleUserEditPage))
	mux.HandleFunc("POST /users/{id}/edit", s.requirePermission(auth.ScopeUsersUpdate, s.handleUserUpdate))
	mux.HandleFunc("POST /users/{id}/enable", s.requirePermission(auth.ScopeUsersUpdate, s.handleUserEnable))
	mux.HandleFunc("POST /users/{id}/disable", s.requirePermission(auth.ScopeUsersUpdate, s.handleUserDisable))
	mux.HandleFunc("POST /users/{id}/delete", s.requirePermission(auth.ScopeUsersDelete, s.handleUserDelete))
	mux.HandleFunc("POST /users/{id}/restore", s.requirePermission(auth.ScopeUsersUpdate, s.handleUserRestore))
	mux.HandleFunc("POST /users/{id}/renew", s.requirePermission(auth.ScopeUsersUpdate, s.handleUserRenew))
	mux.HandleFunc("POST /users/{id}/traffic/add", s.requirePermission(auth.ScopeTrafficUpdate, s.handleUserTrafficAdd))
	mux.HandleFunc("POST /users/{id}/traffic/reset", s.requirePermission(auth.ScopeTrafficUpdate, s.handleUserTrafficReset))
	mux.HandleFunc("POST /users/{id}/sub/create", s.requirePermission(auth.ScopeUsersUpdate, s.handleSubCreate))
	mux.HandleFunc("POST /users/{id}/sub/regenerate", s.requirePermission(auth.ScopeUsersUpdate, s.handleSubRegenerate))
	mux.HandleFunc("POST /users/{id}/sub/revoke", s.requirePermission(auth.ScopeUsersUpdate, s.handleSubRevoke))
	mux.HandleFunc("POST /users/{id}/sub/restore", s.requirePermission(auth.ScopeUsersUpdate, s.handleSubRestore))
	mux.HandleFunc("POST /users/{id}/devices", s.requirePermission(auth.ScopeDevicesWrite, s.handleDeviceCreate))

	// --- devices ---
	mux.HandleFunc("POST /devices/{id}/enable", s.requirePermission(auth.ScopeDevicesWrite, s.handleDeviceEnable))
	mux.HandleFunc("POST /devices/{id}/disable", s.requirePermission(auth.ScopeDevicesWrite, s.handleDeviceDisable))
	mux.HandleFunc("POST /devices/{id}/regenerate", s.requirePermission(auth.ScopeDevicesWrite, s.handleDeviceRegenerate))
	mux.HandleFunc("POST /devices/{id}/delete", s.requirePermission(auth.ScopeDevicesWrite, s.handleDeviceDelete))
	mux.HandleFunc("GET /devices/{id}/config", s.requirePermission(auth.ScopeConfigsRead, s.handleDeviceConfig))
	mux.HandleFunc("GET /devices/{id}/qr", s.requirePermission(auth.ScopeConfigsRead, s.handleDeviceQR))

	// --- plans ---
	mux.HandleFunc("GET /plans", s.requirePermission(auth.ScopePlansRead, s.handlePlanList))
	mux.HandleFunc("GET /plans/new", s.requirePermission(auth.ScopePlansWrite, s.handlePlanNew))
	mux.HandleFunc("POST /plans", s.requirePermission(auth.ScopePlansWrite, s.handlePlanCreate))
	mux.HandleFunc("GET /plans/{id}/edit", s.requirePermission(auth.ScopePlansWrite, s.handlePlanEditPage))
	mux.HandleFunc("POST /plans/{id}/edit", s.requirePermission(auth.ScopePlansWrite, s.handlePlanUpdate))
	mux.HandleFunc("POST /plans/{id}/enable", s.requirePermission(auth.ScopePlansWrite, s.handlePlanEnable))
	mux.HandleFunc("POST /plans/{id}/disable", s.requirePermission(auth.ScopePlansWrite, s.handlePlanDisable))
	mux.HandleFunc("POST /plans/{id}/delete", s.requirePermission(auth.ScopePlansWrite, s.handlePlanDelete))

	// --- interfaces ---
	mux.HandleFunc("GET /interfaces", s.requirePermission(auth.ScopeIfaceRead, s.handleIfaceList))
	mux.HandleFunc("GET /interfaces/new", s.requirePermission(auth.ScopeIfaceWrite, s.handleIfaceNew))
	mux.HandleFunc("POST /interfaces", s.requirePermission(auth.ScopeIfaceWrite, s.handleIfaceCreate))
	mux.HandleFunc("POST /interfaces/profile-preview", s.requirePermission(auth.ScopeIfaceWrite, s.handleProfilePreview))
	mux.HandleFunc("GET /interfaces/{id}/edit", s.requirePermission(auth.ScopeIfaceWrite, s.handleIfaceEditPage))
	mux.HandleFunc("POST /interfaces/{id}/edit", s.requirePermission(auth.ScopeIfaceWrite, s.handleIfaceUpdate))
	mux.HandleFunc("POST /interfaces/{id}/enable", s.requirePermission(auth.ScopeIfaceWrite, s.handleIfaceEnable))
	mux.HandleFunc("POST /interfaces/{id}/disable", s.requirePermission(auth.ScopeIfaceWrite, s.handleIfaceDisable))
	mux.HandleFunc("POST /interfaces/{id}/delete", s.requirePermission(auth.ScopeIfaceWrite, s.handleIfaceDelete))

	// --- settings (node.settings: the registry is operator territory) ---
	mux.HandleFunc("GET /settings", s.requirePermission(auth.ScopeNodeSettings, s.handleSettingsPage))
	mux.HandleFunc("POST /settings", s.requirePermission(auth.ScopeNodeSettings, s.handleSettingsSave))

	// --- backups (backup.manage; ADR-0007: panel/CLI only) ---
	mux.HandleFunc("GET /backups", s.requirePermission(auth.ScopeBackupManage, s.handleBackupsPage))
	mux.HandleFunc("POST /backups/create", s.requirePermission(auth.ScopeBackupManage, s.handleBackupCreate))
	mux.HandleFunc("POST /backups/delete", s.requirePermission(auth.ScopeBackupManage, s.handleBackupDelete))
	mux.HandleFunc("GET /backups/{name}/download", s.requirePermission(auth.ScopeBackupManage, s.handleBackupDownload))
	mux.HandleFunc("POST /backups/restore", s.requirePermission(auth.ScopeBackupManage, s.handleBackupRestore))
	mux.HandleFunc("POST /backups/restore/confirm", s.requirePermission(auth.ScopeBackupManage, s.handleBackupRestoreConfirm))
	mux.HandleFunc("POST /backups/restore/cancel", s.requirePermission(auth.ScopeBackupManage, s.handleBackupRestoreCancel))
	mux.HandleFunc("POST /backups/schedules", s.requirePermission(auth.ScopeBackupManage, s.handleScheduleCreate))
	mux.HandleFunc("POST /backups/schedules/{id}/update", s.requirePermission(auth.ScopeBackupManage, s.handleScheduleUpdate))
	mux.HandleFunc("POST /backups/schedules/{id}/delete", s.requirePermission(auth.ScopeBackupManage, s.handleScheduleDelete))
	mux.HandleFunc("POST /backups/schedules/{id}/toggle", s.requirePermission(auth.ScopeBackupManage, s.handleScheduleToggle))
	mux.HandleFunc("POST /backups/telegram-test", s.requirePermission(auth.ScopeBackupManage, s.handleTelegramTest))

	// --- administrators (admins.manage) ---
	mux.HandleFunc("GET /admins", s.requirePermission(auth.ScopeAdminsManage, s.handleAdminsPage))
	mux.HandleFunc("POST /admins/create", s.requirePermission(auth.ScopeAdminsManage, s.handleAdminCreate))
	mux.HandleFunc("POST /admins/{id}/password", s.requirePermission(auth.ScopeAdminsManage, s.handleAdminPassword))
	mux.HandleFunc("POST /admins/{id}/permissions", s.requirePermission(auth.ScopeAdminsManage, s.handleAdminPermissions))
	mux.HandleFunc("POST /admins/{id}/enable", s.requirePermission(auth.ScopeAdminsManage, s.handleAdminEnable))
	mux.HandleFunc("POST /admins/{id}/delete", s.requirePermission(auth.ScopeAdminsManage, s.handleAdminDelete))

	// --- API tokens (api_tokens.manage) ---
	mux.HandleFunc("GET /tokens", s.requirePermission(auth.ScopeAPITokensManage, s.handleTokensPage))
	mux.HandleFunc("POST /tokens/create", s.requirePermission(auth.ScopeAPITokensManage, s.handleTokenCreate))
	mux.HandleFunc("POST /tokens/{id}/revoke", s.requirePermission(auth.ScopeAPITokensManage, s.handleTokenRevoke))

	// --- webhooks (webhooks.read / webhooks.write) ---
	mux.HandleFunc("GET /webhooks", s.requirePermission(auth.ScopeWebhooksRead, s.handleWebhooksPage))
	mux.HandleFunc("POST /webhooks/create", s.requirePermission(auth.ScopeWebhooksWrite, s.handleWebhookCreate))
	mux.HandleFunc("GET /webhooks/{id}", s.requirePermission(auth.ScopeWebhooksRead, s.handleWebhookShow))
	mux.HandleFunc("POST /webhooks/{id}/update", s.requirePermission(auth.ScopeWebhooksWrite, s.handleWebhookUpdate))
	mux.HandleFunc("POST /webhooks/{id}/rotate", s.requirePermission(auth.ScopeWebhooksWrite, s.handleWebhookRotate))
	mux.HandleFunc("POST /webhooks/{id}/delete", s.requirePermission(auth.ScopeWebhooksWrite, s.handleWebhookDelete))
	mux.HandleFunc("POST /webhooks/{id}/redeliver", s.requirePermission(auth.ScopeWebhooksWrite, s.handleWebhookRedeliver))

	// --- audit (audit.view) ---
	mux.HandleFunc("GET /audit", s.requirePermission(auth.ScopeAuditView, s.handleAuditPage))

	h := http.Handler(mux)
	h = s.requireCSRF(h)
	h = s.sessionMiddleware(h)
	h = securityHeaders(h)
	h = bodyCap(h)
	return h
}

// The shared error surface is used for unmatched browser pages. Binary downloads,
// API envelopes and method-specific responses keep their existing contracts.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	layout := "auth"
	renderRequest := r
	if adminFrom(r) != nil {
		layout = "app"
	} else if lang := r.URL.Query().Get("lang"); lang == "fa" || lang == "en" {
		localized := r.Clone(r.Context())
		localized.Header = r.Header.Clone()
		localized.Header.Del("Cookie")
		for _, cookie := range r.Cookies() {
			if cookie.Name != localeCookie {
				localized.AddCookie(cookie)
			}
		}
		localized.AddCookie(&http.Cookie{Name: localeCookie, Value: lang})
		renderRequest = localized
	}
	_ = s.render(w, renderRequest, "error", layout, struct {
		Status     int
		MessageKey string
	}{404, "common.error_not_found"})
}
