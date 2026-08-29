package accounting

// Benchmarks for the resource-budget contract (archive proposal §8: one
// accounting cycle ≤ 15 ms typical; recorded per release in
// docs/development/status.md). 100 devices ≈ the steady-state target node,
// 1000 devices the stress case.

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

func benchEnv(b *testing.B, devices int) (*env, []string) {
	b.Helper()
	dir := b.TempDir()
	db, err := database.Open(filepath.Join(dir, "bench.db"), database.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, slog.Default()); err != nil {
		b.Fatal(err)
	}
	backend := fake.New()
	e := &env{db: db, backend: backend, now: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	e.svc = NewService(db, backend, nil, nil, nil)
	e.svc.now = func() time.Time { return e.now }
	if err := backend.CreateInterface(ctx, tunnel.InterfaceSpec{Name: ifaceName, ListenPort: 40001}); err != nil {
		b.Fatal(err)
	}
	now := e.now.Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO tunnel_interfaces
		(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted, preset_name, enabled, backend_mode, created_at, updated_at)
		VALUES (?, ?, 40001, ?, 1420, 'srvpub', x'00', 'plain', 1, 'kernel', ?, ?)`,
		ifaceID, ifaceName, subnet, now, now); err != nil {
		b.Fatal(err)
	}

	keys := make([]string, devices)
	peers := make([]tunnel.PeerConfig, 0, devices)
	for i := 0; i < devices; i++ {
		uid := fmt.Sprintf("u%d", i)
		if _, err := db.Exec(`INSERT INTO users (id, username, status, start_policy, enabled, created_at, updated_at)
			VALUES (?, ?, 'active', 'immediate', 1, ?, ?)`, uid, uid, now, now); err != nil {
			b.Fatal(err)
		}
		key := fmt.Sprintf("k%05dAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", i)
		keys[i] = key
		// /22 pool so 1000 devices fit (a /24 caps at ~250).
		o := i + 2
		ip := fmt.Sprintf("10.8.%d.%d/32", o/256, o%256)
		if _, err := db.Exec(`INSERT INTO devices (id, user_id, interface_id, name, ipv4_address, public_key,
			private_key_encrypted, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, x'00', 1, ?, ?)`,
			fmt.Sprintf("d%d", i), uid, ifaceID, uid, ip, key, now, now); err != nil {
			b.Fatal(err)
		}
		peers = append(peers, tunnel.PeerConfig{PublicKey: key, AllowedIPs: []string{ip}})
	}
	// One bulk sync: fake syncconf semantics remove peers missing from the list.
	if err := backend.SyncPeers(ctx, ifaceName, peers); err != nil {
		b.Fatal(err)
	}
	return e, keys
}

func BenchmarkRunCycleIdle100(b *testing.B) {
	e, _ := benchEnv(b, 100)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.svc.RunCycle(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunCycleIdle1000(b *testing.B) {
	e, _ := benchEnv(b, 1000)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.svc.RunCycle(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunCycleActive1000(b *testing.B) {
	e, keys := benchEnv(b, 1000)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e.now = e.now.Add(30 * time.Second) // counters advanced 1 MB RX / 0.5 MB TX per device
		for _, k := range keys {
			if err := e.backend.SetPeerActivity(ifaceName, k, e.now.Add(-time.Second), uint64(i+1)*1_000_000, uint64(i+1)*500_000); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		if _, err := e.svc.RunCycle(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFlushSamples1000(b *testing.B) {
	e, keys := benchEnv(b, 1000)
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e.now = e.now.Add(30 * time.Second)
		for _, k := range keys {
			_ = e.backend.SetPeerActivity(ifaceName, k, e.now.Add(-time.Second), uint64(i+1)*1_000_000, uint64(i+1)*500_000)
		}
		if _, err := e.svc.RunCycle(ctx); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := e.svc.FlushSamples(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
