package iface

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
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

func mustU32Range(t *testing.T, text string) awgparam.U32Range {
	t.Helper()
	value, err := awgparam.ParseU32Range(text)
	if err != nil {
		t.Fatalf("parse u32 range %q: %v", text, err)
	}
	return value
}

func mustU16Range(t *testing.T, text string) awgparam.U16Range {
	t.Helper()
	value, err := awgparam.ParseU16Range(text)
	if err != nil {
		t.Fatalf("parse u16 range %q: %v", text, err)
	}
	return value
}

func TestObfuscationRangeRoundTrip(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	obf := balancedObfuscation()
	obf.H1 = mustU32Range(t, "100-199")
	obf.H2 = mustU32Range(t, "200-299")
	obf.H3 = mustU32Range(t, "300-399")
	obf.H4 = mustU32Range(t, "400-499")
	obf.ContentPaddingAddition = mustU16Range(t, "10-20")
	obf.RekeyAfterTime = mustU16Range(t, "120-180")
	obf.RekeyTimeout = mustU16Range(t, "5-10")
	obf.RejectAfterTime = mustU16Range(t, "180-240")
	obf.KeepaliveTimeout = mustU16Range(t, "15-25")
	obf.MaxHandshakeAttempts = mustU16Range(t, "5-8")

	created, err := svc.Create(ctx, CreateInput{Name: "awg0", Obfuscation: obf})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Obfuscation != obf {
		t.Fatalf("round trip changed ranges:\n got: %+v\nwant: %+v", got.Obfuscation, obf)
	}
	listed, err := svc.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].Obfuscation != obf {
		t.Fatalf("list round trip = %+v, %v", listed, err)
	}

	var legacy [4]int64
	var canonical [4]string
	err = svc.db.QueryRow(`SELECT h1, h2, h3, h4, h1_range, h2_range, h3_range, h4_range
		FROM tunnel_interfaces WHERE id = ?`, created.ID).Scan(
		&legacy[0], &legacy[1], &legacy[2], &legacy[3],
		&canonical[0], &canonical[1], &canonical[2], &canonical[3])
	if err != nil {
		t.Fatal(err)
	}
	if legacy != [4]int64{100, 200, 300, 400} {
		t.Fatalf("legacy low-bound mirror = %#v", legacy)
	}
	if canonical != [4]string{"100-199", "200-299", "300-399", "400-499"} {
		t.Fatalf("canonical ranges = %#v", canonical)
	}

	updated := obf
	updated.H1 = mustU32Range(t, "500-599")
	updated.H2 = mustU32Range(t, "600-699")
	updated.H3 = mustU32Range(t, "700-799")
	updated.H4 = mustU32Range(t, "800-899")
	got, err = svc.Update(ctx, created.ID, UpdateInput{Obfuscation: &updated})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Obfuscation != updated {
		t.Fatalf("update changed ranges: %+v", got.Obfuscation)
	}

	overlap := updated
	overlap.H2 = mustU32Range(t, "550-650")
	if err := ValidateObfuscation(overlap); err == nil {
		t.Fatal("overlapping H intervals accepted")
	}
	disabled := Obfuscation{H1: mustU32Range(t, "5-9")}
	if err := ValidateObfuscation(disabled); err == nil {
		t.Fatal("disabled profile with a range accepted")
	}
}

