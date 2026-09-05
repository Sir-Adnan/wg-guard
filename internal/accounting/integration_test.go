//go:build integration

package accounting

// Smoke-proofs the accounting cycle against the real pinned userspace
// runtime: a real 29-field dump flows through the delta pipeline and writes
// only empty/no-op state for a peer that has not handshaken. Real-traffic
// accounting (non-zero counters) requires a data-plane peer — the Phase 11
// production matrix (see docs/development/status.md).

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/amneziawg"
)

// startDaemon runs amneziawg-go in the foreground (WG_PROCESS_FOREGROUND=1
// is how the upstream re-exec marks the child) and waits for its UAPI socket.
func startDaemon(t *testing.T, daemon, name, socket string) func() {
	t.Helper()
	cmd := exec.Command(daemon, name)
	cmd.Env = append(os.Environ(), "WG_PROCESS_FOREGROUND=1")
	var logBuf strings.Builder
	cmd.Stdout = &logBuf
	cmd.Stderr = &logBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			return func() {
				_ = cmd.Process.Kill()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
				}
				_ = os.Remove(socket) // SIGKILL skips the daemon's cleanup
			}
		}
		select {
		case err := <-done:
			t.Fatalf("daemon exited early: %v\n%s", err, logBuf.String())
		case <-time.After(200 * time.Millisecond):
		}
	}
	_ = cmd.Process.Kill()
	t.Fatalf("UAPI socket %s never appeared\n%s", socket, logBuf.String())
	return func() {}
}

func TestIntegrationCycleAgainstRealDaemon(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root")
	}
	daemon, err := exec.LookPath("amneziawg-go")
	if err != nil {
		t.Skip("amneziawg-go not on PATH")
	}
	_, err = exec.LookPath("awg")
	if err != nil {
		t.Skip("awg not on PATH")
	}

	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "acc.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, slog.Default()); err != nil {
		t.Fatal(err)
	}

	const ifcName = "awgacc0"
	run := subprocess.NewSystem()
	backend := amneziawg.NewWithBinary(run, "awg")
	stop := startDaemon(t, daemon, ifcName, "/var/run/amneziawg/"+ifcName+".sock")
	defer stop()
	serverKeys, err := tunnel.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyInterfaceConfig(ctx, ifcName, tunnel.InterfaceConfig{
		PrivateKey: serverKeys.Private, ListenPort: 40990,
	}); err != nil {
		t.Fatalf("configure accounting fixture interface: %v", err)
	}

	e := &env{db: db, backend: nil, now: time.Now()}
	e.svc = NewService(db, backend, nil, nil, nil)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO tunnel_interfaces
		(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted, preset_name, enabled, backend_mode, created_at, updated_at)
		VALUES ('ifc-acc', ?, 40990, ?, 1420, ?, x'00', 'plain', 1, 'userspace', ?, ?)`,
		ifcName, "10.9.9.0/24", serverKeys.Public, now, now); err != nil {
		t.Fatal(err)
	}
	// A user + device whose peer exists in the backend but has never
	// handshaken: the cycle must observe it and write nothing.
	if _, err := db.Exec(`INSERT INTO users (id, username, status, start_policy, enabled, created_at, updated_at)
		VALUES ('u-acc', 'accuser', 'active', 'immediate', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO devices (id, user_id, interface_id, name, ipv4_address, public_key,
		private_key_encrypted, enabled, created_at, updated_at)
		VALUES ('d-acc', 'u-acc', 'ifc-acc', 'phone', '10.9.9.2/32',
		'2PjMPVyJbWQO3M5uJXU1RzqLXbSKL0zp3yNfOLQHnVs=', x'00', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncPeers(ctx, ifcName, []tunnel.PeerConfig{
		{PublicKey: "2PjMPVyJbWQO3M5uJXU1RzqLXbSKL0zp3yNfOLQHnVs=", AllowedIPs: []string{"10.9.9.2/32"}},
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := e.svc.RunCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Interfaces != 1 || len(rep.Errors) != 0 {
		t.Fatalf("cycle report: %+v", rep)
	}
	if rep.Deltas != 0 || rep.Activated != 0 || rep.QuotaTripped != 0 {
		t.Fatalf("no-traffic peer must produce no transitions: %+v", rep)
	}
	var rx, tx int64
	if err := db.QueryRow(`SELECT rx_bytes, tx_bytes FROM devices WHERE id = 'd-acc'`).Scan(&rx, &tx); err != nil {
		t.Fatal(err)
	}
	if rx != 0 || tx != 0 {
		t.Fatalf("counters must stay zero: %d/%d", rx, tx)
	}
}
