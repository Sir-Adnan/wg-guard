package iface

import (
	"context"
	"net/netip"
	"testing"
)

func TestPoolSuggestionsRespectReservedProfilesAndHostRoutes(t *testing.T) {
	s := newService(t, WithHostRoutes(func(context.Context, string) ([]netip.Prefix, error) {
		return []netip.Prefix{netip.MustParsePrefix("10.8.4.0/24")}, nil
	}))
	ctx := context.Background()
	i, err := s.Create(ctx, CreateInput{Name: "awg0", Subnet: "10.8.0.0/22"})
	if err != nil {
		t.Fatal(err)
	}
	choices, err := s.SuggestPools(ctx, "awg1", "", 24)
	if err != nil || choices.Automatic != "10.8.5.0/24" {
		t.Fatalf("automatic suggestion: %+v %v", choices, err)
	}
	for _, choice := range choices.Choices {
		p := netip.MustParsePrefix(choice.CIDR)
		if p.Overlaps(netip.MustParsePrefix("10.8.0.0/22")) && choice.Status != "overlap" {
			t.Fatal("reserved network offered as available")
		}
	}
	current, err := s.SuggestPools(ctx, "awg0", i.ID, 22)
	if err != nil || current.Choices[0].Status != "current" || current.Choices[0].Free != 1021 {
		t.Fatal("current pool capacity missing")
	}
	if _, err := s.SuggestPools(ctx, "awg1", i.ID, 24); err == nil {
		t.Fatal("name/ID mismatch admitted")
	}
	if _, err := s.SuggestPools(ctx, "awg1", "", 8); err == nil {
		t.Fatal("unbounded suggested size admitted")
	}
}
