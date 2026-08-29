package accounting

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

// reconcilerFunc adapts a function to the Reconciler interface.
type reconcilerFunc func(ctx context.Context) (*reconcile.Report, error)

func (f reconcilerFunc) Run(ctx context.Context) (*reconcile.Report, error) { return f(ctx) }

// ---------------------------------------------------------------------------
// Test environment: real SQLite (temp file) + fake backend. Devices/users are
// seeded directly so tests control baselines exactly.
// ---------------------------------------------------------------------------

const (
	ifaceID   = "ifc-1"
	ifaceName = "awg0"
	subnet    = "10.8.0.0/22" // wide pool: the 1000-device benchmark must fit
)

type env struct {
	db      *database.DB
	backend *fake.Backend
	svc     *Service
	now     time.Time // mutable fake clock
	ipSeq   int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "acc.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), slog.Default()); err != nil {
		t.Fatal(err)
	}
	backend := fake.New()
	e := &env{db: db, backend: backend, now: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	e.svc = NewService(db, backend, audit.NewService(db), nil, nil)
	e.svc.now = func() time.Time { return e.now }
	if err := backend.CreateInterface(context.Background(), tunnel.InterfaceSpec{Name: ifaceName, ListenPort: 40001}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnel_interfaces
		(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted, preset_name, enabled, backend_mode, created_at, updated_at)
		VALUES (?, ?, 40001, ?, 1420, 'srvpub', x'00', 'plain', 1, 'kernel', ?, ?)`,
		ifaceID, ifaceName, subnet, e.now.Format(time.RFC3339Nano), e.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return e
}

// seedUser inserts a user row with the given state and returns its ID.
func (e *env) seedUser(t *testing.T, username string, status string, limit *int64, policy string) string {
	t.Helper()
	id := "usr-" + username
	expires := any(nil)
	if policy == "" {
		policy = "immediate"
	}
	activated := any(nil)
	if status == "active" {
		activated = e.now.Format(time.RFC3339Nano)
	}
	if status == "active" {
		exp := e.now.Add(30 * 24 * time.Hour).Format(time.RFC3339Nano)
		expires = exp
	}
	if _, err := e.db.Exec(`INSERT INTO users
		(id, username, status, traffic_limit_bytes, start_policy, activated_at, expires_at, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		id, username, status, limit, policy, activated, expires,
		e.now.Format(time.RFC3339Nano), e.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return id
}

// seedDevice inserts a device row (explicit baselines) and registers the
// peer on the fake backend.
func (e *env) seedDevice(t *testing.T, userID, name, pubkey string, lastRX, lastTX uint64) string {
	t.Helper()
	id := "dev-" + name
	ip := e.nextIP(t)
	if _, err := e.db.Exec(`INSERT INTO devices
		(id, user_id, interface_id, name, ipv4_address, public_key, private_key_encrypted, enabled,
		 rx_bytes, tx_bytes, last_rx, last_tx, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, x'00', 1, 0, 0, ?, ?, ?, ?)`,
		id, userID, ifaceID, name, ip, pubkey, lastRX, lastTX,
		e.now.Format(time.RFC3339Nano), e.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.backend.SyncPeers(ctx, ifaceName, []tunnel.PeerConfig{{PublicKey: pubkey, AllowedIPs: []string{ip}}}); err != nil {
		t.Fatal(err)
	}
	return id
}

// nextIP hands out device addresses (.2, .3, …) — mirrors the allocator.
func (e *env) nextIP(t *testing.T) string {
	t.Helper()
	e.ipSeq++
	ip := fmt.Sprintf("10.8.0.%d/32", e.ipSeq+1)
	return ip
}

func (e *env) setActivity(t *testing.T, pubkey string, handshake time.Time, rx, tx uint64) {
	t.Helper()
	if err := e.backend.SetPeerActivity(ifaceName, pubkey, handshake, rx, tx); err != nil {
		t.Fatal(err)
	}
}

func (e *env) deviceRow(t *testing.T, id string) (rx, tx, lastRX, lastTX uint64, updated string) {
	t.Helper()
	err := e.db.QueryRow(`SELECT rx_bytes, tx_bytes, last_rx, last_tx, updated_at FROM devices WHERE id = ?`, id).
		Scan(&rx, &tx, &lastRX, &lastTX, &updated)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func (e *env) userRow(t *testing.T, id string) (usedRX, usedTX int64, status, reason string) {
	t.Helper()
	err := e.db.QueryRow(`SELECT traffic_used_rx, traffic_used_tx, status, COALESCE(disable_reason,'') FROM users WHERE id = ?`, id).
		Scan(&usedRX, &usedTX, &status, &reason)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func (e *env) auditCount(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func cycle(t *testing.T, e *env) *CycleReport {
	t.Helper()
	rep, err := e.svc.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// ---------------------------------------------------------------------------
// Delta invariant
// ---------------------------------------------------------------------------

const (
	keyA = "devAKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	keyB = "devBKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
)

func TestCycleBasicDeltaAndAccumulation(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "alice", "active", nil, "immediate")
	did := e.seedDevice(t, uid, "phone", keyA, 0, 0)

	// Cycle 1: counters seen for the first time (baseline 0).
	hs := e.now.Add(-time.Minute)
	e.setActivity(t, keyA, hs, 1000, 400)
	rep := cycle(t, e)
	if rep.Deltas != 1 || rep.RX != 1000 || rep.TX != 400 {
		t.Fatalf("cycle 1: %+v", rep)
	}
	rx, tx, lastRX, lastTX, _ := e.deviceRow(t, did)
	if rx != 1000 || tx != 400 || lastRX != 1000 || lastTX != 400 {
		t.Fatalf("device totals: %d/%d baselines %d/%d", rx, tx, lastRX, lastTX)
	}
	usedRX, usedTX, _, _ := e.userRow(t, uid)
	if usedRX != 1000 || usedTX != 400 {
		t.Fatalf("user usage: %d/%d", usedRX, usedTX)
	}

	// Cycle 2: counters grow → only the delta is added.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 3000, 400)
	cycle(t, e)
	rx, _, _, _, _ = e.deviceRow(t, did)
	if rx != 3000 {
		t.Fatalf("device total after growth: %d", rx)
	}
	usedRX, usedTX, _, _ = e.userRow(t, uid)
	if usedRX != 3000 || usedTX != 400 {
		t.Fatalf("user usage after growth: %d/%d", usedRX, usedTX)
	}
}

func TestCycleCounterResetNeverNegative(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "bob", "active", nil, "immediate")
	did := e.seedDevice(t, uid, "laptop", keyA, 5000, 7000)

	// Baseline established with earlier traffic already in the totals.
	if _, err := e.db.Exec(`UPDATE devices SET rx_bytes = 5000, tx_bytes = 7000 WHERE id = ?`, did); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE users SET traffic_used_rx = 5000, traffic_used_tx = 7000 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}

	// Counter reset (link recreate / reboot): new counters BELOW baseline →
	// count current from zero, re-baseline, never a negative delta.
	e.setActivity(t, keyA, e.now.Add(-time.Second), 300, 0)
	rep := cycle(t, e)
	if rep.RX != 300 || rep.TX != 0 {
		t.Fatalf("reset deltas: %+v", rep)
	}
	rx, tx, lastRX, lastTX, _ := e.deviceRow(t, did)
	if rx != 5300 || tx != 7000 || lastRX != 300 || lastTX != 0 {
		t.Fatalf("after reset: totals %d/%d baselines %d/%d", rx, tx, lastRX, lastTX)
	}
	usedRX, usedTX, _, _ := e.userRow(t, uid)
	if usedRX != 5300 || usedTX != 7000 {
		t.Fatalf("user usage after reset: %d/%d", usedRX, usedTX)
	}
}

func TestCycleRestartRecovery(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "carol", "active", nil, "immediate")
	did := e.seedDevice(t, uid, "phone", keyA, 0, 0)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 1000, 1000)
	cycle(t, e)

	// "Restart": a fresh Service over the same database (the process died and
	// came back; accumulated usage lives in SQLite, not in counters).
	e.now = e.now.Add(10 * time.Minute)
	svc2 := NewService(e.db, e.backend, audit.NewService(e.db), nil, nil)
	svc2.now = e.svc.now

	// Kernel-continue case: counters kept growing while wg-guard was down →
	// the down-time traffic must be counted (no usage loss).
	e.setActivity(t, keyA, e.now.Add(-time.Second), 4000, 1500)
	rep, err := svc2.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.RX != 3000 || rep.TX != 500 {
		t.Fatalf("restart continue deltas: %+v", rep)
	}
	usedRX, usedTX, _, _ := e.userRow(t, uid)
	if usedRX != 4000 || usedTX != 1500 {
		t.Fatalf("usage after restart: %d/%d", usedRX, usedTX)
	}

	// Userspace-reset case: the daemon restarted and recreated the link →
	// counters back to zero → reset path, totals preserved.
	e.now = e.now.Add(10 * time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 0, 0)
	if _, err := svc2.RunCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	usedRX, usedTX, _, _ = e.userRow(t, uid)
	if usedRX != 4000 || usedTX != 1500 {
		t.Fatalf("usage must survive a counter reset: %d/%d", usedRX, usedTX)
	}
	_, _, lastRX, lastTX, _ := e.deviceRow(t, did)
	if lastRX != 0 || lastTX != 0 {
		t.Fatalf("baselines not re-set: %d/%d", lastRX, lastTX)
	}
}

func TestCycleIdleDeviceNotWritten(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "dave", "active", nil, "immediate")
	did := e.seedDevice(t, uid, "idle", keyA, 0, 0)
	e.setActivity(t, keyA, e.now.Add(-time.Minute), 100, 50)
	cycle(t, e)
	_, _, _, _, updated1 := e.deviceRow(t, did)

	// Same counters, same handshake → nothing written.
	e.now = e.now.Add(time.Hour)
	cycle(t, e)
	_, _, _, _, updated2 := e.deviceRow(t, did)
	if updated1 != updated2 {
		t.Fatalf("idle device was rewritten: %s → %s", updated1, updated2)
	}
}

func TestCyclePeerReaddResetsBaseline(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "erin", "active", nil, "immediate")
	did := e.seedDevice(t, uid, "tab", keyA, 0, 0)
	e.setActivity(t, keyA, e.now.Add(-time.Minute), 900, 0)
	cycle(t, e)

	// Peer removed and re-added by reconciliation → counters zeroed.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 0, 0)
	cycle(t, e)
	// Back online, counters grow from zero again.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 250, 0)
	cycle(t, e)

	rx, _, _, _, _ := e.deviceRow(t, did)
	if rx != 1150 { // 900 + 0 (reset) + 250
		t.Fatalf("device total after peer re-add: %d", rx)
	}
}

