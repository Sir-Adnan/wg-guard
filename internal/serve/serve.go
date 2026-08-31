// Package serve composes the long-running WG-Guard node: database → master
// key → settings → services → boot bring-up → HTTP listener → the central
// scheduler. It owns the runtime wiring that cmd/wg-guard keeps thin and
// testable, and the shutdown contract: stop accepting HTTP, drain in-flight
// requests, let the scheduler finish its current job, close the DB.
//
// All periodic work runs on the one scheduler goroutine (docs/architecture/
// overview.md §Resources): accounting cycle + expiry, sample flush, webhook
// delivery pass, housekeeping. Jobs are short; the webhook worker caps its
// own delivery concurrency internally.
package serve

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/api"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/boot"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/hoststats"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/scheduler"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/amneziawg"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/version"
	"github.com/Sir-Adnan/wg-guard/internal/web"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// Runtime cadences that are implementation constants rather than operator
// settings (kept out of the settings registry on purpose: the webhook pass
// is an indexed due-query that costs microseconds when nothing is due, and
// housekeeping runs idempotent prunes).
const (
	webhookPassInterval = 5 * time.Second
	housekeepingEvery   = 10 * time.Minute
	// Idempotency replay horizon: stored responses may be replayed for 24 h.
	idempotencyTTL = 24 * time.Hour
	// Webhook event/delivery rows are pruned after this retention; deliveries
	// that matter are dead-lettered and redeliverable within the window
	// (docs/integrations/webhooks.md: "payloads are pruned per retention").
	webhookRetention = 7 * 24 * time.Hour
	// preMigrationRetention caps the automatic backups-auto pool.
	preMigrationRetention = 5
)

// Options configures one node. Config is required; Backend substitutes the
// real AmneziaWG backend (dev/benchmark mode: no host networking is touched
// and boot bring-up is skipped).
type Options struct {
	Config     *config.Config
	ConfigPath string         // source boot config path (archived by backups)
	Backend    tunnel.Backend // nil = real AmneziaWG CLI backend
	Log        *slog.Logger   // nil = slog.Default()
}

// Node is one running WG-Guard instance: services, HTTP server, scheduler.
type Node struct {
	cfg           *config.Config
	log           *slog.Logger
	db            *database.DB
	ring          *secrets.KeyRing
	reg           *settings.Registry
	sched         *scheduler.Scheduler
	httpServer    *http.Server
	listener      net.Listener
	metrics       *metrics.Collector
	accounting    *accounting.Service
	backup        *backup.Service
	apiServer     *api.Server
	webServer     *web.Server
	webhookWorker *webhook.Worker
	sessions      *auth.SessionStore

	booted atomic.Bool
}

// serializedReconciler serializes reconcile passes: the accounting cycle,
// the expiry pass and API-triggered reconcile share one backend, and
// concurrent AWG subprocess operations on the same interface are exactly the
// race the verify-after-apply gate exists to catch. It implements
// accounting.Reconciler so both paths hold one engine.
type serializedReconciler struct {
	mu    sync.Mutex
	inner accounting.Reconciler
}

func (r *serializedReconciler) Run(ctx context.Context) (*reconcile.Report, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inner.Run(ctx)
}

