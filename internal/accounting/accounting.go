// Package accounting implements the traffic accounting pipeline and the
// limit enforcement the product can never afford to lie about
// (docs/product/requirements.md §Accounting, docs/architecture/database.md).
//
// Delta invariant: accumulated usage lives in SQLite, never in AWG counters.
// Per cycle, one dump per enabled interface is diffed against the stored
// baseline (`devices.last_rx/last_tx`): `new < last ⇒ counter reset ⇒ count
// current from zero and re-baseline`. Counters reset on reboots, link
// recreation, and peer re-adds; the invariant makes all three safe — no
// negative deltas, no reset corruption, no double counting, no usage loss.
//
// One transaction per cycle (BEGIN IMMEDIATE) writes only changed rows:
// device totals/baselines/handshakes, user totals/activity, and lifecycle
// transitions (first-connection activation, quota trips). Rows whose values
// did not change are not written. Expiry is a separate set-based pass.
// Enforcement transitions trigger a reconciliation pass, so an expired or
// quota-exhausted user actually loses their peers — a status flip alone
// would not stop traffic.
//
// Quota enforcement is deliberately edge-triggered (active → traffic_exceeded
// and never back): the cycle must never override an explicit admin decision
// (a manually re-enabled account, a grace grant). Recovery is an admin
// action: reset/add/remove traffic, or a status change.
package accounting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Reconciler is the reconcile-engine surface the cycle needs to stop traffic
// after enforcement transitions (satisfied by *reconcile.Engine).
type Reconciler interface {
	Run(ctx context.Context) (*reconcile.Report, error)
}

// Service runs accounting cycles, expiry and traffic mutations.
type Service struct {
	db      *database.DB
	backend tunnel.Backend
	audit   *audit.Service
	shaper  *shaper.Manager
	reg     *settings.Registry
	now     func() time.Time

	// Reconciler (optional) runs when the cycle or expiry pass changed who
	// may hold peers. Nil = status-only enforcement (tests).
	Reconciler Reconciler

	samples accumulator
}

// NewService wires the pipeline. audit, shaper and reg are optional (nil
// skips audit entries and shaper refresh; nil settings use sample defaults).
func NewService(db *database.DB, backend tunnel.Backend, auditSvc *audit.Service, shaperMgr *shaper.Manager, reg *settings.Registry) *Service {
	return &Service{
		db:      db,
		backend: backend,
		audit:   auditSvc,
		shaper:  shaperMgr,
		reg:     reg,
		now:     time.Now,
	}
}

// Actor identifies who triggered a traffic mutation (system when zero).
type Actor struct {
	Type string
	ID   string
}

func (a Actor) typ() string {
	if a.Type == "" {
		return audit.ActorSystem
	}
	return a.Type
}

// CycleError is a per-interface failure; the cycle continues with the other
// interfaces (same resilience contract as reconcile).
type CycleError struct {
	Interface string
	Err       string
}

// CycleReport summarizes one accounting cycle. It carries no key material.
type CycleReport struct {
	Interfaces    int
	Deltas        int    // devices with traffic this cycle
	RX, TX        uint64 // delta sums
	Activated     int    // first-connection activations
	QuotaTripped  int    // users flipped to traffic_exceeded
	ShaperApplied bool
	ShaperError   string
	Errors        []CycleError
	Reconciled    *reconcile.Report // set when transitions triggered a pass
	Duration      time.Duration
}

// ReconcileNeeded reports whether the cycle changed who may hold peers.
func (r *CycleReport) ReconcileNeeded() bool { return r.Activated > 0 || r.QuotaTripped > 0 }