// ---------------------------------------------------------------------------
// Quota enforcement (edge-triggered) + audit
// ---------------------------------------------------------------------------

func TestQuotaTripAndNoDoubleTrip(t *testing.T) {
	e := newEnv(t)
	limit := int64(1000)
	uid := e.seedUser(t, "frank", "active", &limit, "immediate")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)

	e.setActivity(t, keyA, e.now.Add(-time.Second), 600, 400) // exactly at limit
	rep := cycle(t, e)
	if rep.QuotaTripped != 1 {
		t.Fatalf("quota must trip at exactly the limit: %+v", rep)
	}
	_, _, status, reason := e.userRow(t, uid)
	if status != "traffic_exceeded" || reason != "traffic_limit" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
	if n := e.auditCount(t, "user.traffic_exceeded"); n != 1 {
		t.Fatalf("audit entries: %d", n)
	}

	// Still over → deltas still counted, no new trip, no new audit.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 1600, 400)
	rep = cycle(t, e)
	if rep.QuotaTripped != 0 {
		t.Fatalf("must not re-trip: %+v", rep)
	}
	usedRX, usedTX, status, _ := e.userRow(t, uid)
	if usedRX != 1600 || usedTX != 400 || status != "traffic_exceeded" {
		t.Fatalf("usage/status after continued overage: %d/%d %q", usedRX, usedTX, status)
	}
	if n := e.auditCount(t, "user.traffic_exceeded"); n != 1 {
		t.Fatalf("audit entries after re-cycle: %d", n)
	}
}