// Start brings up a node and begins serving. It returns once the listener is
// bound and background jobs are scheduled; the caller blocks on its own
// signal context and then calls Shutdown. On error, everything started so
// far is torn down.
func Start(ctx context.Context, o Options) (*Node, error) {
	if o.Config == nil {
		return nil, fmt.Errorf("serve: config is required")
	}
	cfg := o.Config
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	log.Info("starting wg-guard", "version", version.Version, "listen", cfg.HTTPListen,
		"tls_mode", string(cfg.TLS.Mode))

	// The node owns its data layout: create the directories for the DB and
	// the master key if the installer (Phase 7) has not already.
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		return nil, fmt.Errorf("serve: data dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.MasterKeyFile), 0o700); err != nil {
		return nil, fmt.Errorf("serve: key dir: %w", err)
	}

	// A staged restore (panel wizard) is consumed BEFORE the database is
	// opened — never against a live WAL handle. Failures never abort boot.
	n := &Node{cfg: cfg, log: log}
	n.backup = &backup.Service{
		Cfg: cfg, ConfigPath: o.ConfigPath, Version: version.Version, Log: log,
	}
	pendingRestoreArchive := n.backup.ConsumePendingRestore()

	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		return nil, fmt.Errorf("serve: open database: %w", err)
	}
	n.db = db
	// Any failure from here on tears the node back down.
	fail := func(err error) (*Node, error) {
		_ = db.Close()
		return nil, err
	}

	// Automatic pre-migration backup (backup-restore.md §Sources): plain
	// archives, on-box only, separate retention pool.
	if pending, err := db.PendingCount(ctx); err == nil && pending > 0 {
		if res, err := n.backup.Create(ctx, backup.CreateOpts{
			Reason:    "pre-migration",
			Dir:       filepath.Join(cfg.DataDir, "backups-auto"),
			Deliver:   false,
			Retention: preMigrationRetention,
		}); err != nil {
			log.Warn("pre-migration backup failed; migrating anyway", "err", err)
		} else {
			log.Info("pre-migration backup created", "archive", res.Name)
		}
	}

	if err := db.Migrate(ctx, log); err != nil {
		return fail(fmt.Errorf("serve: migrate: %w", err))
	}
	if n.ring, err = secrets.LoadKeyRing(cfg.MasterKeyFile); err != nil {
		return fail(fmt.Errorf("serve: master key: %w", err))
	}
	if n.reg, err = settings.New(db, n.ring, settings.Defaults()); err != nil {
		return fail(fmt.Errorf("serve: settings: %w", err))
	}
	if err := n.ensureNodeID(ctx); err != nil {
		return fail(err)
	}

	auditSvc := audit.NewService(db)
	n.backup.DB, n.backup.Reg, n.backup.Audit = db, n.reg, auditSvc
	if pendingRestoreArchive != "" {
		_ = auditSvc.Record(ctx, audit.Entry{
			ActorType: audit.ActorSystem, Action: "backup.restored",
			Target:   pendingRestoreArchive,
			Metadata: map[string]any{"applied_at": "boot"},
		})
	}
	n.metrics = metrics.New()
	n.metrics.SetReady(n.ready)

	// Backend: real AmneziaWG CLI (default) or the injected fake (dev/bench:
	// no host networking, no boot bring-up — the fake models links in
	// memory).
	var (
		backend      tunnel.Backend
		shaperMgr    *shaper.Manager
		toolsVersion string
	)
	if o.Backend != nil {
		backend = o.Backend
		toolsVersion = "fake (dev backend)"
		log.Warn("dev backend active: no tunnel or firewall operations are performed on this host")
	} else {
		runner := subprocess.NewSystem()
		backend = amneziawg.New(runner)
		shaperMgr = shaper.New(runner)
		res, err := boot.BringUp(ctx, boot.Deps{
			DB: db, Ring: n.ring, Settings: n.reg, Backend: backend,
			Run: runner, Audit: auditSvc, Shaper: shaperMgr,
		})
		if err != nil {
			return fail(fmt.Errorf("serve: boot: %w", err))
		}
		toolsVersion = res.ToolsVersion
		for _, f := range res.Findings {
			log.Warn("boot finding", "tool", f.Tool, "detail", f.Detail, "remedy", f.Remedy)
		}
	}

	// Reconcile engine shared by boot, the accounting cycle/expiry and the
	// API — serialized behind one mutex (see serializedReconciler).
	rec := &serializedReconciler{inner: &reconcile.Engine{
		DB: db, Backend: backend, Ring: n.ring, Policy: n.driftPolicy(ctx),
	}}

	// Domain services. The webhook recorder is injected into user, device
	// and accounting so events commit in the SAME transaction as the state
	// change (docs/integrations/webhooks.md).
	recorder := webhook.NewRecorder()
	users := user.NewService(db)
	users.Recorder = recorder
	devices := device.NewService(db, n.ring)
	devices.Recorder = recorder
	plans := plan.NewService(db)
	ifaces := iface.NewService(db, n.reg, n.ring)
	webhooksSvc := webhook.NewService(db, n.ring)
	links := subscription.NewService(db, n.ring)

	n.accounting = accounting.NewService(db, backend, auditSvc, shaperMgr, n.reg)
	n.accounting.Reconciler = rec
	n.accounting.Recorder = recorder

	n.webhookWorker = webhook.NewWorker(db, n.ring, n.reg, log)
	n.sessions = auth.NewSessionStore(db, n.sessionIdleTTL(ctx), n.sessionAbsoluteTTL(ctx))
	admins := admin.NewService(db, n.sessions)

	nodeID, _ := n.reg.GetString(ctx, "node.id")
	n.apiServer = api.New(api.Deps{
		DB:           db,
		Tokens:       token.NewService(db),
		Users:        users,
		Devices:      devices,
		Plans:        plans,
		Ifaces:       ifaces,
		Settings:     n.reg,
		Ring:         n.ring,
		Audit:        auditSvc,
		Accounting:   n.accounting,
		Webhooks:     webhooksSvc,
		Metrics:      n.metrics,
		Log:          log,
		Reconciler:   rec,
		NodeID:       nodeID,
		ToolsVersion: toolsVersion,
	})

	// Admin panel: same services, session-cookie surface (Phase 5).
	n.webServer, err = web.New(web.Deps{
		DB:           db,
		Sessions:     n.sessions,
		Admins:       admins,
		Settings:     n.reg,
		Ring:         n.ring,
		Audit:        auditSvc,
		Users:        users,
		Devices:      devices,
		Plans:        plans,
		Ifaces:       ifaces,
		Accounting:   n.accounting,
		Links:        links,
		Log:          log,
		Reconciler:   rec,
		Host:         hoststats.New(cfg.DataDir),
		Version:      version.Version,
		TLSMode:      cfg.TLS.Mode,
		NodeID:       nodeID,
		ToolsVersion: toolsVersion,
	})
	if err != nil {
		return fail(fmt.Errorf("serve: web: %w", err))
	}

	// Listener + HTTP server. The root mux keeps the public ops endpoints
	// and the versioned API on their documented paths; everything else is
	// the admin panel.
	ln, err := n.listen()
	if err != nil {
		return fail(err)
	}
	n.listener = ln
	root := http.NewServeMux()
	if cfg.Metrics.Enabled {
		root.HandleFunc("/metrics", n.metrics.Handler)
		log.Info("metrics endpoint enabled", "path", "/metrics")
	}
	apiHandler := n.apiServer.Handler()
	root.Handle("/healthz", apiHandler)
	root.Handle("/readyz", apiHandler)
	root.Handle("/openapi.json", apiHandler)
	root.Handle("/docs", apiHandler)
	root.Handle("/api/", apiHandler)
	root.Handle("/", n.webServer.Handler())
	n.httpServer = &http.Server{
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second, // slowloris bound
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Scheduler: one goroutine for all periodic work. Jobs are registered
	// before Start so nothing can fire before the node is up; intervals are
	// re-read from settings on every run (live-apply without restart).
	n.sched = scheduler.New(log)
	n.sched.Every("accounting", n.accountingInterval(ctx), n.jobAccounting)
	n.sched.Every("samples", n.sampleFlushInterval(ctx), n.jobSamples)
	n.sched.Every("webhooks", webhookPassInterval, n.jobWebhooks)
	n.sched.Every("housekeeping", housekeepingEvery, n.jobHousekeeping)
	n.sched.Every("backups", time.Minute, n.jobBackups)
	n.sched.Start(ctx)

	serveErr := make(chan error, 1)
	go func() {
		defer close(serveErr)
		if err := n.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()
	select {
	case err := <-serveErr:
		n.sched.Stop()
		return fail(fmt.Errorf("serve: http: %w", err))
	default:
	}
	n.booted.Store(true)

	// node.started is a best-effort lifecycle event, emitted once the node
	// is actually serving. Failures are logged, never fatal.
	if err := recorder.Emit(ctx, db, webhook.EventNodeStarted, map[string]any{
		"node_id": nodeID, "version": version.Version,
	}); err != nil {
		log.Warn("node.started event not recorded", "err", err)
	}
	log.Info("wg-guard is serving",
		"addr", ln.Addr().String(), "backend", toolsVersion, "node_id", nodeID)
	return n, nil
}

// listen opens the listener for the configured TLS mode (ADR-0011). ACME is
// designed but deferred to the installer phase (it brings golang.org/x/crypto
// and port-80 lifecycle management that belong with deployment).
func (n *Node) listen() (net.Listener, error) {
	switch n.cfg.TLS.Mode {
	case config.TLSModeDev, config.TLSModeProxy:
		return net.Listen("tcp", n.cfg.HTTPListen)
	case config.TLSModeManual:
		cert, err := tls.LoadX509KeyPair(n.cfg.TLS.CertFile, n.cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("serve: tls: %w", err)
		}
		return tls.Listen("tcp", n.cfg.HTTPListen, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		})
	case config.TLSModeACME:
		return nil, domain.E(domain.CodeConfigInvalid,
			"tls.mode=acme is designed (ADR-0011) but lands with the installer (Phase 7); "+
				"use tls.mode=manual or tls.mode=proxy today")
	}
	return nil, domain.E(domain.CodeConfigInvalid, "unknown tls mode %q", n.cfg.TLS.Mode)
}

