package iface

import (
	"context"
	"net/netip"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func TestAutomaticPoolsSkipWidePoolsAndDisabledExtras(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	i, err := s.Create(ctx, CreateInput{Name: "awg0", Pools: []string{"10.8.0.0/22", "10.8.4.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetEnabled(ctx, i.ID, false)
	next, err := s.Create(ctx, CreateInput{Name: "awg1"})
	if err != nil || next.Subnet != "10.8.5.0/24" {
		t.Fatalf("automatic overlapping subnet: %+v %v", next, err)
	}
	if _, err := s.Create(ctx, CreateInput{Name: "awg2", Subnet: "10.8.4.0/24"}); domain.CodeOf(err) != domain.CodeSubnetOverlap {
		t.Fatal("disabled overflow pool not reserved")
	}
	if _, err := s.Update(ctx, next.ID, UpdateInput{Pools: []string{"10.8.6.0/24"}}); domain.CodeOf(err) != domain.CodeSubnetInvalid {
		t.Fatal("primary pool changed")
	}
}

func TestHostRoutesRejectExplicitAndGuideAutomaticPools(t *testing.T) {
	s := newService(t, WithHostRoutes(func(context.Context, string) ([]netip.Prefix, error) {
		return []netip.Prefix{netip.MustParsePrefix("10.8.0.0/22")}, nil
	}))
	ctx := context.Background()
	if _, err := s.Create(ctx, CreateInput{Name: "awg0", Subnet: "10.8.1.0/24"}); domain.CodeOf(err) != domain.CodeSubnetOverlap {
		t.Fatal("host route overlap admitted")
	}
	i, err := s.Create(ctx, CreateInput{Name: "awg0"})
	if err != nil || i.Subnet != "10.8.4.0/24" {
		t.Fatalf("host-aware selection: %+v %v", i, err)
	}
	if _, err := s.Update(ctx, i.ID, UpdateInput{Pools: []string{i.Subnet, "10.8.2.0/24"}}); domain.CodeOf(err) != domain.CodeSubnetOverlap {
		t.Fatal("expansion overlaps host routes")
	}
}
