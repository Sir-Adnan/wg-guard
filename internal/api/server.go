package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/integration"
	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/nodestatus"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/runtimeapply"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// Deps wires everything the REST surface needs. Nil-able collaborators
// (audit, metrics, accounting) degrade gracefully for tests.
type Deps struct {
	DB          *database.DB
	Tokens      *token.Service
	Users       *user.Service
	Devices     *device.Service
	Plans       *plan.Service
	Ifaces      *iface.Service
	Settings    *settings.Registry
	Ring        *secrets.KeyRing
	Audit       *audit.Service
	Accounting  *accounting.Service
	Webhooks    *webhook.Service
	Metrics     *metrics.Collector
	Telemetry   *telemetry.Sampler
	Links       *subscription.Service
	Integration *integration.Service
	Log         *slog.Logger

	// ClientConf renders client configs + QR (shared with the web panel).
	ClientConf *clientconf.Renderer

	// Reconciler runs after structural mutations so tunnel, peer, firewall,
	// NAT and shaping state take effect together. Serve supplies the full
	// boot.RuntimeReconciler behind one serialization lock. Nil = status-only
	// (tests without a backend).
	Reconciler accounting.Reconciler
	NodeStatus func(context.Context, time.Time) nodestatus.Snapshot

	// NodeID and ToolsVersion populate /node.
	NodeID       string
	ToolsVersion string
}

// Server is the REST API server. Handlers are registered once; the route
// table doubles as the OpenAPI coverage input.
type Server struct {
	Deps
	idem    *idempotencyStore
	limiter *rateLimiter
	routes  []routeDef
}

// routeDef is one registered route — the single source of truth shared by
// the mux, the OpenAPI coverage test, and the route smoke test.
type routeDef struct {
	Method       string
	Path         string
	Scope        string // "" = public
	Handler      http.HandlerFunc
	Idempotent   bool
	Paginated    bool
	NoStore      bool         // sensitive response (config/qr)
	TenantPolicy tenantPolicy // zero value denies reseller-bound tokens
}

type tenantPolicy uint8

const (
	tenantDenied tenantPolicy = iota
	tenantUserList
	tenantUserID
	tenantDeviceID
	tenantPrincipalOperation // own namespace is derived from the verified token
)

// New builds the server and registers every route.
func New(d Deps) *Server {
	if d.Links == nil && d.DB != nil && d.Ring != nil {
		d.Links = subscription.NewService(d.DB, d.Ring)
	}
	if d.Integration == nil && d.Links != nil {
		d.Integration = &integration.Service{DB: d.DB, Users: d.Users, Devices: d.Devices,
			Plans: d.Plans, Links: d.Links, Accounting: d.Accounting}
	}
	if d.ClientConf == nil {
		d.ClientConf = &clientconf.Renderer{
			Devices: d.Devices, Ifaces: d.Ifaces, Settings: d.Settings,
		}
	}
	s := &Server{Deps: d, idem: &idempotencyStore{db: d.DB}}
	s.limiter = newRateLimiter(s.rateLimitSetting())
	s.registerRoutes()
	return s
}

// Handler returns the full middleware-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routes {
		h := http.Handler(r.Handler)
		if r.Idempotent {
			h = s.idem.wrap(h)
		}
		if r.Scope != "" {
			h = s.rateLimitMiddleware(h)
			h = s.authMiddleware(r, h)
		}
		mux.HandleFunc(r.Method+" "+r.Path, h.ServeHTTP)
	}
	// Unmatched paths → the standard envelope (never a bare 404 page).
	mux.HandleFunc("/", s.notFound)

	var h http.Handler = mux
	h = s.loggingMiddleware(h)
	h = maxBodyMiddleware(h)
	h = corsMiddleware(h)
	h = securityHeaders(h)
	h = s.recoverMiddleware(h)
	h = requestIDMiddleware(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Request accounting: one counter per finished request.
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		h.ServeHTTP(rec, r)
		if s.Metrics != nil {
			s.Metrics.IncRequest(rec.status)
		}
	})
}