func TestQuotaSkipsAdminBlockedAndDeletedUsers(t *testing.T) {
	e := newEnv(t)
	limit := int64(100)
	uid := e.seedUser(t, "gina", "disabled", &limit, "immediate")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 500, 0)
	rep := cycle(t, e)
	if rep.QuotaTripped != 0 {
		t.Fatalf("admin-blocked user must not be re-labelled: %+v", rep)
	}
	_, _, status, reason := e.userRow(t, uid)
	if status != "disabled" || reason != "" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
	// Usage still counted.
	usedRX, _, _, _ := e.userRow(t, uid)
	if usedRX != 500 {
		t.Fatalf("usage not counted: %d", usedRX)
	}

	// Deleted user: counted, never transitioned.
	e2 := newEnv(t)
	uid2 := e2.seedUser(t, "hank", "active", &limit, "immediate")
	e2.seedDevice(t, uid2, "phone", keyA, 0, 0)
	now := e2.now.Format(time.RFC3339Nano)
	if _, err := e2.db.Exec(`UPDATE users SET deleted_at = ? WHERE id = ?`, now, uid2); err != nil {
		t.Fatal(err)
	}
	e2.setActivity(t, keyA, e2.now.Add(-time.Second), 500, 0)
	rep = cycle(t, e2)
	if rep.QuotaTripped != 0 || rep.Deltas != 1 {
		t.Fatalf("deleted-user cycle: %+v", rep)
	}
	usedRX, _, status, _ = e2.userRow(t, uid2)
	if usedRX != 500 || status != "active" {
		t.Fatalf("deleted user: usage %d status %q", usedRX, status)
	}
}