// ensureNodeID fills node.id with the hostname on first serve (settings
// contract: the panel identity is a default, not a hard requirement).
func (n *Node) ensureNodeID(ctx context.Context) error {
	v, err := n.reg.Get(ctx, "node.id")
	if err != nil {
		return fmt.Errorf("serve: node.id: %w", err)
	}
	if s, _ := v.(string); s != "" {
		return nil
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "wg-guard"
	}
	if err := n.reg.Set(ctx, "node.id", host); err != nil {
		return fmt.Errorf("serve: node.id: %w", err)
	}
	n.log.Info("node.id initialized from hostname", "node_id", host)
	return nil
}

func (n *Node) driftPolicy(ctx context.Context) reconcile.Policy {
	p, err := n.reg.GetString(ctx, "drift.policy")
	if err != nil || p == "" {
		return reconcile.PolicyReport
	}
	return reconcile.Policy(p)
}

func (n *Node) accountingInterval(ctx context.Context) time.Duration {
	if v, err := n.reg.GetInt(ctx, "accounting.interval_seconds"); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return 30 * time.Second
}

func (n *Node) sampleFlushInterval(ctx context.Context) time.Duration {
	if v, err := n.reg.GetInt(ctx, "accounting.sample_flush_seconds"); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return 300 * time.Second
}