func balancedObfuscation() Obfuscation {
	return Obfuscation{
		Enabled: true,
		Jc:      4, Jmin: 40, Jmax: 70, S1: 15, S2: 64,
		H1: awgparam.ScalarU32(1), H2: awgparam.ScalarU32(2),
		H3: awgparam.ScalarU32(3), H4: awgparam.ScalarU32(4),
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
	if !got.Obfuscation.Enabled || got.Obfuscation.Jc != 4 || got.Obfuscation.H4 != awgparam.ScalarU32(4) {
		t.Fatalf("obfuscation round trip broken: %+v", got.Obfuscation)
	}
	if got.Obfuscation.I1 != "" {
		t.Fatalf("I1 should be unset: %q", got.Obfuscation.I1)
	}
}

// TestCreateDefaultPoolSetting: network.default_pool drives the subnet
// offered to awg0 when its subnet is left blank (the installer seeds it);
// later interfaces keep the 10.8.N.0/24 ladder.
func TestCreateDefaultPoolSetting(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if err := svc.reg.Set(ctx, "network.default_pool", "10.77.0.0/24"); err != nil {
		t.Fatalf("set pool: %v", err)
	}
	ifc, err := svc.Create(ctx, CreateInput{Name: "awg0", Obfuscation: balancedObfuscation()})
	if err != nil {
		t.Fatalf("create awg0: %v", err)
	}
	if ifc.Subnet != "10.77.0.0/24" {
		t.Fatalf("awg0 subnet = %q, want the configured pool", ifc.Subnet)
	}
	next, err := svc.Create(ctx, CreateInput{Name: "awg1", Obfuscation: balancedObfuscation()})
	if err != nil {
		t.Fatalf("create awg1: %v", err)
	}
	if next.Subnet != "10.8.1.0/24" {
		t.Fatalf("awg1 subnet = %q, want the ladder default", next.Subnet)
	}
	// An explicitly requested subnet always wins.
	explicit, err := svc.Create(ctx, CreateInput{Name: "awg2", Subnet: "10.78.0.0/24", Obfuscation: balancedObfuscation()})
	if err != nil {
		t.Fatalf("create awg2: %v", err)
	}
	if explicit.Subnet != "10.78.0.0/24" {
		t.Fatalf("awg2 subnet = %q", explicit.Subnet)
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
		{"duplicate H", func() Obfuscation { o := balancedObfuscation(); o.H2 = awgparam.ScalarU32(1); return o }(), false},
		{"zero H", func() Obfuscation { o := balancedObfuscation(); o.H4 = awgparam.U32Range{}; return o }(), false},
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
	base := balancedObfuscation()
	cases := []struct {
		name    string
		mutate  func(*Obfuscation)
		wantErr bool
	}{
		{"plain defaults ok", func(o *Obfuscation) {}, false},
		{"S3/S4 in range", func(o *Obfuscation) { o.S3 = 40; o.S4 = 100 }, false},
		{"S3 over u16", func(o *Obfuscation) { o.S3 = 70000 }, true},
		{"padding single value", func(o *Obfuscation) { o.ContentPaddingAddition = awgparam.ScalarU16(10) }, false},
		{"padding range", func(o *Obfuscation) { o.ContentPaddingAddition = mustU16Range(t, "10-20") }, false},
		{"rekey range ok", func(o *Obfuscation) { o.RekeyAfterTime = mustU16Range(t, "120-180") }, false},
		{"hpk valid with S3/S4", func(o *Obfuscation) {
			o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
			o.S3 = 24
			o.S4 = 24
		}, false},
		{"hpk without S3/S4 rejected (kernel constraint)", func(o *Obfuscation) {
			o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
		}, true},
		{"hpk with S3 only rejected", func(o *Obfuscation) {
			o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
			o.S3 = 24
		}, true},
		{"hpk with S1 below nonce rejected", func(o *Obfuscation) {
			o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
			o.S1 = 11
			o.S3 = 24
			o.S4 = 24
		}, true},
		{"hpk wrong length", func(o *Obfuscation) {
			o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(make([]byte, 16))
		}, true},
		{"hpk not base64", func(o *Obfuscation) { o.HeaderProtectionKey = "not-base64!!" }, true},
		{"timers set", func(o *Obfuscation) {
			o.RekeyAfterTime = awgparam.ScalarU16(120)
			o.RekeyTimeout = mustU16Range(t, "5-10")
			o.RejectAfterTime = awgparam.ScalarU16(90)
			o.KeepaliveTimeout = awgparam.ScalarU16(25)
			o.MaxHandshakeAttempts = awgparam.ScalarU16(5)
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
	o := Obfuscation{Enabled: true, Jc: 4, Jmin: 40, Jmax: 70, H1: awgparam.ScalarU32(5)}
	randomizeHeaders(&o)
	hs := [4]awgparam.U32Range{o.H1, o.H2, o.H3, o.H4}
	seen := map[awgparam.U32Range]bool{}
	for i, h := range hs {
		if h.IsZero() {
			t.Fatalf("H%d not randomized", i+1)
		}
		if seen[h] {
			t.Fatalf("duplicate header value %s", h)
		}
		seen[h] = true
	}
	if o.H1 != awgparam.ScalarU32(5) {
		t.Fatalf("pre-set header H1 overwritten: %s", o.H1)
	}
	// Disabled profiles are untouched.
	p := Obfuscation{}
	randomizeHeaders(&p)
	if p != (Obfuscation{}) {
		t.Fatal("plain profile mutated")
	}
}
