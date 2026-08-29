//go:build integration

package firewall

import (
	"context"
	"os/exec"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Integration against real nftables (WSL2/CI as root): the rendered-state
// apply loop, including the atomic delete+recreate of an existing table and
// idempotent removal. Interface names need not exist — iifname/oifname are
// string matches.
//
//	go test -tags integration ./internal/firewall/ -run Integration -v
func TestIntegrationNftablesApply(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft not on PATH")
	}
	m := &Manager{Run: subprocess.NewSystem()}
	ctx := context.Background()

	// Clean start.
	if err := m.Remove(ctx); err != nil {
		t.Fatalf("initial remove: %v", err)
	}
	if present, _ := m.Present(ctx); present {
		t.Fatal("table should be absent")
	}

	ifaces := []Interface{
		{Name: "awg-integ0", Subnet: "10.8.50.0/24"},
		{Name: "awg-integ1", Subnet: "10.8.51.0/24"},
	}
	if err := m.Apply(ctx, ifaces); err != nil {
		t.Fatalf("apply: %v", err)
	}
	present, err := m.Present(ctx)
	if err != nil || !present {
		t.Fatalf("present = %v err = %v", present, err)
	}

	// Re-apply must replace (not duplicate) rules.
	if err := m.Apply(ctx, ifaces); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	out, err := m.Run.Run(ctx, []string{"nft", "list", "table", TableName})
	if err != nil {
		t.Fatalf("list table: %v", err)
	}
	count := func(sub string) int {
		n := 0
		for i := 0; i+len(sub) <= len(out.Stdout); i++ {
			if string(out.Stdout[i:i+len(sub)]) == sub {
				n++
			}
		}
		return n
	}
	if n := count(`iifname "awg-integ0"`); n != 1 {
		t.Fatalf("forward rule count = %d, want 1 (duplicate apply merged?)", n)
	}
	if n := count(`ip saddr 10.8.51.0/24 masquerade`); n != 1 {
		t.Fatalf("masquerade rule count = %d, want 1", n)
	}

	if err := m.Remove(ctx); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if present, _ := m.Present(ctx); present {
		t.Fatal("table should be gone after remove")
	}
}
