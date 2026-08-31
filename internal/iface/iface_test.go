package iface

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "i.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db, reg, ring)
}

func balancedObfuscation() Obfuscation {
	return Obfuscation{
		Enabled: true,
		Jc:      4, Jmin: 40, Jmax: 70, S1: 15, S2: 64,
		H1: 1, H2: 2, H3: 3, H4: 4,
	}
}

func TestCreateDefaults(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	ifc, err := svc.Create(ctx, CreateInput{Name: "awg0", Obfuscation: balancedObfuscation(), Preset: "balanced"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ifc.ListenPort < 30000 || ifc.ListenPort > 50000 {
		t.Fatalf("port not allocated from recommended window: %d", ifc.ListenPort)
	}
	if ifc.Subnet != "10.8.0.0/24" {
		t.Fatalf("default subnet = %q", ifc.Subnet)
	}
	if ifc.MTU != 1420 {
		t.Fatalf("default MTU = %d", ifc.MTU)
	}
	if ifc.BackendMode != domain.BackendKernel {
		t.Fatalf("default backend mode = %q", ifc.BackendMode)
	}

	// Round trip through the DB.
	got, err := svc.GetByName(ctx, "awg0")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Obfuscation.Enabled || got.Obfuscation.Jc != 4 || got.Obfuscation.H4 != 4 {
		t.Fatalf("obfuscation round trip broken: %+v", got.Obfuscation)
	}
	if got.Obfuscation.I1 != "" {
		t.Fatalf("I1 should be unset: %q", got.Obfuscation.I1)
	}
}

func TestCreateValidation(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	cases := []struct {
		name     string
		input    CreateInput
		wantCode string
	}{
		{"bad name", CreateInput{Name: "wg9"}, domain.CodeInvalidRequest},
		{"name above cap", CreateInput{Name: "awg8"}, domain.CodeInvalidRequest},
		{"port too low", CreateInput{Name: "awg0", ListenPort: 80}, domain.CodeInvalidRequest},
		{"bad subnet", CreateInput{Name: "awg0", Subnet: "10.8.0.1/24"}, domain.CodeSubnetInvalid},
		{"tiny pool", CreateInput{Name: "awg0", Subnet: "10.9.0.0/30"}, domain.CodeSubnetInvalid},
		{"mtu out of range", CreateInput{Name: "awg0", MTU: 100}, domain.CodeInvalidRequest},
		{"bad mode", CreateInput{Name: "awg0", BackendMode: "tun"}, domain.CodeInvalidRequest},
	}
	for _, tc := range cases {
		if _, err := svc.Create(ctx, tc.input); domain.CodeOf(err) != tc.wantCode {
			t.Errorf("%s: want %s, got %v", tc.name, tc.wantCode, err)
		}
	}
}

func TestObfuscationConstraintMatrix(t *testing.T) {
	// The constraint set from docs/integrations/amneziawg.md — the AWG
	// parser accepts all of these; the daemon rejects them at setconf.
	cases := []struct {
		name string
		o    Obfuscation
		ok   bool
	}{
		{"plain", Obfuscation{}, true},
		{"balanced", balancedObfuscation(), true},
		{"jc zero", func() Obfuscation { o := balancedObfuscation(); o.Jc = 0; return o }(), false},
		{"jc over max", func() Obfuscation { o := balancedObfuscation(); o.Jc = 129; return o }(), false},
		{"jc max ok", func() Obfuscation { o := balancedObfuscation(); o.Jc = 128; return o }(), true},
		{"jmin == jmax", func() Obfuscation { o := balancedObfuscation(); o.Jmin = 70; o.Jmax = 70; return o }(), false},
		{"jmin > jmax (parser accepts, runtime rejects)", func() Obfuscation { o := balancedObfuscation(); o.Jmin = 100; o.Jmax = 50; return o }(), false},
		{"jmax over 1280", func() Obfuscation { o := balancedObfuscation(); o.Jmax = 1281; return o }(), false},
		{"s1 at max", func() Obfuscation { o := balancedObfuscation(); o.S1 = 1132; return o }(), true},
		{"s1 over max", func() Obfuscation { o := balancedObfuscation(); o.S1 = 1133; return o }(), false},
		{"s2 at max", func() Obfuscation { o := balancedObfuscation(); o.S2 = 1188; return o }(), true},
		{"s2 over max", func() Obfuscation { o := balancedObfuscation(); o.S2 = 1189; return o }(), false},
		{"s1+56 == s2", func() Obfuscation { o := balancedObfuscation(); o.S1 = 8; o.S2 = 64; return o }(), false},
		{"s1+56 != s2", func() Obfuscation { o := balancedObfuscation(); o.S1 = 9; o.S2 = 64; return o }(), true},
		{"duplicate H", func() Obfuscation { o := balancedObfuscation(); o.H2 = 1; return o }(), false},
		{"zero H", func() Obfuscation { o := balancedObfuscation(); o.H4 = 0; return o }(), false},
		{"distinct H", balancedObfuscation(), true},
		{"partial params with disabled flag", func() Obfuscation { o := balancedObfuscation(); o.Enabled = false; return o }(), false},
	}
	for _, tc := range cases {
		err := ValidateObfuscation(tc.o)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.ok && domain.CodeOf(err) != domain.CodeParamConstraint {
			t.Errorf("%s: want PARAM_CONSTRAINT, got %v", tc.name, err)
		}
	}
}

func TestSubnetOverlapAndPortUniqueness(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	first, err := svc.Create(ctx, CreateInput{
		Name: "awg0", ListenPort: 40000, Subnet: "10.8.0.0/24",
		Obfuscation: balancedObfuscation(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Overlapping pool (contains first).
	if _, err := svc.Create(ctx, CreateInput{Name: "awg1", ListenPort: 40001, Subnet: "10.8.0.0/16"}); domain.CodeOf(err) != domain.CodeSubnetOverlap {
		t.Fatalf("overlap must be SUBNET_OVERLAP, got %v", err)
	}
	// Identical pool.
	if _, err := svc.Create(ctx, CreateInput{Name: "awg1", Subnet: "10.8.0.0/24"}); domain.CodeOf(err) != domain.CodeSubnetOverlap {
		t.Fatalf("same pool must be SUBNET_OVERLAP, got %v", err)
	}
	// Port in use.
	if _, err := svc.Create(ctx, CreateInput{Name: "awg1", ListenPort: 40000, Subnet: "10.9.0.0/24"}); domain.CodeOf(err) != domain.CodePortInUse {
		t.Fatalf("port reuse must be PORT_IN_USE, got %v", err)
	}
	// Name in use.
	if _, err := svc.Create(ctx, CreateInput{Name: "awg0", Subnet: "10.9.0.0/24"}); domain.CodeOf(err) != domain.CodeInterfaceNameTaken {
		t.Fatalf("name reuse must be INTERFACE_NAME_TAKEN, got %v", err)
	}
	// Non-overlapping works.
	if _, err := svc.Create(ctx, CreateInput{Name: "awg1", ListenPort: 40001, Subnet: "10.9.0.0/24"}); err != nil {
		t.Fatalf("distinct pool refused: %v", err)
	}
	if _, err := svc.Get(ctx, first.ID); err != nil {
		t.Fatalf("get: %v", err)
	}
}

func TestPortRangeValidation(t *testing.T) {
	if err := ValidatePortRange(50000, 30000); domain.CodeOf(err) != domain.CodeSettingInvalid {
		t.Fatalf("inverted range accepted: %v", err)
	}
	if err := ValidatePortRange(80, 90); err == nil {
		t.Fatal("privileged range accepted silently")
	}
	if err := ValidatePortRange(30000, 50000); err != nil {
		t.Fatalf("valid range rejected: %v", err)
	}
}

func TestDeleteProtectionAndLifecycle(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	ifc, err := svc.Create(ctx, CreateInput{Name: "awg0", Obfuscation: balancedObfuscation()})
	if err != nil {
		t.Fatal(err)
	}
	// A device row referencing the interface blocks deletion (rotation flow).
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, created_at, updated_at)
		VALUES ('u1', 'user1', 'active', 'now', 'now')`)
	_, err = svc.db.Exec(`INSERT INTO devices (id, user_id, interface_id, name, ipv4_address,
		public_key, private_key_encrypted, created_at, updated_at)
		VALUES ('d1', 'u1', ?, 'phone', '10.8.0.2/32', 'pk', x'00', 'now', 'now')`, ifc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, ifc.ID); err == nil {
		t.Fatal("delete with devices must be refused")
	}
	// Clear devices, then delete works.
	if _, err := svc.db.Exec(`DELETE FROM devices`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, ifc.ID); err != nil {
		t.Fatalf("delete after migration: %v", err)
	}
	if _, err := svc.Get(ctx, ifc.ID); domain.CodeOf(err) != domain.CodeInterfaceNotFound {
		t.Fatalf("expected INTERFACE_NOT_FOUND, got %v", err)
	}
}

func TestPresets(t *testing.T) {
	for _, p := range Presets() {
		if err := ValidateObfuscation(p.Obfuscation); err != nil {
			t.Errorf("preset %s violates constraints: %v", p.Name, err)
		}
	}
	if p, ok := PresetByName("balanced"); !ok || p.Obfuscation.Jc != 4 {
		t.Fatal("balanced preset not found or wrong")
	}
	if _, ok := PresetByName("turbo"); ok {
		t.Fatal("unknown preset found")
	}
}

func TestInterfaceCountCapFromSettings(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if err := svc.reg.Set(ctx, "interfaces.max_count", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateInput{Name: "awg0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateInput{Name: "awg1"}); err != nil {
		t.Fatal(err)
	}
	// awg2 is below the *old* default cap but above the configured one.
	if _, err := svc.Create(ctx, CreateInput{Name: "awg2"}); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("cap not enforced from settings: %v", err)
	}
}

func TestValidateObfuscationGated(t *testing.T) {
	base := Obfuscation{Enabled: true, Jc: 4, Jmin: 40, Jmax: 70, S1: 15, S2: 64, H1: 1, H2: 2, H3: 3, H4: 4}
	cases := []struct {
		name    string
		mutate  func(*Obfuscation)
		wantErr bool
	}{
		{"plain defaults ok", func(o *Obfuscation) {}, false},
		{"S3/S4 in range", func(o *Obfuscation) { o.S3 = 40; o.S4 = 100 }, false},
		{"S3 over u16", func(o *Obfuscation) { o.S3 = 70000 }, true},
		{"padding single value", func(o *Obfuscation) { o.ContentPaddingAddition = "10" }, false},
		{"padding range", func(o *Obfuscation) { o.ContentPaddingAddition = "10-20" }, false},
		{"padding inverted range", func(o *Obfuscation) { o.ContentPaddingAddition = "20-10" }, true},
		{"padding over u16", func(o *Obfuscation) { o.ContentPaddingAddition = "70000" }, true},
		{"padding garbage", func(o *Obfuscation) { o.ContentPaddingAddition = "10;20" }, true},
		{"rekey range ok", func(o *Obfuscation) { o.RekeyAfterTime = "120-180" }, false},
		{"hpk valid", func(o *Obfuscation) {
			o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
		}, false},
		{"hpk wrong length", func(o *Obfuscation) {
			o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(make([]byte, 16))
		}, true},
		{"hpk not base64", func(o *Obfuscation) { o.HeaderProtectionKey = "not-base64!!" }, true},
		{"timers set", func(o *Obfuscation) {
			o.RekeyAfterTime = "120"
			o.RekeyTimeout = "5-10"
			o.RejectAfterTime = "90"
			o.KeepaliveTimeout = "25"
			o.MaxHandshakeAttempts = "5"
		}, false},
	}
	for _, tc := range cases {
		o := base
		tc.mutate(&o)
		err := ValidateObfuscation(o)
		if tc.wantErr && err == nil {
			t.Errorf("%s: want error, got nil", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestRandomizeHeaders(t *testing.T) {
	// Zero headers get filled with distinct non-zero values.
	o := Obfuscation{Enabled: true, Jc: 4, Jmin: 40, Jmax: 70, H1: 5}
	randomizeHeaders(&o)
	hs := [4]uint32{o.H1, o.H2, o.H3, o.H4}
	seen := map[uint32]bool{}
	for i, h := range hs {
		if h == 0 {
			t.Fatalf("H%d not randomized", i+1)
		}
		if seen[h] {
			t.Fatalf("duplicate header value %d", h)
		}
		seen[h] = true
	}
	if o.H1 != 5 {
		t.Fatalf("pre-set header H1 overwritten: %d", o.H1)
	}
	// Disabled profiles are untouched.
	p := Obfuscation{}
	randomizeHeaders(&p)
	if p != (Obfuscation{}) {
		t.Fatal("plain profile mutated")
	}
}