// RunCycle dumps every enabled interface once, then applies all deltas,
// activity, and lifecycle transitions in one transaction.
func (s *Service) RunCycle(ctx context.Context) (*CycleReport, error) {
	start := s.now()
	rep := &CycleReport{}
	pendingAudit := []audit.Entry{}

	ifaces, err := s.loadEnabledInterfaces(ctx)
	if err != nil {
		return nil, err
	}
	rep.Interfaces = len(ifaces)

	// Dumps run OUTSIDE the write transaction: subprocesses must never hold
	// the SQLite write lock.
	observed := map[string]tunnel.PeerState{}
	for _, ifc := range ifaces {
		st, err := s.backend.Dump(ctx, ifc.name)
		if err != nil {
			if errors.Is(err, tunnel.ErrInterfaceNotFound) {
				continue // reconciler's business, not accounting's
			}
			rep.Errors = append(rep.Errors, CycleError{Interface: ifc.name, Err: err.Error()})
			continue
		}
		for _, p := range st.Peers {
			observed[p.PublicKey] = p
		}
	}

	// Shaping: refresh the rendered tc state (change-detected; an unchanged
	// desired state costs zero subprocesses). Limits follow DB state within
	// one cycle.
	if s.shaper != nil {
		s.applyShaper(ctx, ifaces, rep)
	}

	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		return s.cycleTx(ctx, tx, observed, rep, &pendingAudit)
	}); err != nil {
		return nil, err
	}

	s.record(ctx, pendingAudit)

	// Enforcement that changed peer eligibility must actually stop traffic.
	if s.Reconciler != nil && rep.ReconcileNeeded() {
		if rrep, err := s.Reconciler.Run(ctx); err != nil {
			rep.Errors = append(rep.Errors, CycleError{Interface: "*", Err: "reconcile after enforcement: " + err.Error()})
		} else {
			rep.Reconciled = rrep
		}
	}

	rep.Duration = s.now().Sub(start)
	return rep, nil
}

func (s *Service) applyShaper(ctx context.Context, ifaces []ifaceRef, rep *CycleReport) {
	groups, err := shaper.LoadGroups(ctx, s.db)
	if err != nil {
		rep.ShaperError = err.Error()
		return
	}
	byIface := map[string][]shaper.Group{}
	for _, g := range groups {
		byIface[g.InterfaceName] = append(byIface[g.InterfaceName], g)
	}
	for _, ifc := range ifaces {
		applied, err := s.shaper.Ensure(ctx, ifc.name, byIface[ifc.name])
		if err != nil {
			rep.ShaperError = err.Error()
			continue
		}
		rep.ShaperApplied = rep.ShaperApplied || applied
	}
}

type ifaceRef struct {
	id   string
	name string
}

