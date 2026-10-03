package reconcile

import (
	"context"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

func TestPoolExpansionPreservesPeerAndLinkIdentity(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	i := h.seedProfile(t, "awg0", 1, true)
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	h.backend.ResetOps()
	if _, err := h.ifaceSvc.Update(ctx, i.ID, iface.UpdateInput{Pools: []string{i.Subnet, "10.8.3.0/24"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	spec, _ := h.backend.Spec("awg0")
	if len(spec.Addresses) != 2 || spec.Addresses[1] != "10.8.3.1/24" {
		t.Fatal("runtime did not receive overflow gateway")
	}
	for _, op := range h.backend.Ops() {
		if op.Kind == fake.OpCreate || op.Kind == fake.OpRemove {
			t.Fatal("expansion recreated the link")
		}
	}
	state, err := h.backend.Dump(ctx, "awg0")
	if err != nil || len(state.Peers) != 1 {
		t.Fatal("expansion lost existing peer")
	}
}
