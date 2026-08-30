package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// Deps wires everything the REST surface needs. Nil-able collaborators
// (audit, metrics, accounting) degrade gracefully for tests.
type Deps struct {
	DB         *database.DB
	Tokens     *token.Service
	Users      *user.Service
	Devices    *device.Service
	Plans      *plan.Service
	Ifaces     *iface.Service
	Settings   *settings.Registry
	Ring       *secrets.KeyRing
	Audit      *audit.Service
	Accounting *accounting.Service
	Webhooks   *webhook.Service
	Metrics    *metrics.Collector
	Log        *slog.Logger

	// Reconciler runs after structural mutations so peer changes take
	// effect immediately (satisfied by *reconcile.Engine; serve wraps it in
	// a single-flight adapter so API calls and the accounting cycle
	// serialize). Nil = status-only (tests without a backend).
	Reconciler accounting.Reconciler

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
	Method     string
	Path       string
	Scope      string // "" = public
	Handler    http.HandlerFunc
	Idempotent bool
	Paginated  bool
	NoStore    bool // sensitive response (config/qr)
}

// New builds the server and registers every route.
func New(d Deps) *Server {
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
		if r.Scope != "" {
			h = s.rateLimitMiddleware(h)
			h = s.authMiddleware(r.Scope, h)
		}
		if r.Idempotent {
			h = s.idem.wrap(h)
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

	// --- Users ---
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users", Scope: "users.create", Handler: s.handleUserCreate, Idempotent: true})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users", Scope: "users.read", Handler: s.handleUserList, Paginated: true})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}", Scope: "users.read", Handler: s.handleUserGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/users/{id}", Scope: "users.update", Handler: s.handleUserUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/users/{id}", Scope: "users.delete", Handler: s.handleUserDelete})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/enable", Scope: "users.update", Handler: s.handleUserEnable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/disable", Scope: "users.update", Handler: s.handleUserDisable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/renew", Scope: "users.update", Handler: s.handleUserRenew, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/traffic/add", Scope: "traffic.update", Handler: s.handleTrafficAdd, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/traffic/set", Scope: "traffic.update", Handler: s.handleTrafficSet, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/traffic/reset", Scope: "traffic.update", Handler: s.handleTrafficReset, Idempotent: true})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/traffic", Scope: "traffic.read", Handler: s.handleTrafficSeries})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/bulk", Scope: "users.bulk", Handler: s.handleBulkCreate, Idempotent: true})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/bulk-action", Scope: "users.bulk", Handler: s.handleBulkAction, Idempotent: true})

	// --- Devices ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/devices", Scope: "devices.read", Handler: s.handleDeviceListForUser})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/users/{id}/devices", Scope: "devices.write", Handler: s.handleDeviceCreate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}", Scope: "devices.read", Handler: s.handleDeviceGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/devices/{id}", Scope: "devices.write", Handler: s.handleDeviceUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/devices/{id}", Scope: "devices.write", Handler: s.handleDeviceDelete})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/devices/{id}/enable", Scope: "devices.write", Handler: s.handleDeviceEnable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/devices/{id}/disable", Scope: "devices.write", Handler: s.handleDeviceDisable})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/devices/{id}/regenerate", Scope: "devices.write", Handler: s.handleDeviceRegenerate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}/config", Scope: "configs.read", Handler: s.handleDeviceConfig, NoStore: true})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}/qr", Scope: "configs.read", Handler: s.handleDeviceQR, NoStore: true})

	// --- Stats ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/stats", Scope: "stats.read", Handler: s.handleStats})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/users/{id}/stats", Scope: "stats.read", Handler: s.handleUserStats})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/devices/{id}/stats", Scope: "stats.read", Handler: s.handleDeviceStats})

	// --- Plans ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/plans", Scope: "plans.read", Handler: s.handlePlanList})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/plans", Scope: "plans.write", Handler: s.handlePlanCreate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/plans/{id}", Scope: "plans.read", Handler: s.handlePlanGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/plans/{id}", Scope: "plans.write", Handler: s.handlePlanUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/plans/{id}", Scope: "plans.write", Handler: s.handlePlanDelete})

	// --- Interfaces ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/interfaces", Scope: "interfaces.read", Handler: s.handleIfaceList})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/interfaces", Scope: "interfaces.write", Handler: s.handleIfaceCreate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/interfaces/{id}", Scope: "interfaces.read", Handler: s.handleIfaceGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/interfaces/{id}", Scope: "interfaces.write", Handler: s.handleIfaceUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/interfaces/{id}", Scope: "interfaces.write", Handler: s.handleIfaceDelete})

	// --- Settings ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/settings", Scope: "node.read", Handler: s.handleSettingsGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/settings", Scope: "node.settings", Handler: s.handleSettingsUpdate})

	// --- Webhooks ---
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/webhooks", Scope: "webhooks.read", Handler: s.handleWebhookList})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/webhooks", Scope: "webhooks.write", Handler: s.handleWebhookCreate})
	add(routeDef{Method: http.MethodGet, Path: "/api/v1/webhooks/{id}", Scope: "webhooks.read", Handler: s.handleWebhookGet})
	add(routeDef{Method: http.MethodPatch, Path: "/api/v1/webhooks/{id}", Scope: "webhooks.write", Handler: s.handleWebhookUpdate})
	add(routeDef{Method: http.MethodDelete, Path: "/api/v1/webhooks/{id}", Scope: "webhooks.write", Handler: s.handleWebhookDelete})
	add(routeDef{Method: http.MethodPost, Path: "/api/v1/webhooks/{id}/redeliver", Scope: "webhooks.write", Handler: s.handleWebhookRedeliver})
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

// reconcile runs the engine after structural changes so peer updates land
// immediately. Errors are logged, never failed loudly: the accounting cycle
// and boot re-derive the same state (DB is the source of truth).
func (s *Server) reconcile(r *http.Request) {
	if s.Reconciler == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := s.Reconciler.Run(ctx); err != nil && s.Log != nil {
		s.Log.Warn("reconcile after mutation failed", "error", err, "request_id", RequestID(r.Context()))
	}
}
