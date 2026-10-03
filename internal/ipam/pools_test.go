package ipam

import "testing"

func TestPrivatePoolBoundariesAndCapacity(t *testing.T) {
	for cidr, want := range map[string]uint64{"10.8.0.0/24": 253, "10.8.0.0/23": 509, "10.8.0.0/22": 1021, "10.8.0.0/21": 2045, "10.8.0.0/20": 4093, "192.168.0.0/29": 5} {
		p, err := Parse(cidr)
		if err != nil || Capacity(p) != want {
			t.Fatalf("capacity %s: %v", cidr, err)
		}
	}
	for _, cidr := range []string{"10.8.0.1/24", "10.8.0.0/31", "192.168.0.0/15", "8.8.8.0/24", "::/64", "bad"} {
		if _, err := Parse(cidr); err == nil {
			t.Fatalf("invalid pool admitted: %s", cidr)
		}
	}
	if Validate([]string{"10.8.0.0/22", "10.8.1.0/24"}) == nil {
		t.Fatal("overlapping primary/extras accepted")
	}
	if _, err := Decode("10.8.0.0/24", `["invalid"]`); err == nil {
		t.Fatal("invalid stored pool hidden")
	}
}