// ---------------------------------------------------------------------------
// First-connection activation
// ---------------------------------------------------------------------------

func TestFirstConnectionActivation(t *testing.T) {
	e := newEnv(t)
	duration := int64(3600)
	uid := e.seedUser(t, "ivan", "waiting_first_connection", nil, "first_connection")
	if _, err := e.db.Exec(`UPDATE users SET duration_seconds = ? WHERE id = ?`, duration, uid); err != nil {
		t.Fatal(err)
	}
	did := e.seedDevice(t, uid, "phone", keyA, 0, 0)

	// No handshake yet → untouched.
	cycle(t, e)
	_, _, status, _ := e.userRow(t, uid)
	if status != "waiting_first_connection" {
		t.Fatalf("premature activation: %q", status)
	}

	// First handshake → activation: activated_at now, expires now+duration.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 10, 10)
	rep := cycle(t, e)
	if rep.Activated != 1 {
		t.Fatalf("activation: %+v", rep)
	}
	_, _, status, _ = e.userRow(t, uid)
	if status != "active" {
		t.Fatalf("status after activation: %q", status)
	}
	var activated, expires string
	if err := e.db.QueryRow(`SELECT activated_at, expires_at FROM users WHERE id = ?`, uid).
		Scan(&activated, &expires); err != nil {
		t.Fatal(err)
	}
	activatedAt, err1 := time.Parse(time.RFC3339Nano, activated)
	expiresAt, err2 := time.Parse(time.RFC3339Nano, expires)
	if err1 != nil || err2 != nil {
		t.Fatalf("parse: %v %v", err1, err2)
	}
	if !activatedAt.Equal(e.now) {
		t.Fatalf("activated_at = %v, want %v", activatedAt, e.now)
	}
	if want := e.now.Add(time.Duration(duration) * time.Second); !expiresAt.Equal(want) {
		t.Fatalf("expires_at = %v, want %v", expiresAt, want)
	}

	// Idempotent: a later cycle does not re-activate or move expires_at.
	e.now = e.now.Add(time.Hour)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 20, 0)
	rep = cycle(t, e)
	if rep.Activated != 0 {
		t.Fatalf("double activation: %+v", rep)
	}
	var expires2 string
	if err := e.db.QueryRow(`SELECT expires_at FROM users WHERE id = ?`, uid).Scan(&expires2); err != nil {
		t.Fatal(err)
	}
	if expires2 != expires {
		t.Fatalf("expires_at moved: %v → %v", expires, expires2)
	}
	_ = did
}

func TestActivationAndQuotaSameCycle(t *testing.T) {
	e := newEnv(t)
	limit := int64(100)
	uid := e.seedUser(t, "jade", "waiting_first_connection", &limit, "first_connection")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)

	e.setActivity(t, keyA, e.now.Add(-time.Second), 150, 0)
	rep := cycle(t, e)
	if rep.Activated != 1 || rep.QuotaTripped != 1 {
		t.Fatalf("activation+trip in one cycle: %+v", rep)
	}
	_, _, status, reason := e.userRow(t, uid)
	if status != "traffic_exceeded" || reason != "traffic_limit" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}

// ---------------------------------------------------------------------------
// Expiry enforcement
// ---------------------------------------------------------------------------

