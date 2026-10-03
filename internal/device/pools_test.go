package device

import (
	"context"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func TestOverflowPoolsAndPermanentAccountDeletion(t *testing.T) {
	s, ifaces := newService(t)
	ctx := context.Background()
	i, err := ifaces.Create(ctx, iface.CreateInput{Name: "awg0", Pools: []string{"10.77.0.0/29", "10.77.1.0/29"}})
	if err != nil {
		t.Fatal(err)
	}
	users := user.NewService(s.db)
	u, err := users.Create(ctx, user.Input{Username: "multipool"})
	if err != nil {
		t.Fatal(err)
	}
	var first *Device
	for n := 0; n < 10; n++ {
		d, err := s.Create(ctx, u.ID, "device-"+itoa(n), newKeyPair(t, s.ring), i.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			first = d
		}
		if n == 5 && d.IPv4 != "10.77.1.2/32" {
			t.Fatal("allocation did not continue in second pool")
		}
	}
	if _, err := s.Create(ctx, u.ID, "overflow", newKeyPair(t, s.ring), i.ID); domain.CodeOf(err) != domain.CodeDevicePoolExhausted {
		t.Fatal("full pools not rejected")
	}
	if _, err := ifaces.Update(ctx, i.ID, iface.UpdateInput{Pools: []string{i.Subnet}}); domain.CodeOf(err) != domain.CodeSubnetOverlap {
		t.Fatal("occupied pool removed")
	}
	usage, err := ifaces.Capacity(ctx, i.ID)
	if err != nil || len(usage) != 2 || usage[0].Free != 0 || usage[1].Used != 5 {
		t.Fatal("capacity does not count both pools")
	}
	if err := users.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	u2, err := users.Create(ctx, user.Input{Username: "multipool"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Create(ctx, u2.ID, "replacement", newKeyPair(t, s.ring), i.ID)
	if err != nil || d.IPv4 != first.IPv4 {
		t.Fatal("account deletion did not release all IPs")
	}
	if _, err := ifaces.Update(ctx, i.ID, iface.UpdateInput{Pools: []string{i.Subnet}}); err != nil {
		t.Fatal("empty overflow pool could not be removed")
	}
}