func (s *Server) rateLimitSetting() int {
	if s.Settings == nil {
		return 600
	}
	n, err := s.Settings.GetInt(context.Background(), "api.rate_limit_per_minute")
	if err != nil || n < 0 {
		return 600
	}
	return n
}

// Reload re-reads runtime-tunable API settings (the per-token rate limit)
// from the registry and drops expired rate-limit windows. The serve
// housekeeping job calls it so a settings PATCH takes effect without a
// restart and the limiter map stays bounded by active tokens.
func (s *Server) Reload() {
	s.limiter.SetLimit(s.rateLimitSetting())
	s.limiter.enforce(time.Now().Unix())
}

func (s *Server) registerRoutes() {
	add := func(r routeDef) { s.routes = append(s.routes, r) }

	// --- Ops (public) ---
	add(routeDef{Method: http.MethodGet, Path: "/healthz", Handler: s.Metrics.Healthz})
	add(routeDef{Method: http.MethodGet, Path: "/readyz", Handler: s.Metrics.Readyz})
	add(routeDef{Method: http.MethodGet, Path: "/openapi.json", Handler: s.handleOpenAPI})
	add(routeDef{Method: http.MethodGet, Path: "/docs", Handler: s.handleDocs})

	// --- Node ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/node/health", Handler: s.handleNodeHealth})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/node", Scope: "node.read", Handler: s.handleNode})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/node/stats", Scope: "node.read", Handler: s.handleNodeStats})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/node/status", Scope: "node.read", Handler: s.handleNodeStatus, NoStore: true})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/node/telemetry", Scope: "stats.read", Handler: s.handleTelemetry})

	// --- Users ---
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/purchases", Scope: "purchases.create", Handler: s.handlePurchase, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/operations/result", Scope: "operations.read", Handler: s.handleOperationResult, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users", Scope: "users.create", Handler: s.handleUserCreate, Idempotent: true})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users", Scope: "users.read", Handler: s.handleUserList, Paginated: true, TenantPolicy: tenantUserList})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}", Scope: "users.read", Handler: s.handleUserGet, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/subscription", Scope: "subscriptions.read", Handler: s.handleCustomerLink, NoStore: true, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/subscription/rotate", Scope: "subscriptions.rotate", Handler: s.handleCustomerLinkRotate, NoStore: true, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/next-plan", Scope: "next_plans.read", Handler: s.handleNextPlanGet, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodPut, Path: "/api/v1/users/{id}/next-plan", Scope: "next_plans.write", Handler: s.handleNextPlanPut, Idempotent: true, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/users/{id}/next-plan", Scope: "next_plans.write", Handler: s.handleNextPlanDelete, Idempotent: true, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/next-plan/activations", Scope: "next_plans.read", Handler: s.handleNextPlanActivations, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/users/{id}", Scope: "users.update", Handler: s.handleUserUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/users/{id}", Scope: "users.delete", Handler: s.handleUserDelete})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/enable", Scope: "users.update", Handler: s.handleUserEnable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/disable", Scope: "users.update", Handler: s.handleUserDisable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/renew", Scope: "users.update", Handler: s.handleUserRenew, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/quota/add", Scope: "users.update", Handler: s.handleQuotaTopUp, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/traffic/add", Scope: "traffic.update", Handler: s.handleTrafficAdd, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/traffic/set", Scope: "traffic.update", Handler: s.handleTrafficSet, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/traffic/reset", Scope: "traffic.update", Handler: s.handleTrafficReset, Idempotent: true, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/traffic", Scope: "traffic.read", Handler: s.handleTrafficSeries, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/bulk", Scope: "users.bulk", Handler: s.handleBulkCreate, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/bulk-action", Scope: "users.bulk", Handler: s.handleBulkAction, Idempotent: true})

	// --- Devices ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/devices", Scope: "devices.read", Handler: s.handleDeviceListForUser, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/devices", Scope: "devices.write", Handler: s.handleDeviceCreate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}", Scope: "devices.read", Handler: s.handleDeviceGet, TenantPolicy: tenantDeviceID})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/devices/{id}", Scope: "devices.write", Handler: s.handleDeviceUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/devices/{id}", Scope: "devices.write", Handler: s.handleDeviceDelete})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/devices/{id}/enable", Scope: "devices.write", Handler: s.handleDeviceEnable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/devices/{id}/disable", Scope: "devices.write", Handler: s.handleDeviceDisable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/devices/{id}/regenerate", Scope: "devices.write", Handler: s.handleDeviceRegenerate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}/config", Scope: "configs.read", Handler: s.handleDeviceConfig, NoStore: true, TenantPolicy: tenantDeviceID})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}/qr", Scope: "configs.read", Handler: s.handleDeviceQR, NoStore: true, TenantPolicy: tenantDeviceID})

	// --- Stats ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/stats", Scope: "stats.read", Handler: s.handleStats})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/stats", Scope: "stats.read", Handler: s.handleUserStats, TenantPolicy: tenantUserID})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}/stats", Scope: "stats.read", Handler: s.handleDeviceStats, TenantPolicy: tenantDeviceID})

	// --- Plans ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/templates", Scope: "templates.read", Handler: s.handlePlanList})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/templates", Scope: "templates.write", Handler: s.handlePlanCreate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/templates/{id}", Scope: "templates.read", Handler: s.handlePlanGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/templates/{id}", Scope: "templates.write", Handler: s.handlePlanUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/templates/{id}", Scope: "templates.write", Handler: s.handlePlanDelete})

	// --- Interfaces ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/interfaces", Scope: "interfaces.read", Handler: s.handleIfaceList})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/interfaces", Scope: "interfaces.write", Handler: s.handleIfaceCreate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/interfaces/{id}", Scope: "interfaces.read", Handler: s.handleIfaceGet})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/interfaces/{id}/capacity", Scope: "interfaces.read", Handler: s.handleIfaceCapacity})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/interfaces/{id}", Scope: "interfaces.write", Handler: s.handleIfaceUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/interfaces/{id}", Scope: "interfaces.write", Handler: s.handleIfaceDelete})

	// --- Settings ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/settings", Scope: "node.read", Handler: s.handleSettingsGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/settings", Scope: "node.settings", Handler: s.handleSettingsUpdate})

	// --- Webhooks ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/webhooks", Scope: "webhooks.read", Handler: s.handleWebhookList, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/webhooks", Scope: "webhooks.write", Handler: s.handleWebhookCreate, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/webhooks/{id}", Scope: "webhooks.read", Handler: s.handleWebhookGet, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/webhooks/{id}", Scope: "webhooks.write", Handler: s.handleWebhookUpdate, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/webhooks/{id}", Scope: "webhooks.write", Handler: s.handleWebhookDelete, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/webhooks/{id}/redeliver", Scope: "webhooks.write", Handler: s.handleWebhookRedeliver, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/webhooks/{id}/deliveries", Scope: "webhooks.read", Handler: s.handleWebhookReceipts, TenantPolicy: tenantPrincipalOperation})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/webhooks/{id}/deliveries/{deliveryID}", Scope: "webhooks.read", Handler: s.handleWebhookReceipt, TenantPolicy: tenantPrincipalOperation})
}

// audit records one API action with the token actor and request context.
func (s *Server) audit(r *http.Request, action, target string, meta map[string]any) {
	if s.Audit == nil {
		return
	}
	actorType, actorID := audit.ActorSystem, ""
	if v := TokenFrom(r.Context()); v != nil {
		actorType, actorID = audit.ActorToken, v.Token.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Entry{
		ActorType: actorType, ActorID: actorID,
		Action: action, Target: target,
		SourceIP: clientIP(r), RequestID: RequestID(r.Context()),
		Metadata: meta,
	})
}

// reconcile runs the canonical network pass after structural changes. Errors
// are logged and returned; ordinary mutations deliberately ignore the return
// because the database is their retry source, while destructive mutations can
// require runtime success before removing that source of truth.
func (s *Server) reconcile(r *http.Request) error {
	log := s.Log
	if log != nil {
		log = log.With("request_id", RequestID(r.Context()))
	}
	return runtimeapply.Attempt(r.Context(), s.Reconciler, log).Err
}
