package network

import (
	"context"
	"strings"
	"testing"
)

func TestSyncAddressesAddsBeforeRemovingAndIsIdempotent(t *testing.T) {
	f := &fakeRunner{}
	l := &Links{Run: f}
	ctx := context.Background()
	f.push("ip", `[{"addr_info":[{"local":"10.8.0.1","prefixlen":24},{"local":"10.8.2.1","prefixlen":24}]}]`, "", nil)
	if err := l.SyncAddresses(ctx, "awg0", []string{"10.8.0.1/24", "10.8.1.1/24"}); err != nil {
		t.Fatal(err)
	}
	want := "ip -j -4 addr show dev awg0\nip addr add 10.8.1.1/24 dev awg0\nip addr del 10.8.2.1/24 dev awg0\n"
	if f.joined() != want {
		t.Fatalf("address sequence: %s", f.joined())
	}
	f.calls = nil
	f.push("ip", `[{"addr_info":[{"local":"10.8.0.1","prefixlen":24},{"local":"10.8.1.1","prefixlen":24}]}]`, "", nil)
	if err := l.SyncAddresses(ctx, "awg0", []string{"10.8.0.1/24", "10.8.1.1/24"}); err != nil || len(f.calls) != 1 {
		t.Fatal("stable addressing mutated the link")
	}
	f.push("ip", "invalid-json", "", nil)
	if err := l.SyncAddresses(ctx, "awg0", []string{"10.8.0.1/24"}); err == nil {
		t.Fatal("invalid inventory allowed mutations")
	}
}

func TestPoolRoutesIgnoreDefaultAndOwnedTunnelOnly(t *testing.T) {
	f := &fakeRunner{}
	l := &Links{Run: f}
	f.push("ip", `[{"dst":"default","dev":"eth0"},{"dst":"10.8.0.0/24","dev":"awg0"},{"dst":"172.18.0.0/16","dev":"docker0"},{"dst":"10.0.0.2","dev":"eth0"}]`, "", nil)
	pools, err := l.PoolConflicts(context.Background(), "awg0")
	if err != nil || len(pools) != 2 || pools[0].String() != "172.18.0.0/16" || pools[1].String() != "10.0.0.2/32" {
		t.Fatalf("route inventory: %v %v", pools, err)
	}
	if !strings.Contains(f.joined(), "route show table all") {
		t.Fatal("route inventory missing")
	}
}

func TestEmptyOverflowGatewayResizeRemovesConflictingAliasFirst(t *testing.T) {
	f := &fakeRunner{}
	l := &Links{Run: f}
	f.push("ip", `[{"addr_info":[{"local":"10.8.0.1","prefixlen":24},{"local":"10.8.4.1","prefixlen":24}]}]`, "", nil)
	if err := l.SyncAddresses(context.Background(), "awg0", []string{"10.8.0.1/24", "10.8.4.1/23"}); err != nil {
		t.Fatal(err)
	}
	if f.joined() != "ip -j -4 addr show dev awg0\nip addr del 10.8.4.1/24 dev awg0\nip addr add 10.8.4.1/23 dev awg0\n" {
		t.Fatal("gateway prefix replacement tried to add a duplicate local address")
	}
}
