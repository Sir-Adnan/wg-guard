//go:build integration && linux

package serve

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/hoststats"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// This exercises production server/scheduler/archive/delivery code with a fake
// tunnel and local HTTP receivers. It never opens installed data or changes host
// networking. It measures real factor-18 crypto and I/O, not a synthetic sleep
// replacing the archive job; real-kernel/client acceptance remains separate.
func TestEncryptedBackupLoadPreservesEnforcementCadence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	backend := fake.New()
	cfg := testConfig(t, "127.0.0.1:0")
	n, err := Start(ctx, Options{Config: cfg, Backend: backend, Log: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Shutdown(context.Background())
	for key, value := range map[string]string{"accounting.interval_seconds": "15", "backup.password": "synthetic-load-password", "backup.telegram_token": "synthetic-token", "backup.telegram_chat": "123456789"} {
		if err := n.reg.SetRaw(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := n.apiServer.Ifaces.Create(ctx, iface.CreateInput{Name: "awg0", Subnet: "10.77.0.0/22"})
	if err != nil {
		t.Fatal(err)
	}
	var accounts, keys []string
	for _, name := range []string{"load-quota", "load-expiry", "load-background"} {
		account, err := n.apiServer.Users.Create(ctx, user.Input{Username: name, TrafficLimitBytes: domain.OptInt64{Set: true, Value: 100}})
		if err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, account.ID)
	}
	for index := range 512 {
		pair, err := tunnel.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		private, err := n.ring.Encrypt([]byte(pair.Private))
		if err != nil {
			t.Fatal(err)
		}
		account := accounts[2]
		if index < 2 {
			account = accounts[index]
			keys = append(keys, pair.Public)
		}
		if _, err := n.apiServer.Devices.Create(ctx, account, fmt.Sprintf("device-%d", index), device.KeyMaterial{PublicKey: pair.Public, PrivateKeyEnc: private}, profile.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := n.reconciler.Run(ctx); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	var releaseOnce sync.Once
	deliveryStarted := make(chan struct{}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case deliveryStarted <- struct{}{}:
		default:
		}
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"ok":true}`)
		case <-r.Context().Done():
		}
	}))
	defer receiver.Close()
	defer releaseOnce.Do(func() { close(release) })
	n.backup.HTTPClient = loadTelegramClient{base: receiver.URL, client: receiver.Client()}
	// Node-wide operator webhooks may target this local test receiver. Do not
	// weaken reseller destination validation or install a production transport seam.
	if _, _, err := n.apiServer.Webhooks.Create(ctx, receiver.URL, []string{webhook.EventUserUpdated}, "synthetic-load-webhook-secret"); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := webhook.NewRecorder().Emit(ctx, n.db, webhook.EventUserUpdated, map[string]any{"user_id": accounts[2]}); err != nil {
			t.Fatal(err)
		}
	}
	schedule, err := n.backup.CreateSchedule(ctx, &backup.Schedule{Name: "load-fixture", Kind: backup.KindInterval, IntervalHours: 1, Enabled: true, RetentionCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.db.ExecContext(ctx, `UPDATE backup_schedules SET next_run_at=? WHERE id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), schedule.ID); err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	reader := hoststats.New(cfg.DataDir)
	baseline := reader.Snapshot(time.Now()).ProcessRSSBytes
	peak := baseline
	baseGoroutines := runtime.NumGoroutine()
	_ = n.slowWork[0].request(ctx)
	_ = n.slowWork[1].request(ctx)
	// Observe real KDF allocation before dispatching the first enforcement pass.
	deadline := time.NewTimer(15 * time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	defer deadline.Stop()
	kdfObserved := false
	for !kdfObserved {
		select {
		case <-ticker.C:
			rss := reader.Snapshot(time.Now()).ProcessRSSBytes
			if rss > peak {
				peak = rss
			}
			kdfObserved = rss >= baseline+(128<<20)
		case <-deadline.C:
			t.Fatal("did not observe the actual archive KDF allocation")
		case <-ctx.Done():
			t.Fatal("load fixture timed out")
		}
	}
	if err := backend.SetPeerActivity(profile.Name, keys[0], time.Now(), 101, 0); err != nil {
		t.Fatal(err)
	}
	first := make(chan time.Duration, 1)
	second := make(chan time.Duration, 1)
	due := time.Now()
	if _, err := n.db.ExecContext(ctx, `UPDATE users SET expires_at=? WHERE id=?`, due.Add(15*time.Second).UTC().Format(time.RFC3339Nano), accounts[1]); err != nil {
		t.Fatal(err)
	}
	var cycles int
	n.sched.At("accounting", due, func(job context.Context) error {
		start := time.Now()
		err := n.jobAccounting(job)
		cycles++
		if cycles == 1 {
			first <- time.Since(due)
		} else if cycles == 2 {
			second <- time.Since(start)
		}
		return err
	})
	budget := 15 * time.Second
	var firstLag, secondCycle time.Duration
	select {
	case firstLag = <-first:
	case <-time.After(budget):
		t.Fatal("KDF stalled quota enforcement beyond one accounting cadence")
	}
	if firstLag > budget {
		t.Fatal("quota enforcement exceeded its cadence budget")
	}
	select {
	case <-deliveryStarted:
	case <-ctx.Done():
		t.Fatal("slow delivery was not exercised")
	}
	// Both real worker passes are active; the backup receiver remains stalled
	// for a full accounting cadence while expiry must still be applied.
	for secondCycle == 0 {
		select {
		case secondCycle = <-second:
		case <-ticker.C:
			rss := reader.Snapshot(time.Now()).ProcessRSSBytes
			if rss > peak {
				peak = rss
			}
		case <-ctx.Done():
			t.Fatal("slow delivery stalled expiry")
		}
	}
	for index, expected := range []string{"traffic_exceeded", "expired"} {
		var status string
		if err := n.db.QueryRowContext(ctx, `SELECT status FROM users WHERE id=?`, accounts[index]).Scan(&status); err != nil || status != expected {
			t.Fatal("load prevented an enforcement transition")
		}
	}
	state, err := backend.Dump(ctx, profile.Name)
	if err != nil || len(state.Peers) != 510 {
		t.Fatal("enforced accounts retained their live peers")
	}
	releaseOnce.Do(func() { close(release) })
	for {
		current, err := n.backup.GetSchedule(ctx, schedule.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.LastRunAt != nil {
			if current.LastStatus != "ok" {
				t.Fatal("scheduled archive did not finish")
			}
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("scheduled archive did not complete")
		}
	}
	archives, err := n.backup.List()
	if err != nil || len(archives) != 1 || !archives[0].Encrypted {
		t.Fatal("actual encrypted archive was not published")
	}
	report, err := backup.VerifyArchive(ctx, archives[0].Path, "synthetic-load-password")
	if err != nil || report.Inventory.Devices != 512 {
		t.Fatal("load archive is not independently portable", err)
	}
	activeGoroutines := runtime.NumGoroutine()
	if err := n.Shutdown(ctx); err != nil {
		t.Fatal("load fixture did not drain", err)
	}
	receiver.CloseClientConnections()
	receiver.Client().CloseIdleConnections()
	runtime.Gosched()
	t.Logf("Linux synthetic node: devices=512, GOMAXPROCS=%d, quota_lag=%s, expiry_cycle=%s, cadence=15s, RSS_baseline_MiB=%.1f, RSS_peak_MiB=%.1f, goroutines_baseline=%d, goroutines_final=%d", runtime.GOMAXPROCS(0), firstLag.Round(time.Millisecond), secondCycle.Round(time.Millisecond), float64(baseline)/(1<<20), float64(peak)/(1<<20), baseGoroutines, runtime.NumGoroutine())
	t.Logf("goroutines_before_drain=%d", activeGoroutines)
}

type loadTelegramClient struct {
	base   string
	client *http.Client
}

func (c loadTelegramClient) Do(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	endpoint, err := url.Parse(c.base + "/archive")
	if err != nil {
		return nil, err
	}
	request.URL = endpoint
	return c.client.Do(request)
}