func (n *Node) sessionIdleTTL(ctx context.Context) time.Duration {
	if v, err := n.reg.GetInt(ctx, "security.session_idle_hours"); err == nil && v > 0 {
		return time.Duration(v) * time.Hour
	}
	return 12 * time.Hour
}

func (n *Node) sessionAbsoluteTTL(ctx context.Context) time.Duration {
	if v, err := n.reg.GetInt(ctx, "security.session_absolute_hours"); err == nil && v > 0 {
		return time.Duration(v) * time.Hour
	}
	return 168 * time.Hour
}

// ready is the readiness gate: bring-up finished and the DB answers.
func (n *Node) ready() bool {
	if !n.booted.Load() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return n.db.PingContext(ctx) == nil
}

// Addr returns the bound listener address (useful for tests and logs).
func (n *Node) Addr() string {
	if n.listener == nil {
		return ""
	}
	return n.listener.Addr().String()
}

// Shutdown drains HTTP, lets the current scheduler job finish, and closes
// the database. It is safe to call more than once.
func (n *Node) Shutdown(ctx context.Context) error {
	var errs []error
	if n.httpServer != nil {
		if err := n.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if n.sched != nil {
		n.sched.Stop()
	}
	if err := n.db.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// --- scheduler job bodies -------------------------------------------------
//
// All bodies honor ctx cancellation (Shutdown / process stop) and log at
// debug when idle so a quiet node logs nothing per cycle at info level.

// jobAccounting runs one delta cycle, then the set-based expiry pass, then
// live-applies interval changes (safe from inside the running job: the
// scheduler anchors the next run at finish+newInterval — no hot loop).
func (n *Node) jobAccounting(ctx context.Context) error {
	if rep, err := n.accounting.RunCycle(ctx); err != nil {
		n.log.Warn("accounting cycle failed", "err", err)
	} else if rep != nil {
		n.metrics.SetLastCycle(rep.Duration, time.Now(), rep.Deltas)
		if rep.Deltas > 0 || rep.Activated > 0 || rep.QuotaTripped > 0 || len(rep.Errors) > 0 {
			n.log.Debug("accounting cycle",
				"devices", rep.Deltas, "rx", rep.RX, "tx", rep.TX,
				"activated", rep.Activated, "quota_tripped", rep.QuotaTripped,
				"duration", rep.Duration.Round(time.Millisecond))
		}
		for _, e := range rep.Errors {
			n.log.Warn("accounting cycle interface error", "interface", e.Interface, "err", e.Err)
		}
		if rep.ShaperError != "" {
			n.log.Warn("shaper refresh failed", "err", rep.ShaperError)
		}
	}
	if rep, err := n.accounting.EnforceExpiry(ctx); err != nil {
		n.log.Warn("expiry pass failed", "err", err)
	} else if rep != nil && rep.Expired > 0 {
		n.log.Info("expiry pass", "expired", rep.Expired)
	}
	n.sched.SetInterval("accounting", n.accountingInterval(ctx))
	return nil
}

// jobSamples flushes the in-memory sample accumulator (bucket-aligned
// upserts); totals never depend on it (persisted every cycle).
func (n *Node) jobSamples(ctx context.Context) error {
	flushed, err := n.accounting.FlushSamples(ctx)
	if err != nil {
		return err
	}
	if flushed > 0 {
		n.log.Debug("samples flushed", "rows", flushed)
	}
	n.sched.SetInterval("samples", n.sampleFlushInterval(ctx))
	return nil
}

// jobWebhooks runs one delivery pass: indexed due-query, capped concurrency,
// backoff, dead-lettering. Quiet when nothing is due.
func (n *Node) jobWebhooks(ctx context.Context) error {
	rep, err := n.webhookWorker.Pass(ctx)
	if err != nil {
		return err
	}
	if rep.Failed > 0 || rep.Dead > 0 {
		n.log.Warn("webhook delivery",
			"attempted", rep.Attempted, "delivered", rep.Delivered,
			"failed", rep.Failed, "dead", rep.Dead)
	}
	return nil
}

// jobBackups runs the once-per-minute due scan over backup schedules; each
// fired schedule creates its archive with its own retention (the scan is an
// indexed query over a tiny table — restart-safe, catch-up-once).
func (n *Node) jobBackups(ctx context.Context) error {
	_, err := n.backup.RunDue(ctx)
	return err
}

// jobHousekeeping prunes expired idempotency keys, dead sessions, old
// samples/rollups and webhook events, and re-reads runtime-tunable API
// settings so PATCHes apply without a restart.
func (n *Node) jobHousekeeping(ctx context.Context) error {
	now := time.Now().UTC()
	if rows, err := api.PruneIdempotency(ctx, n.db, now.Add(-idempotencyTTL)); err != nil {
		n.log.Warn("housekeeping: idempotency prune failed", "err", err)
	} else if rows > 0 {
		n.log.Debug("housekeeping: idempotency keys pruned", "rows", rows)
	}
	if rows, err := n.sessions.Prune(ctx, now); err != nil {
		n.log.Warn("housekeeping: session prune failed", "err", err)
	} else if rows > 0 {
		n.log.Debug("housekeeping: sessions pruned", "rows", rows)
	}
	if rep, err := n.accounting.Prune(ctx, now); err != nil {
		n.log.Warn("housekeeping: sample prune failed", "err", err)
	} else if rep.Samples+rep.Hourly+rep.Daily > 0 {
		n.log.Debug("housekeeping: traffic history pruned",
			"samples", rep.Samples, "hourly", rep.Hourly, "daily", rep.Daily)
	}
	if rows, err := webhook.Prune(ctx, n.db, now.Add(-webhookRetention)); err != nil {
		n.log.Warn("housekeeping: webhook prune failed", "err", err)
	} else if rows > 0 {
		n.log.Debug("housekeeping: webhook events pruned", "rows", rows)
	}
	n.apiServer.Reload()
	return nil
}