func TestEnforceExpiry(t *testing.T) {
	e := newEnv(t)
	past := e.now.Add(-time.Hour).Format(time.RFC3339Nano)
	future := e.now.Add(time.Hour).Format(time.RFC3339Nano)

	mk := func(name, status, expires string) string {
		uid := e.seedUser(t, name, status, nil, "immediate")
		var q string
		if expires == "" {
			q = `UPDATE users SET expires_at = NULL WHERE id = ?`
		} else {
			q = `UPDATE users SET expires_at = ? WHERE id = ?`
		}
		if _, err := e.db.Exec(q, expires, uid); err != nil {
			t.Fatal(err)
		}
		return uid
	}
	activePast := mk("kal", "active", past)
	waitPast := mk("leme", "waiting_first_connection", past)
	activeFuture := mk("mike", "active", future)
	exceededPast := mk("nell", "traffic_exceeded", past)
	if _, err := e.db.Exec(`UPDATE users SET disable_reason = 'traffic_limit' WHERE id = ?`, exceededPast); err != nil {
		t.Fatal(err)
	}
	deletedPast := mk("otto", "active", past)
	if _, err := e.db.Exec(`UPDATE users SET deleted_at = ? WHERE id = ?`, past, deletedPast); err != nil {
		t.Fatal(err)
	}
	noExpiry := mk("pia", "active", "")

	rep, err := e.svc.EnforceExpiry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Expired != 2 {
		t.Fatalf("expired count: %+v", rep)
	}
	_, _, status, reason := e.userRow(t, activePast)
	if status != "expired" || reason != "expired" {
		t.Fatalf("activePast: %q/%q", status, reason)
	}
	_, _, status, _ = e.userRow(t, waitPast)
	if status != "expired" {
		t.Fatalf("waiting user past expiry must expire: %q", status)
	}
	_, _, status, _ = e.userRow(t, activeFuture)
	if status != "active" {
		t.Fatalf("future expiry must not fire: %q", status)
	}
	_, _, status, reason = e.userRow(t, exceededPast)
	if status != "traffic_exceeded" || reason != "traffic_limit" {
		t.Fatalf("already-blocked user must keep its status: %q/%q", status, reason)
	}
	_, _, status, _ = e.userRow(t, deletedPast)
	if status != "active" {
		t.Fatalf("deleted user must be untouched: %q", status)
	}
	_, _, status, _ = e.userRow(t, noExpiry)
	if status != "active" {
		t.Fatalf("no-expiry user must be untouched: %q", status)
	}
	if n := e.auditCount(t, "user.expired"); n != 2 {
		t.Fatalf("expiry audit: %d", n)
	}
}

// ---------------------------------------------------------------------------
// Traffic mutations
// ---------------------------------------------------------------------------

func TestResetTraffic(t *testing.T) {
	e := newEnv(t)
	limit := int64(1000)
	uid := e.seedUser(t, "quin", "active", &limit, "immediate")
	did := e.seedDevice(t, uid, "phone", keyA, 0, 0)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 1500, 500)
	cycle(t, e) // trips quota (2000 ≥ 1000)
	if _, _, status, _ := e.userRow(t, uid); status != "traffic_exceeded" {
		t.Fatalf("setup: %q", status)
	}

	if err := e.svc.ResetTraffic(context.Background(), uid, Actor{}); err != nil {
		t.Fatal(err)
	}
	usedRX, usedTX, status, reason := e.userRow(t, uid)
	if usedRX != 0 || usedTX != 0 || status != "active" || reason != "" {
		t.Fatalf("after reset: %d/%d %q/%q", usedRX, usedTX, status, reason)
	}
	rx, tx, lastRX, lastTX, _ := e.deviceRow(t, did)
	if rx != 0 || tx != 0 || lastRX != 1500 || lastTX != 500 {
		t.Fatalf("device after reset: totals %d/%d baselines %d/%d", rx, tx, lastRX, lastTX)
	}
	if n := e.auditCount(t, "user.traffic_reset"); n != 1 {
		t.Fatalf("reset audit: %d", n)
	}

	// Baselines intact: the next cycle counts only new traffic.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 1600, 500)
	cycle(t, e)
	usedRX, _, _, _ = e.userRow(t, uid)
	if usedRX != 100 {
		t.Fatalf("usage after reset+cycle: %d, want 100 (no double count)", usedRX)
	}
}