func (s *Service) loadEnabledInterfaces(ctx context.Context) ([]ifaceRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM tunnel_interfaces WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("accounting: load interfaces: %w", err)
	}
	defer rows.Close()
	var out []ifaceRef
	for rows.Next() {
		var f ifaceRef
		if err := rows.Scan(&f.id, &f.name); err != nil {
			return nil, fmt.Errorf("accounting: scan interface: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// cycleDevice is one device row as the cycle sees it.
type cycleDevice struct {
	id        string
	pubkey    string
	lastRX    uint64
	lastTX    uint64
	rx        uint64
	tx        uint64
	handshake *time.Time
	endpoint  string
	userID    string
}

// cycleUser carries the user fields the cycle reads and the values it
// computes for the single per-user write.
type cycleUser struct {
	id          string
	username    string
	status      domain.UserStatus
	reason      *domain.DisableReason
	limit       *int64
	usedRX      int64
	usedTX      int64
	activity    *time.Time
	activatedAt *time.Time
	expiresAt   *time.Time
	duration    *int64
	policy      domain.StartPolicy
	deleted     bool

	deltaRX, deltaTX int64
	transition       string // "", "activated", "quota_tripped"
}

// cycleTx loads the device/user rows inside the write transaction (serialized
// against API mutations: the cycle computes from the state it writes to),
// applies the delta invariant, and writes only changed rows.
func (s *Service) cycleTx(ctx context.Context, tx *sql.Tx, observed map[string]tunnel.PeerState, rep *CycleReport, pendingAudit *[]audit.Entry) error {
	devs, users, err := s.loadRows(ctx, tx)
	if err != nil {
		return err
	}
	if len(devs) == 0 {
		return nil
	}

	now := s.now().UTC()
	sampleInterval := s.sampleInterval(ctx)

	devStmt, err := tx.PrepareContext(ctx, `UPDATE devices SET rx_bytes = ?, tx_bytes = ?, last_rx = ?,
		last_tx = ?, last_handshake_at = ?, last_endpoint = ?, updated_at = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("accounting: prepare device update: %w", err)
	}
	defer devStmt.Close()

	for pubkey, p := range observed {
		d, ok := devs[pubkey]
		if !ok {
			continue // unknown peer: the drift policy governs it, not accounting
		}
		u := users[d.userID]

		dRX, dTX := delta(d.lastRX, p.RXBytes), delta(d.lastTX, p.TXBytes)
		newRX, newTX := d.rx+dRX, d.tx+dTX

		handshake := d.handshake
		handshakeChanged := false
		if !p.LastHandshake.IsZero() && (handshake == nil || p.LastHandshake.After(*handshake)) {
			h := p.LastHandshake
			handshake = &h
			handshakeChanged = true
		}
		endpoint := d.endpoint
		if p.Endpoint != "" && p.Endpoint != endpoint {
			endpoint = p.Endpoint
		}

		// Idle devices (no traffic, no newer handshake, same endpoint) are
		// not written at all — bounded churn is a stated resource goal.
		changed := dRX != 0 || dTX != 0 || handshakeChanged || endpoint != d.endpoint
		if !changed {
			continue
		}
		if _, err := devStmt.ExecContext(ctx, int64(newRX), int64(newTX), int64(p.RXBytes), int64(p.TXBytes),
			nullTime(handshake), endpoint, now.Format(time.RFC3339Nano), d.id); err != nil {
			return fmt.Errorf("accounting: device update %s: %w", d.id, err)
		}

		if dRX != 0 || dTX != 0 {
			u.deltaRX += int64(dRX)
			u.deltaTX += int64(dTX)
			rep.Deltas++
			rep.RX += dRX
			rep.TX += dTX
			// Samples bucket by cycle time: the delta was measured now.
			s.samples.push(d.id, now, sampleInterval, dRX, dTX)
		}
		if !p.LastHandshake.IsZero() {
			h := p.LastHandshake
			if u.activity == nil || h.After(*u.activity) {
				u.activity = &h
			}
		}
	}

	userStmt, err := tx.PrepareContext(ctx, `UPDATE users SET traffic_used_rx = ?, traffic_used_tx = ?,
		last_activity_at = ?, status = ?, disable_reason = ?, activated_at = ?, expires_at = ?, updated_at = ?
		WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("accounting: prepare user update: %w", err)
	}
	defer userStmt.Close()

	for _, u := range users {
		// 1. First-connection activation: the first observed handshake starts
		// the subscription (idempotent — only when never activated).
		if !u.deleted && u.status == domain.UserWaitingFirstConnection && u.activity != nil {
			at := now
			u.status = domain.UserActive
			u.activatedAt = &at
			if u.duration != nil {
				exp := at.Add(time.Duration(*u.duration) * time.Second)
				u.expiresAt = &exp
			}
			u.transition = "activated"
			rep.Activated++
			*pendingAudit = append(*pendingAudit, audit.Entry{
				ActorType: audit.ActorSystem, Action: "user.activated", Target: u.id,
				Metadata: map[string]any{"username": u.username},
			})
		}

		// 2. Quota edge: only an unblocked account trips (never re-audit a
		// traffic_exceeded user, never override an admin's explicit choice).
		total := u.usedRX + u.deltaRX + u.usedTX + u.deltaTX
		if !u.deleted && u.limit != nil && total >= *u.limit &&
			(u.status == domain.UserActive || u.status == domain.UserWaitingFirstConnection) {
			u.status = domain.UserTrafficExceeded
			r := domain.DisableTrafficLimit
			u.reason = &r
			u.transition = "quota_tripped"
			rep.QuotaTripped++
			*pendingAudit = append(*pendingAudit, audit.Entry{
				ActorType: audit.ActorSystem, Action: "user.traffic_exceeded", Target: u.id,
				Metadata: map[string]any{"username": u.username, "used_bytes": total, "limit_bytes": *u.limit},
			})
		}

		if u.deltaRX == 0 && u.deltaTX == 0 && u.transition == "" && u.activity == nil {
			continue // untouched user: no write
		}
		var reason any
		if u.reason != nil {
			reason = string(*u.reason)
		}
		if _, err := userStmt.ExecContext(ctx,
			u.usedRX+u.deltaRX, u.usedTX+u.deltaTX,
			nullTime(u.activity), string(u.status), reason,
			nullTime(u.activatedAt), nullTime(u.expiresAt),
			now.Format(time.RFC3339Nano), u.id); err != nil {
			return fmt.Errorf("accounting: user update %s: %w", u.id, err)
		}
	}
	return nil
}

// loadRows reads device+user rows for all enabled interfaces. Soft-deleted
// users' devices are deliberately included: traffic that really flowed is
// counted (their peers should not exist — that is reconcile's business), but
// lifecycle transitions never touch a deleted user.
func (s *Service) loadRows(ctx context.Context, tx *sql.Tx) (map[string]*cycleDevice, map[string]*cycleUser, error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.id, d.public_key, d.last_rx, d.last_tx, d.rx_bytes, d.tx_bytes,
			d.last_handshake_at, d.last_endpoint,
			u.id, u.username, u.status, u.disable_reason, u.traffic_limit_bytes, u.traffic_used_rx,
			u.traffic_used_tx, u.last_activity_at, u.activated_at, u.expires_at, u.duration_seconds,
			u.start_policy, u.deleted_at
		FROM devices d JOIN users u ON u.id = d.user_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("accounting: load rows: %w", err)
	}
	defer rows.Close()

	devs := map[string]*cycleDevice{}
	users := map[string]*cycleUser{}
	for rows.Next() {
		d := &cycleDevice{}
		var (
			u         cycleUser
			handshake sql.NullString
			endpoint  sql.NullString
			status    string
			reason    sql.NullString
			limit     sql.NullInt64
			activity  sql.NullString
			activated sql.NullString
			expires   sql.NullString
			duration  sql.NullInt64
			policy    string
			deleted   sql.NullString
		)
		if err := rows.Scan(&d.id, &d.pubkey, &d.lastRX, &d.lastTX, &d.rx, &d.tx,
			&handshake, &endpoint,
			&u.id, &u.username, &status, &reason, &limit, &u.usedRX, &u.usedTX,
			&activity, &activated, &expires, &duration, &policy, &deleted); err != nil {
			return nil, nil, fmt.Errorf("accounting: scan row: %w", err)
		}
		d.userID = u.id
		if handshake.Valid {
			if t, err := time.Parse(time.RFC3339Nano, handshake.String); err == nil {
				d.handshake = &t
			}
		}
		d.endpoint = endpoint.String

		if _, ok := users[u.id]; !ok {
			u.status = domain.UserStatus(status)
			if reason.Valid {
				r := domain.DisableReason(reason.String)
				u.reason = &r
			}
			if limit.Valid {
				v := limit.Int64
				u.limit = &v
			}
			u.activity = parseTimePtr(activity)
			u.activatedAt = parseTimePtr(activated)
			u.expiresAt = parseTimePtr(expires)
			if duration.Valid {
				v := duration.Int64
				u.duration = &v
			}
			u.policy = domain.StartPolicy(policy)
			u.deleted = deleted.Valid
			users[u.id] = &u
		}
		devs[d.pubkey] = d
	}
	return devs, users, rows.Err()
}

// delta applies the accounting invariant: a decreasing counter is a reset —
// count current from zero and re-baseline (never a negative delta).
func delta(last, cur uint64) uint64 {
	if cur < last {
		return cur
	}
	return cur - last
}

// ExpiryReport summarizes one expiry pass.
type ExpiryReport struct {
	Expired    int
	Reconciled *reconcile.Report
	Duration   time.Duration
}

// EnforceExpiry expires every live user whose expiration has passed (a
// set-based pass over `active`/`waiting_first_connection` rows past
// `expires_at`). Accounts already blocked (traffic_exceeded, disabled,
// suspended) keep their status: renewal semantics differ per status and the
// admin's chosen block must not be silently replaced.
func (s *Service) EnforceExpiry(ctx context.Context) (*ExpiryReport, error) {
	start := s.now()
	rep := &ExpiryReport{}
	now := s.now().UTC().Format(time.RFC3339Nano)

	type due struct {
		id, username string
	}
	var dueUsers []due

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, username FROM users
			WHERE deleted_at IS NULL AND expires_at IS NOT NULL AND expires_at < ?
			  AND status IN ('active', 'waiting_first_connection')`, now)
		if err != nil {
			return fmt.Errorf("accounting: expiry select: %w", err)
		}
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.username); err != nil {
				rows.Close()
				return fmt.Errorf("accounting: expiry scan: %w", err)
			}
			dueUsers = append(dueUsers, d)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("accounting: expiry scan: %w", err)
		}
		rows.Close()

		if len(dueUsers) == 0 {
			return nil
		}
		res, err := tx.ExecContext(ctx, `UPDATE users SET status = 'expired', disable_reason = 'expired',
			updated_at = ?
			WHERE deleted_at IS NULL AND expires_at IS NOT NULL AND expires_at < ?
			  AND status IN ('active', 'waiting_first_connection')`,
			now, now)
		if err != nil {
			return fmt.Errorf("accounting: expiry update: %w", err)
		}
		n, _ := res.RowsAffected()
		rep.Expired = int(n)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if rep.Expired > 0 {
		if s.audit != nil {
			for _, d := range dueUsers {
				_ = s.audit.Record(ctx, audit.Entry{
					ActorType: audit.ActorSystem, Action: "user.expired", Target: d.id,
					Metadata: map[string]any{"username": d.username},
				})
			}
		}
		// Expired users lose their peers (PeerWanted() == false).
		if s.Reconciler != nil {
			if rrep, err := s.Reconciler.Run(ctx); err != nil {
				return rep, fmt.Errorf("accounting: reconcile after expiry: %w", err)
			} else {
				rep.Reconciled = rrep
			}
		}
	}

	rep.Duration = s.now().Sub(start)
	return rep, nil
}

// Actor-system traffic mutations (requirements §Lifecycle: reset/add/remove
// traffic). Usage lives on the USER (the charged counter); device totals are
// per-device observations. The two are intentionally independent: manual
// corrections adjust the charged counter without rewriting history.

// ResetTraffic zeroes the user's charged usage and every device total, and
// reactivates a traffic_exceeded account (one-op unblock). Baselines are
// kept, so the next cycle continues cleanly — no double counting.
func (s *Service) ResetTraffic(ctx context.Context, userID string, actor Actor) error {
	var username string
	var resetDevices []string
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var (
			status, policy string
			activated      sql.NullString
		)
		err := tx.QueryRowContext(ctx, `SELECT username, status, start_policy, activated_at
			FROM users WHERE id = ? AND deleted_at IS NULL`, userID).
			Scan(&username, &status, &policy, &activated)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.E(domain.CodeUserNotFound, "user %s not found", userID)
		}
		if err != nil {
			return fmt.Errorf("accounting: reset lookup: %w", err)
		}

		// Reactivation (one-op unblock): a never-activated first_connection
		// account returns to waiting, not active.
		newStatus := domain.UserStatus(status)
		var reason any
		if domain.UserStatus(status) == domain.UserTrafficExceeded {
			if !activated.Valid && domain.StartPolicy(policy) == domain.StartFirstConnection {
				newStatus = domain.UserWaitingFirstConnection
			} else {
				newStatus = domain.UserActive
			}
			reason = nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET traffic_used_rx = 0, traffic_used_tx = 0,
			status = ?, disable_reason = ?, updated_at = ? WHERE id = ?`,
			string(newStatus), reason, s.now().UTC().Format(time.RFC3339Nano), userID); err != nil {
			return fmt.Errorf("accounting: reset user: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET rx_bytes = 0, tx_bytes = 0 WHERE user_id = ?`, userID); err != nil {
			return fmt.Errorf("accounting: reset devices: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM devices WHERE user_id = ?`, userID)
		if err != nil {
			return fmt.Errorf("accounting: reset device list: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("accounting: reset device list: %w", err)
			}
			resetDevices = append(resetDevices, id)
		}
		return rows.Close()
	})
	if err != nil {
		return err
	}
	s.samples.clearDevices(resetDevices)
	s.record(ctx, []audit.Entry{{
		ActorType: actor.typ(), ActorID: actor.ID, Action: "user.traffic_reset", Target: userID,
		Metadata: map[string]any{"username": username},
	}})
	return nil
}

// AddTraffic adds bytes to the charged counter (top-up style corrections);
// a push over the limit trips the account immediately (same edge as the
// cycle).
func (s *Service) AddTraffic(ctx context.Context, userID string, bytes int64, actor Actor) error {
	if bytes <= 0 {
		return domain.E(domain.CodeInvalidRequest, "bytes must be positive")
	}
	return s.adjustTraffic(ctx, userID, bytes, "user.traffic_added", actor)
}

// RemoveTraffic subtracts bytes from the charged counter (rx first, then tx,
// floored at zero); a traffic_exceeded account that drops below its limit is
// reactivated (one-op unblock).
func (s *Service) RemoveTraffic(ctx context.Context, userID string, bytes int64, actor Actor) error {
	if bytes <= 0 {
		return domain.E(domain.CodeInvalidRequest, "bytes must be positive")
	}
	return s.adjustTraffic(ctx, userID, -bytes, "user.traffic_removed", actor)
}

func (s *Service) adjustTraffic(ctx context.Context, userID string, deltaBytes int64, action string, actor Actor) error {
	var username string
	var auditExtra map[string]any
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var (
			status, policy string
			usedRX, usedTX int64
			limit          sql.NullInt64
			activated      sql.NullString
		)
		err := tx.QueryRowContext(ctx, `SELECT username, status, traffic_used_rx, traffic_used_tx,
			traffic_limit_bytes, start_policy, activated_at
			FROM users WHERE id = ? AND deleted_at IS NULL`, userID).
			Scan(&username, &status, &usedRX, &usedTX, &limit, &policy, &activated)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.E(domain.CodeUserNotFound, "user %s not found", userID)
		}
		if err != nil {
			return fmt.Errorf("accounting: adjust lookup: %w", err)
		}

		var newRX, newTX int64
		if deltaBytes >= 0 {
			newRX, newTX = usedRX, usedTX
			if deltaBytes > math.MaxInt64-newRX {
				newRX = math.MaxInt64 // saturate, never wrap
			} else {
				newRX += deltaBytes
			}
		} else {
			// Remove: rx first, then tx, floored at zero.
			newRX = usedRX + deltaBytes
			if newRX < 0 {
				newTX = usedTX + newRX
				newRX = 0
				if newTX < 0 {
					newTX = 0
				}
			} else {
				newTX = usedTX
			}
		}
		total := newRX + newTX

		newStatus := domain.UserStatus(status)
		var reason any
		st := newStatus
		live := st == domain.UserActive || st == domain.UserWaitingFirstConnection
		overLimit := limit.Valid && total >= limit.Int64
		switch {
		case live && overLimit:
			newStatus = domain.UserTrafficExceeded
			r := domain.DisableTrafficLimit
			reason = &r
		case st == domain.UserTrafficExceeded && limit.Valid && total < limit.Int64:
			// Usage dropped below the limit: one-op unblock.
			if !activated.Valid && domain.StartPolicy(policy) == domain.StartFirstConnection {
				newStatus = domain.UserWaitingFirstConnection
			} else {
				newStatus = domain.UserActive
			}
			reason = nil
		}

		if _, err := tx.ExecContext(ctx, `UPDATE users SET traffic_used_rx = ?, traffic_used_tx = ?,
			status = ?, disable_reason = ?, updated_at = ? WHERE id = ?`,
			newRX, newTX, string(newStatus), reason,
			s.now().UTC().Format(time.RFC3339Nano), userID); err != nil {
			return fmt.Errorf("accounting: adjust user: %w", err)
		}
		auditExtra = map[string]any{"username": username, "bytes": deltaBytes, "used_bytes": total}
		return nil
	})
	if err != nil {
		return err
	}
	s.record(ctx, []audit.Entry{{
		ActorType: actor.typ(), ActorID: actor.ID, Action: action, Target: userID,
		Metadata: auditExtra,
	}})
	return nil
}

// sampleInterval reads the flush cadence (nil registry → default).
func (s *Service) sampleInterval(ctx context.Context) time.Duration {
	const def = 300 * time.Second
	if s.reg == nil {
		return def
	}
	n, err := s.reg.GetInt(ctx, "accounting.sample_flush_seconds")
	if err != nil || n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}

func (s *Service) record(ctx context.Context, entries []audit.Entry) {
	if s.audit == nil {
		return
	}
	for _, e := range entries {
		_ = s.audit.Record(ctx, e)
	}
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTimePtr(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return nil
	}
	return &t
}
