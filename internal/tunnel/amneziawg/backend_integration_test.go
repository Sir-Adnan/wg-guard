//go:build integration

package amneziawg

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Integration against the pinned userspace runtime (amneziawg-go): the real
// setconf/syncconf/dump round-trip, including the verify-after-apply gate and
// the upstream constraint rejection. Runs as root in WSL2/CI with the daemon
// on PATH; skips otherwise. Kernel link operations (ip link add type
// amneziawg) need the kernel module and stay in the VPS matrix (Phase 8).
//
//	go test -tags integration ./internal/tunnel/amneziawg/ -run Integration -v
func TestIntegrationUserspaceBackend(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root required for TUN")
	}
	daemon, err := exec.LookPath("amneziawg-go")
	if err != nil {
		t.Skip("amneziawg-go not on PATH")
	}
	if _, err := exec.LookPath("awg"); err != nil {
		t.Skip("awg not on PATH")
	}

	ctx := context.Background()
	name := "awg-gutest"
	socket := "/var/run/amneziawg/" + name + ".sock"

	stop := startDaemon(t, daemon, name, socket)
	t.Cleanup(stop)

	b := New(subprocess.NewSystem())

	v, err := b.ToolsVersion(ctx)
	if err != nil {
		t.Fatalf("tools version: %v", err)
	}
	t.Logf("awg tools: %s (pin %s)", v, PinnedToolsVersion)

	kp, err := tunnel.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	obf := tunnel.Obfuscation{
		Enabled: true,
		Jc:      5, Jmin: 40, Jmax: 70, S1: 86, S2: 61,
		H1: 1234567, H2: 2345678, H3: 3456789, H4: 4567890,
	}
	cfg := tunnel.InterfaceConfig{
		PrivateKey:  kp.Private,
		ListenPort:  39417,
		Obfuscation: obf,
	}

	// setconf + verify-after-apply against the real runtime: the renderer's
	// output must be accepted and echoed back exactly by the pinned daemon.
	if err := b.ApplyInterfaceConfig(ctx, name, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}

	st, err := b.Dump(ctx, name)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if st.ListenPort != 39417 || st.Obfuscation != obf {
		t.Fatalf("state = %+v", st)
	}
	if len(st.Peers) != 0 {
		t.Fatalf("peers = %d, want 0", len(st.Peers))
	}

	// syncconf: add a peer, observe it, remove it.
	psk, err := tunnel.GeneratePresharedKey()
	if err != nil {
		t.Fatal(err)
	}
	peerKP, err := tunnel.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	peers := []tunnel.PeerConfig{{
		PublicKey:    peerKP.Public,
		PresharedKey: psk,
		AllowedIPs:   []string{"10.8.99.2/32"},
	}}
	if err := b.SyncPeers(ctx, name, peers); err != nil {
		t.Fatalf("sync add: %v", err)
	}
	st, err = b.Dump(ctx, name)
	if err != nil || len(st.Peers) != 1 || !st.Peers[0].PresharedKeySet {
		t.Fatalf("peer not applied: %+v err=%v", st, err)
	}
	if len(st.Peers[0].AllowedIPs) != 1 || st.Peers[0].AllowedIPs[0] != "10.8.99.2/32" {
		t.Fatalf("allowed IPs = %v", st.Peers[0].AllowedIPs)
	}
	if err := b.SyncPeers(ctx, name, nil); err != nil {
		t.Fatalf("sync remove: %v", err)
	}
	st, _ = b.Dump(ctx, name)
	if len(st.Peers) != 0 {
		t.Fatalf("peer not removed: %d", len(st.Peers))
	}

	// The runtime rejects duplicate header values (pinned fact): our verify
	// path must surface the CLI error, not pass silently.
	bad := cfg
	bad.Obfuscation.H2 = bad.Obfuscation.H1
	if err := b.ApplyInterfaceConfig(ctx, name, bad); err == nil {
		t.Fatal("constraint-violating config must be rejected")
	}

	// Pinned limitation (verified here): setconf cannot move an interface
	// from obfuscated back to plain — omitted params persist and explicit
	// zeros are rejected with EINVAL. The verify gate must surface it;
	// reconcile recreates the link for mode transitions instead.
	plain := tunnel.InterfaceConfig{PrivateKey: kp.Private, ListenPort: 39417}
	if err := b.ApplyInterfaceConfig(ctx, name, plain); err == nil {
		t.Fatal("plain apply after obfuscated state must fail verification (pinned runtime keeps params)")
	}

	// Same-mode value changes apply cleanly via setconf (all params written).
	obf2 := obf
	obf2.Jc = 7
	obf2.S2 = 62
	if err := b.ApplyInterfaceConfig(ctx, name, tunnel.InterfaceConfig{
		PrivateKey: kp.Private, ListenPort: 39417, Obfuscation: obf2,
	}); err != nil {
		t.Fatalf("apply changed obfuscation values: %v", err)
	}

	names, err := b.ListInterfaces(ctx)
	if err != nil || len(names) == 0 {
		t.Fatalf("list: %v %v", names, err)
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("interface not listed: %v", names)
	}
}

// startDaemon runs amneziawg-go in the foreground (WG_PROCESS_FOREGROUND=1 is
// how the upstream re-exec marks the child) and waits for its UAPI socket.
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
	t.Fatalf("UAPI socket %s never appeared\n%s", socket, truncate(logBuf.String(), 500))
	return func() {}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
