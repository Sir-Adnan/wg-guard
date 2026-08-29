package device

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

func newService(t *testing.T) (*Service, *iface.Service) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "d.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
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
	ifaceSvc := iface.NewService(db, reg, ring)
	return NewService(db, ring), ifaceSvc
}

func newKeyPair(t *testing.T, ring *secrets.KeyRing) KeyMaterial {
	t.Helper()
	kp, err := tunnel.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privEnc, err := ring.Encrypt([]byte(kp.Private))
	if err != nil {
		t.Fatal(err)
	}
	psk, err := tunnel.GeneratePresharedKey()
	if err != nil {
		t.Fatal(err)
	}
	pskEnc, err := ring.Encrypt([]byte(psk))
	if err != nil {
		t.Fatal(err)
	}
	return KeyMaterial{PublicKey: kp.Public, PrivateKeyEnc: privEnc, PresharedEnc: pskEnc}
}

func TestCreateAndRoundTrip(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	ifc, err := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	kp := newKeyPair(t, svc.ring)
	d, err := svc.Create(ctx, "u1", "phone", kp, ifc.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.IPv4 != "10.8.0.2/32" {
		t.Fatalf("first device IP = %q, want 10.8.0.2/32 (network+1 reserved)", d.IPv4)
	}
	got, err := svc.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicKey != kp.PublicKey || got.InterfaceID != ifc.ID || !got.Enabled {
		t.Fatalf("round trip broken: %+v", got)
	}
	// Sequential allocation walks the pool.
	d2, err := svc.Create(ctx, "u1", "laptop", newKeyPair(t, svc.ring), ifc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d2.IPv4 != "10.8.0.3/32" {
		t.Fatalf("second device IP = %q", d2.IPv4)
	}
}

func TestDeviceLimitEnforced(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	ifc, _ := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0"})
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, device_limit, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', 1, 2, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	if _, err := svc.Create(ctx, "u1", "phone", newKeyPair(t, svc.ring), ifc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, "u1", "laptop", newKeyPair(t, svc.ring), ifc.ID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, "u1", "tablet", newKeyPair(t, svc.ring), ifc.ID)
	if domain.CodeOf(err) != domain.CodeDeviceLimitReached {
		t.Fatalf("want DEVICE_LIMIT_REACHED, got %v", err)
	}
	if n, _ := svc.CountForUser(ctx, "u1"); n != 2 {
		t.Fatalf("count = %d", n)
	}
}

// TestDeviceLimitRace is an acceptance criterion: N concurrent creates
// against a limit of K must land exactly K devices — never K+1.
func TestDeviceLimitRace(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	ifc, _ := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0"})
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, device_limit, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', 1, 10, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	const (
		goroutines = 50
		limit      = 10
	)
	var (
		wg        sync.WaitGroup
		succeeded atomic.Int64
		rejected  atomic.Int64
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.Create(ctx, "u1", "dev-"+itoa(i), newKeyPair(t, svc.ring), ifc.ID)
			switch {
			case err == nil:
				succeeded.Add(1)
			case domain.CodeOf(err) == domain.CodeDeviceLimitReached:
				rejected.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if succeeded.Load() != limit {
		t.Fatalf("limit race: %d devices created, want exactly %d", succeeded.Load(), limit)
	}
	if rejected.Load() != goroutines-limit {
		t.Fatalf("rejections = %d, want %d", rejected.Load(), goroutines-limit)
	}
	if n, _ := svc.CountForUser(ctx, "u1"); n != limit {
		t.Fatalf("device count = %d, want %d", n, limit)
	}
}

func TestDisabledUserCannotAddDevices(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	ifc, _ := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0"})
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, created_at, updated_at)
		VALUES ('u1', 'alice', 'suspended', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if _, err := svc.Create(ctx, "u1", "phone", newKeyPair(t, svc.ring), ifc.ID); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("want FORBIDDEN, got %v", err)
	}
}

func TestPoolExhaustion(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	// /29 pool: 8 addresses − network − broadcast − gateway = 5 devices.
	ifc, err := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0", Subnet: "10.77.0.0/29"})
	if err != nil {
		t.Fatalf("create /29: %v", err)
	}
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	for i := 1; i <= 5; i++ {
		if _, err := svc.Create(ctx, "u1", "dev-"+itoa(i), newKeyPair(t, svc.ring), ifc.ID); err != nil {
			t.Fatalf("device %d: %v", i, err)
		}
	}
	_, err = svc.Create(ctx, "u1", "extra", newKeyPair(t, svc.ring), ifc.ID)
	if domain.CodeOf(err) != domain.CodeDevicePoolExhausted {
		t.Fatalf("want DEVICE_POOL_EXHAUSTED, got %v", err)
	}
}

func TestDuplicateKeyRejected(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	ifc, _ := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0"})
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	kp := newKeyPair(t, svc.ring)
	if _, err := svc.Create(ctx, "u1", "phone", kp, ifc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, "u1", "tablet", kp, ifc.ID); domain.CodeOf(err) != domain.CodeDeviceKeyExists {
		t.Fatalf("want DEVICE_KEY_EXISTS, got %v", err)
	}
}

func TestDeleteReleasesIP(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	ifc, _ := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0"})
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	d1, _ := svc.Create(ctx, "u1", "phone", newKeyPair(t, svc.ring), ifc.ID)
	d2, _ := svc.Create(ctx, "u1", "laptop", newKeyPair(t, svc.ring), ifc.ID)
	if err := svc.Delete(ctx, d1.ID); err != nil {
		t.Fatal(err)
	}
	d3, err := svc.Create(ctx, "u1", "tablet", newKeyPair(t, svc.ring), ifc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d3.IPv4 != d1.IPv4 {
		t.Fatalf("released IP not reused: %s vs %s", d3.IPv4, d1.IPv4)
	}
	_ = d2
}

func TestLifecycleAndRegenerate(t *testing.T) {
	svc, ifaceSvc := newService(t)
	ctx := context.Background()
	ifc, _ := ifaceSvc.Create(ctx, iface.CreateInput{Name: "awg0"})
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, enabled, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	d, _ := svc.Create(ctx, "u1", "phone", newKeyPair(t, svc.ring), ifc.ID)

	if err := svc.SetEnabled(ctx, d.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(ctx, d.ID)
	if got.Enabled {
		t.Fatal("disable not persisted")
	}
	newKeys := newKeyPair(t, svc.ring)
	if err := svc.Regenerate(ctx, d.ID, newKeys); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Get(ctx, d.ID)
	if got.PublicKey != newKeys.PublicKey {
		t.Fatal("keys not rotated")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