func TestResetTrafficReturnsWaitingForUnactivated(t *testing.T) {
	e := newEnv(t)
	uid := e.seedUser(t, "romeo", "waiting_first_connection", nil, "first_connection")
	// Force a traffic_exceeded status on a never-activated account.
	if _, err := e.db.Exec(`UPDATE users SET status = 'traffic_exceeded', disable_reason = 'traffic_limit' WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ResetTraffic(context.Background(), uid, Actor{}); err != nil {
		t.Fatal(err)
	}
	_, _, status, reason := e.userRow(t, uid)
	if status != "waiting_first_connection" || reason != "" {
		t.Fatalf("after reset: %q/%q", status, reason)
	}
}

func TestAddRemoveTraffic(t *testing.T) {
	e := newEnv(t)
	limit := int64(1000)
	uid := e.seedUser(t, "sara", "active", &limit, "immediate")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)

	// Add below limit.
	if err := e.svc.AddTraffic(context.Background(), uid, 400, Actor{Type: "admin", ID: "a1"}); err != nil {
		t.Fatal(err)
	}
	usedRX, usedTX, status, _ := e.userRow(t, uid)
	if usedRX != 400 || status != "active" {
		t.Fatalf("after add: %d %q", usedRX, status)
	}
	// Add pushing over limit trips immediately.
	if err := e.svc.AddTraffic(context.Background(), uid, 700, Actor{}); err != nil {
		t.Fatal(err)
	}
	_, _, status, reason := e.userRow(t, uid)
	if status != "traffic_exceeded" || reason != "traffic_limit" {
		t.Fatalf("after trip-add: %q/%q", status, reason)
	}
	// Remove below limit → one-op unblock.
	if err := e.svc.RemoveTraffic(context.Background(), uid, 200, Actor{}); err != nil {
		t.Fatal(err)
	}
	usedRX, _, status, reason = e.userRow(t, uid)
	if usedRX != 900 || status != "active" || reason != "" {
		t.Fatalf("after remove: %d %q/%q", usedRX, status, reason)
	}
	// Clamp at zero.
	if err := e.svc.RemoveTraffic(context.Background(), uid, 99999, Actor{}); err != nil {
		t.Fatal(err)
	}
	usedRX, usedTX, _, _ = e.userRow(t, uid)
	if usedRX != 0 || usedTX != 0 {
		t.Fatalf("clamp: %d/%d", usedRX, usedTX)
	}
	// Invalid input rejected.
	if err := e.svc.AddTraffic(context.Background(), uid, 0, Actor{}); err == nil {
		t.Fatal("zero bytes must be rejected")
	}
	if err := e.svc.AddTraffic(context.Background(), "usr-nope", 100, Actor{}); err == nil {
		t.Fatal("unknown user must be rejected")
	}
	if n := e.auditCount(t, "user.traffic_added"); n != 2 {
		t.Fatalf("add audit: %d", n)
	}
	if n := e.auditCount(t, "user.traffic_removed"); n != 2 {
		t.Fatalf("remove audit: %d", n)
	}
}

// ---------------------------------------------------------------------------
// Reconciler trigger + shaper wiring
// ---------------------------------------------------------------------------

func TestReconcilerRunsOnTransitions(t *testing.T) {
	e := newEnv(t)
	limit := int64(100)
	uid := e.seedUser(t, "tara", "active", &limit, "immediate")
	e.seedDevice(t, uid, "phone", keyA, 0, 0)

	runs := 0
	e.svc.Reconciler = reconcilerFunc(func(context.Context) (*reconcile.Report, error) {
		runs++
		return &reconcile.Report{}, nil
	})

	e.setActivity(t, keyA, e.now.Add(-time.Second), 500, 0)
	if _, err := e.svc.RunCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("quota trip must trigger reconcile, runs=%d", runs)
	}
	// A quiet cycle does not reconcile.
	e.now = e.now.Add(time.Minute)
	e.setActivity(t, keyA, e.now.Add(-time.Second), 500, 0)
	if _, err := e.svc.RunCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("quiet cycle must not reconcile, runs=%d", runs)
	}
	// Expiry triggers too: re-arm tara as active with a passed expiration.
	if _, err := e.db.Exec(`UPDATE users SET status = 'active', disable_reason = NULL, expires_at = ? WHERE id = ?`,
		e.now.Add(-time.Second).Format(time.RFC3339Nano), uid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.EnforceExpiry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Fatalf("expiry must trigger reconcile, runs=%d", runs)
	}
}
