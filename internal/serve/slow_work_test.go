package serve

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/scheduler"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func TestSlowWorkDoesNotDelayCentralEnforcementAndCoalescesRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	finish := make(chan struct{})
	var calls atomic.Int32
	work := startSlowWork(ctx, quietLogger(), "fixture", time.Minute, func(ctx context.Context) error {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	defer work.stop(context.Background())
	_ = work.request(ctx)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow job did not start")
	}
	for range 100 {
		_ = work.request(ctx)
	}
	enforced := make(chan struct{}, 1)
	sched := scheduler.New(quietLogger())
	sched.At("slow", time.Now(), work.request)
	sched.At("enforcement", time.Now(), func(context.Context) error { enforced <- struct{}{}; return nil })
	sched.Start(ctx)
	defer sched.Stop()
	select {
	case <-enforced:
	case <-time.After(time.Second):
		t.Fatal("stalled I/O delayed central enforcement")
	}
	if calls.Load() != 1 || len(work.wake) != 1 {
		t.Fatal("slow job overlapped itself or queued unlimited work")
	}
	close(finish)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("coalesced durable-work notification was lost")
	}
}

func TestSlowWorkShutdownIsBoundedAndRetryable(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	work := startSlowWork(context.Background(), quietLogger(), "fixture", time.Minute, func(context.Context) error {
		close(started)
		<-release // Deliberately non-cooperative operation.
		return nil
	})
	_ = work.request(context.Background())
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := work.stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("non-cooperative job bypassed shutdown deadline")
	}
	close(release)
	if err := work.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownPreservesDatabaseAndOwnershipUntilSlowWorkDrains(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:0")
	service := &backup.Service{Cfg: cfg}
	lease, err := service.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	started, release := make(chan struct{}), make(chan struct{})
	work := startSlowWork(context.Background(), quietLogger(), "fixture", time.Minute, func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	released := false
	defer func() {
		if !released {
			close(release)
		}
		_ = work.stop(context.Background())
	}()
	_ = work.request(context.Background())
	<-started
	n := &Node{db: db, dataLease: lease, slowWork: []*slowWork{work}}
	n.booted.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := n.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown ignored its unfinished slow worker")
	}
	if n.booted.Load() || db.PingContext(context.Background()) != nil {
		t.Fatal("draining node stayed ready or closed its active database")
	}
	if contender, err := service.OpenData(true); err == nil {
		contender.Close()
		t.Fatal("replacement admitted before slow work drained")
	}
	close(release)
	released = true
	if err := n.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	contender, err := service.OpenData(true)
	if err != nil {
		t.Fatal("successful drain leaked data ownership", err)
	}
	contender.Close()
}

func TestStalledSlowPassesDoNotDelayQuotaAndExpiryEnforcement(t *testing.T) {
	ctx := context.Background()
	backend := fake.New()
	n, err := Start(ctx, Options{Config: testConfig(t, "127.0.0.1:0"), Backend: backend, Log: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Shutdown(ctx)
	if err := n.reg.SetRaw(ctx, "accounting.interval_seconds", "15"); err != nil {
		t.Fatal(err)
	}
	profile, err := n.apiServer.Ifaces.Create(ctx, iface.CreateInput{Name: "awg0"})
	if err != nil {
		t.Fatal(err)
	}
	var ids, publicKeys []string
	for _, name := range []string{"quota-fixture", "expiry-fixture"} {
		account, err := n.apiServer.Users.Create(ctx, user.Input{Username: name, TrafficLimitBytes: domain.OptInt64{Set: true, Value: 100}})
		if err != nil {
			t.Fatal(err)
		}
		pair, err := tunnel.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		private, err := n.ring.Encrypt([]byte(pair.Private))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := n.apiServer.Devices.Create(ctx, account.ID, name, device.KeyMaterial{PublicKey: pair.Public, PrivateKeyEnc: private}, profile.ID); err != nil {
			t.Fatal(err)
		}
		ids, publicKeys = append(ids, account.ID), append(publicKeys, pair.Public)
	}
	if _, err := n.reconciler.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := backend.SetPeerActivity(profile.Name, publicKeys[0], time.Now(), 101, 0); err != nil {
		t.Fatal("quota fixture peer was not applied")
	}
	if _, err := n.db.Exec(`UPDATE users SET expires_at=? WHERE id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), ids[1]); err != nil {
		t.Fatal(err)
	}
	// Use the production worker/dispatch/shutdown code with deliberately stalled
	// operations. Metering, expiry and peer reconciliation remain the real services.
	n.sched.Stop()
	for _, work := range n.slowWork {
		_ = work.stop(ctx)
	}
	started := make(chan struct{}, 2)
	stalled := func(job context.Context) error { started <- struct{}{}; <-job.Done(); return job.Err() }
	n.slowWork = []*slowWork{
		startSlowWork(ctx, quietLogger(), "delivery-fixture", time.Minute, stalled),
		startSlowWork(ctx, quietLogger(), "archive-fixture", time.Minute, stalled),
	}
	for _, work := range n.slowWork {
		_ = work.request(ctx)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("slow-work fixture did not start")
		}
	}
	n.sched = scheduler.New(quietLogger())
	finished := make(chan struct{})
	begin := time.Now()
	n.sched.At("accounting", begin, func(job context.Context) error {
		err := n.jobAccounting(job)
		close(finished)
		return err
	})
	n.sched.Start(ctx)
	budget := n.accountingInterval(ctx)
	select {
	case <-finished:
	case <-time.After(budget):
		t.Fatal("stalled slow work exceeded one accounting cadence of enforcement lag")
	}
	for index, expected := range []string{"traffic_exceeded", "expired"} {
		var actual string
		if err := n.db.QueryRow(`SELECT status FROM users WHERE id=?`, ids[index]).Scan(&actual); err != nil || actual != expected {
			t.Fatal("stalled work prevented the expected lifecycle transition")
		}
	}
	state, err := backend.Dump(ctx, profile.Name)
	if err != nil || len(state.Peers) != 0 {
		t.Fatal("status changed without removing the enforced peers")
	}
	t.Logf("local fake-backend enforcement completed in %s while both slow passes were stalled; budget %s", time.Since(begin).Round(time.Millisecond), budget)
}
