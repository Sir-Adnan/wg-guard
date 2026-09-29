package integration

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

func purchaseEnv(t *testing.T) (*Service, *reseller.Service, *secrets.KeyRing) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(filepath.Join(t.TempDir(), "integration.db"), database.Options{})
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
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	ifaces := iface.NewService(db, reg, ring)
	ifc, err := ifaces.Create(ctx, iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	plans := plan.NewService(db)
	duration := int64(86400)
	product, err := plans.Create(ctx, plan.Input{Name: "Basic", InterfaceID: domain.OptString{Set: true, Value: ifc.ID},
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 1000000}, DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Users: user.NewService(db), Devices: device.NewService(db, ring),
		Plans: plans, Links: subscription.NewService(db, ring)}
	_ = product
	return svc, reseller.NewService(db), ring
}

func purchaseKeys(t *testing.T, ring *secrets.KeyRing) device.KeyMaterial {
	t.Helper()
	kp, err := tunnel.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := ring.Encrypt([]byte(kp.Private))
	if err != nil {
		t.Fatal(err)
	}
	return device.KeyMaterial{PublicKey: kp.Public, PrivateKeyEnc: enc}
}

func TestPurchaseCommitsAndRecoversWithoutSecretsInJournal(t *testing.T) {
	svc, resellers, ring := purchaseEnv(t)
	ctx := context.Background()
	products, err := svc.Plans.List(ctx)
	if err != nil || len(products) != 1 {
		t.Fatalf("plan fixture: %v %v", products, err)
	}
	in := PurchaseInput{Key: "order-1", PlanID: products[0].ID, Username: "alice", Keys: purchaseKeys(t, ring)}
	first, replay, err := svc.Purchase(ctx, in)
	if err != nil || replay || first.UserID == "" || first.DeviceID == "" {
		t.Fatalf("purchase: %+v replay=%v err=%v", first, replay, err)
	}
	link, err := svc.Links.ForUser(ctx, first.UserID)
	if err != nil || link == nil || link.Token == "" {
		t.Fatalf("customer link missing: %v", err)
	}
	var stored string
	if err := svc.DB.QueryRowContext(ctx, `SELECT result_json FROM integration_operations WHERE id = ?`, first.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "" || containsSecret(stored, link.Token, in.Keys.PrivateKeyEnc) {
		t.Fatal("operation journal contains a capability or private material")
	}
	in.Keys = purchaseKeys(t, ring) // retries do not depend on newly generated keys
	second, replay, err := svc.Purchase(ctx, in)
	if err != nil || !replay || second.UserID != first.UserID || second.DeviceID != first.DeviceID {
		t.Fatalf("retry duplicated purchase: %+v %+v %v %v", first, second, replay, err)
	}
	lookedUp, err := svc.Lookup(ctx, nil, in.Key)
	if err != nil || lookedUp.ID != first.ID {
		t.Fatalf("result lookup: %+v %v", lookedUp, err)
	}
	in.Username = "other"
	if _, _, err := svc.Purchase(ctx, in); domain.CodeOf(err) != domain.CodeIdempotencyKeyReused {
		t.Fatalf("changed request reused key: %v", err)
	}
	r, err := resellers.Create(ctx, "north", "North", []string{"users.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, &r.ID, "order-1"); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-principal result lookup: %v", err)
	}
	var users, devices, operations int
	_ = svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)
	_ = svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&devices)
	_ = svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM integration_operations`).Scan(&operations)
	if users != 1 || devices != 1 || operations != 1 {
		t.Fatalf("retry changed row counts: users=%d devices=%d operations=%d", users, devices, operations)
	}
}

func TestResellerPurchaseRequiresAssignedPlan(t *testing.T) {
	svc, resellers, ring := purchaseEnv(t)
	ctx := context.Background()
	products, _ := svc.Plans.List(ctx)
	r, err := resellers.Create(ctx, "north", "North", []string{"users.read"})
	if err != nil {
		t.Fatal(err)
	}
	in := PurchaseInput{Key: "north-order", ResellerID: &r.ID, PlanID: products[0].ID, Keys: purchaseKeys(t, ring)}
	if _, _, err := svc.Purchase(ctx, in); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("unassigned plan accepted: %v", err)
	}
	if err := resellers.SetPlans(ctx, r.ID, []string{products[0].ID}); err != nil {
		t.Fatal(err)
	}
	result, replay, err := svc.Purchase(ctx, in)
	if err != nil || replay {
		t.Fatalf("assigned plan purchase: %+v %v %v", result, replay, err)
	}
	u, err := svc.Users.Get(ctx, result.UserID)
	if err != nil || u.ResellerID == nil || *u.ResellerID != r.ID || u.TrafficLimitBytes == nil || *u.TrafficLimitBytes != 1000000 {
		t.Fatalf("purchase ownership or plan limits: %+v %v", u, err)
	}
}

func TestFailedInitialDeviceRollsBackPurchaseAndKey(t *testing.T) {
	svc, _, ring := purchaseEnv(t)
	ctx := context.Background()
	products, _ := svc.Plans.List(ctx)
	in := PurchaseInput{Key: "retry-after-failure", PlanID: products[0].ID,
		Username: "rollback-customer", DeviceName: strings.Repeat("x", 65), Keys: purchaseKeys(t, ring)}
	if _, _, err := svc.Purchase(ctx, in); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("invalid device name: %v", err)
	}
	if _, err := svc.Users.GetByUsername(ctx, in.Username); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("failed purchase kept a user: %v", err)
	}
	if _, err := svc.Lookup(ctx, nil, in.Key); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("failed purchase kept an outcome: %v", err)
	}
	in.DeviceName = "phone"
	if _, replay, err := svc.Purchase(ctx, in); err != nil || replay {
		t.Fatalf("retry after rollback: replay=%v err=%v", replay, err)
	}
}

func TestConcurrentPurchaseWithOneKeyCommitsOnce(t *testing.T) {
	svc, _, ring := purchaseEnv(t)
	ctx := context.Background()
	products, _ := svc.Plans.List(ctx)
	in := PurchaseInput{Key: "concurrent-order", PlanID: products[0].ID, Keys: purchaseKeys(t, ring)}
	const callers = 8
	type outcome struct {
		id  string
		err error
	}
	results := make(chan outcome, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, _, err := svc.Purchase(ctx, in)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			results <- outcome{id: result.ID}
		}()
	}
	wg.Wait()
	close(results)
	var first string
	for result := range results {
		if result.err != nil || result.id == "" {
			t.Fatalf("concurrent purchase: %+v", result)
		}
		if first == "" {
			first = result.id
		} else if result.id != first {
			t.Fatalf("one key returned different operations: %s and %s", first, result.id)
		}
	}
	var count int
	if err := svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM integration_operations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed operations = %d, %v", count, err)
	}
}

func TestExpiredResultCannotBeReadAndIsPruned(t *testing.T) {
	svc, _, ring := purchaseEnv(t)
	ctx := context.Background()
	products, _ := svc.Plans.List(ctx)
	in := PurchaseInput{Key: "expired-order", PlanID: products[0].ID, Keys: purchaseKeys(t, ring)}
	result, _, err := svc.Purchase(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE integration_operations SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), result.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, nil, in.Key); domain.CodeOf(err) != domain.CodeNotFound {
		t.Fatalf("expired result readable: %v", err)
	}
	if rows, err := Prune(ctx, svc.DB, time.Now()); err != nil || rows != 1 {
		t.Fatalf("expired result prune: %d %v", rows, err)
	}
}

func containsSecret(stored, capability string, private []byte) bool {
	return strings.Contains(stored, capability) || strings.Contains(stored, string(private))
}
