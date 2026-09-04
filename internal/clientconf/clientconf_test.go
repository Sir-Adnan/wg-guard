package clientconf

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

const (
	literalPrivateKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	literalDeviceKey  = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	literalServerKey  = "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="
	literalPSK        = "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ="
	literalHPK        = "BQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQU="
)

type fullConfigFixture struct {
	db       *database.DB
	renderer *Renderer
	deviceID string
}

func newFullConfigFixture(t testing.TB) fullConfigFixture {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(filepath.Join(t.TempDir(), "clientconf.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{
		"node.endpoint":                       "ignored.example.com",
		"network.mtu":                         1420,
		"network.dns_servers":                 []string{"9.9.9.9", "149.112.112.112"},
		"network.client_allowed_ips":          "0.0.0.0/0, ::/0",
		"network.client_persistent_keepalive": "25-35",
		"downloads.filename_prefix":           "wg",
		"downloads.filename_suffix":           "v2",
	} {
		if err := registry.Set(ctx, key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	h1, _ := awgparam.ParseU32Range("100-110")
	h2, _ := awgparam.ParseU32Range("200-210")
	h3, _ := awgparam.ParseU32Range("300-310")
	h4, _ := awgparam.ParseU32Range("400-410")
	u16 := func(text string) awgparam.U16Range {
		value, err := awgparam.ParseU16Range(text)
		if err != nil {
			t.Fatalf("parse %s: %v", text, err)
		}
		return value
	}
	obfuscation := iface.Obfuscation{
		Enabled: true, Jc: 5, Jmin: 40, Jmax: 70,
		S1: 86, S2: 61, S3: 40, S4: 48,
		H1: h1, H2: h2, H3: h3, H4: h4,
		I1: "<r 90>", I2: "aabbcc", I4: "<b 0x01>",
		HeaderProtectionKey:    literalHPK,
		ContentPaddingAddition: u16("10-20"),
		RekeyAfterTime:         u16("120-180"),
		RekeyTimeout:           u16("5-10"),
		RejectAfterTime:        u16("200-240"),
		KeepaliveTimeout:       u16("15-25"),
		MaxHandshakeAttempts:   u16("4-8"),
		RandomTrailers:         true,
		DisableCookies:         true,
	}
	ifaces := iface.NewService(db, registry, ring)
	ifc, err := ifaces.Create(ctx, iface.CreateInput{
		Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24", MTU: 1380,
		EndpointOverride: "vpn.example.com", Obfuscation: obfuscation, Preset: "custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tunnel_interfaces SET public_key = ? WHERE id = ?`, literalServerKey, ifc.ID); err != nil {
		t.Fatal(err)
	}

	users := user.NewService(db)
	u, err := users.Create(ctx, user.Input{Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	privateEnvelope, err := ring.Encrypt([]byte(literalPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	pskEnvelope, err := ring.Encrypt([]byte(literalPSK))
	if err != nil {
		t.Fatal(err)
	}
	devices := device.NewService(db, ring)
	d, err := devices.Create(ctx, u.ID, "phone", device.KeyMaterial{
		PublicKey: literalDeviceKey, PrivateKeyEnc: privateEnvelope, PresharedEnc: pskEnvelope,
	}, ifc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return fullConfigFixture{
		db: db, deviceID: d.ID,
		renderer: &Renderer{Devices: devices, Ifaces: ifaces, Settings: registry},
	}
}

func BenchmarkConfigRender(b *testing.B) {
	fixture := newFullConfigFixture(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		config, err := fixture.renderer.Render(ctx, fixture.deviceID)
		if err != nil {
			b.Fatal(err)
		}
		if len(config) == 0 {
			b.Fatal("empty config")
		}
	}
}

func BenchmarkQRFullConfig(b *testing.B) {
	fixture := newFullConfigFixture(b)
	config, err := fixture.renderer.Render(context.Background(), fixture.deviceID)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		png, err := QR(config)
		if err != nil {
			b.Fatal(err)
		}
		if len(png) == 0 {
			b.Fatal("empty QR")
		}
	}
}

func TestRenderFullAWGConfig(t *testing.T) {
	fixture := newFullConfigFixture(t)
	got, err := fixture.renderer.Render(t.Context(), fixture.deviceID)
	if err != nil {
		t.Fatal(err)
	}
	want := "[Interface]\n" +
		"PrivateKey = " + literalPrivateKey + "\n" +
		"Address = 10.77.0.2/32\n" +
		"DNS = 9.9.9.9, 149.112.112.112\n" +
		"MTU = 1380\n" +
		"Jc = 5\n" +
		"Jmin = 40\n" +
		"Jmax = 70\n" +
		"S1 = 86\n" +
		"S2 = 61\n" +
		"S3 = 40\n" +
		"S4 = 48\n" +
		"H1 = 100-110\n" +
		"H2 = 200-210\n" +
		"H3 = 300-310\n" +
		"H4 = 400-410\n" +
		"I1 = <r 90>\n" +
		"I2 = aabbcc\n" +
		"I4 = <b 0x01>\n" +
		"HeaderProtectionKey = " + literalHPK + "\n" +
		"ContentPaddingAddition = 10-20\n" +
		"RekeyAfterTime = 120-180\n" +
		"RekeyTimeout = 5-10\n" +
		"RejectAfterTime = 200-240\n" +
		"KeepaliveTimeout = 15-25\n" +
		"MaxHandshakeAttempts = 4-8\n" +
		"RandomTrailers = on\n" +
		"DisableCookies = on\n" +
		"\n[Peer]\n" +
		"PublicKey = " + literalServerKey + "\n" +
		"PresharedKey = " + literalPSK + "\n" +
		"AllowedIPs = 0.0.0.0/0, ::/0\n" +
		"Endpoint = vpn.example.com:39001\n" +
		"PersistentKeepalive = 25-35\n"
	if got != want {
		t.Fatalf("canonical config mismatch: got %d bytes, want %d; first difference at byte %d",
			len(got), len(want), firstDifference(got, want))
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Fatal("canonical config must end with exactly one newline")
	}
}

func TestRenderRejectsInvalidStoredPersistentKeepalive(t *testing.T) {
	fixture := newFullConfigFixture(t)
	if _, err := fixture.db.Exec(`UPDATE settings SET value = '25--35' WHERE key = 'network.client_persistent_keepalive'`); err != nil {
		t.Fatal(err)
	}
	config, err := fixture.renderer.Render(t.Context(), fixture.deviceID)
	if err == nil {
		t.Fatalf("invalid stored keepalive produced a %d-byte config", len(config))
	}
	if !strings.Contains(err.Error(), "persistent keepalive") || strings.Contains(err.Error(), "25--35") {
		t.Fatalf("invalid keepalive error is not explicit and redacted: %v", err)
	}
}

func firstDifference(left, right string) int {
	limit := min(len(left), len(right))
	for i := 0; i < limit; i++ {
		if left[i] != right[i] {
			return i
		}
	}
	return limit
}
