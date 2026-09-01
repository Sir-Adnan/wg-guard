package fake

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

func spec(name string) tunnel.InterfaceSpec {
	return tunnel.InterfaceSpec{Name: name, PrivateKey: "priv-" + name, ListenPort: 51820, MTU: 1420}
}

func TestCreateApplySyncDump(t *testing.T) {
	b := New()
	ctx := context.Background()
	if err := b.CreateInterface(ctx, spec("awg0")); err != nil {
		t.Fatal(err)
	}
	if err := b.CreateInterface(ctx, spec("awg0")); err == nil {
		t.Fatal("duplicate create accepted")
	}

	cfg := tunnel.InterfaceConfig{
		PrivateKey: "priv-awg0",
		ListenPort: 51820,
		Peers: []tunnel.PeerConfig{
			{PublicKey: "pkA", AllowedIPs: []string{"10.8.0.2/32"}, PresharedKey: "psk",
				PersistentKeepalive: mustU16Range(t, "25-35")},
		},
	}
	if err := b.ApplyInterfaceConfig(ctx, "awg0", cfg); err != nil {
		t.Fatal(err)
	}
	if err := b.SyncPeers(ctx, "awg0", []tunnel.PeerConfig{
		{PublicKey: "pkA", AllowedIPs: []string{"10.8.0.2/32"}, PersistentKeepalive: mustU16Range(t, "25-35")},
		{PublicKey: "pkB", AllowedIPs: []string{"10.8.0.3/32"}},
	}); err != nil {
		t.Fatal(err)
	}
	st, err := b.Dump(ctx, "awg0")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Peers) != 2 {
		t.Fatalf("want 2 peers, got %d", len(st.Peers))
	}
	for _, peer := range st.Peers {
		if peer.PublicKey == "pkA" && peer.PersistentKeepalive != mustU16Range(t, "25-35") {
			t.Fatalf("keepalive range lost: %s", peer.PersistentKeepalive)
		}
	}

	// syncconf removes unknown peers, keeps known ones' state.
	if err := b.SetPeerActivity("awg0", "pkA", time.Now(), 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := b.SyncPeers(ctx, "awg0", []tunnel.PeerConfig{
		{PublicKey: "pkA", AllowedIPs: []string{"10.8.0.2/32"}},
	}); err != nil {
		t.Fatal(err)
	}
	st, _ = b.Dump(ctx, "awg0")
	if len(st.Peers) != 1 || st.Peers[0].PublicKey != "pkA" {
		t.Fatalf("peer diff wrong: %+v", st.Peers)
	}
	if st.Peers[0].RXBytes != 100 {
		t.Fatalf("peer state lost in sync: %+v", st.Peers[0])
	}

	ops := b.Ops()
	if ops[0].Kind != OpCreate || ops[1].Kind != OpApply || ops[2].Kind != OpSync {
		t.Fatalf("op order wrong: %+v", ops)
	}

	if err := b.RemoveInterface(ctx, "awg0"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Dump(ctx, "awg0"); !errors.Is(err, tunnel.ErrInterfaceNotFound) {
		t.Fatalf("expected ErrInterfaceNotFound, got %v", err)
	}
	names, _ := b.ListInterfaces(ctx)
	if len(names) != 0 {
		t.Fatalf("interface not removed: %v", names)
	}
}

func mustU16Range(t *testing.T, text string) awgparam.U16Range {
	t.Helper()
	value, err := awgparam.ParseU16Range(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFailureInjection(t *testing.T) {
	b := New()
	boom := errors.New("injected")
	b.FailOn[OpCreate] = boom
	if err := b.CreateInterface(context.Background(), spec("awg1")); !errors.Is(err, boom) {
		t.Fatalf("injection not honored: %v", err)
	}
	// One-shot: the next create succeeds.
	if err := b.CreateInterface(context.Background(), spec("awg1")); err != nil {
		t.Fatalf("one-shot injection not cleared: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	b := New()
	ctx := context.Background()
	if err := b.CreateInterface(ctx, spec("awg0")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = b.SyncPeers(ctx, "awg0", []tunnel.PeerConfig{{ //nolint:errcheck
				PublicKey: "pk", AllowedIPs: []string{"10.8.0.9/32"},
			}})
			_, _ = b.Dump(ctx, "awg0")
			_, _ = b.ListInterfaces(ctx)
		}(i)
	}
	wg.Wait()
}
