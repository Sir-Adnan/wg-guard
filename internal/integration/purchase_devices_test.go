package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func TestMultiDevicePurchaseAtomicReplayAndRecovery(t *testing.T) {
	svc, _, ring := purchaseEnv(t)
	ctx := context.Background()
	products, _ := svc.Plans.List(ctx)
	in := PurchaseInput{Key: "three-devices", TemplateID: products[0].ID, DeviceName: "phone",
		DeviceKeys: []device.KeyMaterial{purchaseKeys(t, ring), purchaseKeys(t, ring), purchaseKeys(t, ring)}}
	first, replayed, err := svc.Purchase(ctx, in)
	if err != nil || replayed || len(first.DeviceIDs) != 3 || first.DeviceID != first.DeviceIDs[0] {
		t.Fatalf("multi-device purchase: %+v %v %v", first, replayed, err)
	}
	seenKeys, seenIPs := map[string]bool{}, map[string]bool{}
	for i, id := range first.DeviceIDs {
		d, err := svc.Devices.Get(ctx, id)
		if err != nil || d.UserID != first.UserID || d.Name != []string{"phone-1", "phone-2", "phone-3"}[i] {
			t.Fatalf("device %d: %+v %v", i, d, err)
		}
		if seenKeys[d.PublicKey] || seenIPs[d.IPv4] {
			t.Fatal("devices share a key or address")
		}
		seenKeys[d.PublicKey], seenIPs[d.IPv4] = true, true
	}
	link, err := svc.Links.ForUser(ctx, first.UserID)
	if err != nil || link == nil {
		t.Fatalf("missing customer link: %v", err)
	}
	var stored string
	if err := svc.DB.QueryRowContext(ctx, `SELECT result_json FROM integration_operations WHERE id = ?`, first.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for _, keys := range in.DeviceKeys {
		if containsSecret(stored, link.Token, keys.PrivateKeyEnc) {
			t.Fatal("private material leaked into operation result")
		}
	}
	// Concurrent redeliveries with fresh keys must all recover the same IDs.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		retry := in
		retry.DeviceKeys = []device.KeyMaterial{purchaseKeys(t, ring), purchaseKeys(t, ring), purchaseKeys(t, ring)}
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, replayed, err := svc.Purchase(ctx, retry)
			if err != nil || !replayed || got.ID != first.ID || !reflect.DeepEqual(got.DeviceIDs, first.DeviceIDs) {
				t.Errorf("concurrent replay: %+v %v %v", got, replayed, err)
			}
		}()
	}
	wg.Wait()
	got, err := svc.Lookup(ctx, nil, in.Key)
	if err != nil || !reflect.DeepEqual(got.DeviceIDs, first.DeviceIDs) {
		t.Fatalf("recover all device IDs: %+v %v", got, err)
	}
	in.DeviceKeys = in.DeviceKeys[:2]
	if _, _, err := svc.Purchase(ctx, in); domain.CodeOf(err) != domain.CodeIdempotencyKeyReused {
		t.Fatalf("changed device count accepted under same key: %v", err)
	}
	var count int
	if err := svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("retries changed device count: %d %v", count, err)
	}
}

func TestMultiDevicePurchaseRollsBackEveryResource(t *testing.T) {
	for _, failure := range []string{"device_limit", "duplicate_key", "name_suffix"} {
		t.Run(failure, func(t *testing.T) {
			svc, _, ring := purchaseEnv(t)
			ctx := context.Background()
			in := PurchaseInput{Key: "rollback", Username: "rollback", Entitlement: &DirectEntitlement{
				TrafficLimitBytes: 1_000_000, DurationSeconds: 86400, DeviceLimit: 2},
				DeviceKeys: []device.KeyMaterial{purchaseKeys(t, ring), purchaseKeys(t, ring)}}
			switch failure {
			case "device_limit":
				in.Entitlement.DeviceLimit = 1
			case "duplicate_key":
				in.DeviceKeys[1] = in.DeviceKeys[0] // fail after first device insertion
			case "name_suffix":
				in.DeviceName = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789x"
			}
			if _, _, err := svc.Purchase(ctx, in); err == nil {
				t.Fatal("invalid multi-device purchase succeeded")
			}
			for _, table := range []string{"users", "devices", "sub_links", "integration_operations"} {
				var count int
				if err := svc.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial purchase retained %s: %d %v", table, count, err)
				}
			}
			in.Entitlement.DeviceLimit = 2
			in.DeviceName = ""
			in.DeviceKeys[1] = purchaseKeys(t, ring)
			if got, replayed, err := svc.Purchase(ctx, in); err != nil || replayed || len(got.DeviceIDs) != 2 {
				t.Fatalf("retry after rollback: %+v %v %v", got, replayed, err)
			}
		})
	}
}

func TestPurchaseDeviceBatchBounds(t *testing.T) {
	svc, _, ring := purchaseEnv(t)
	ctx := context.Background()
	products, _ := svc.Plans.List(ctx)
	in := PurchaseInput{Key: "bounded", TemplateID: products[0].ID}
	if _, _, err := svc.Purchase(ctx, in); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("empty key batch accepted: %v", err)
	}
	for i := 0; i < MaxPurchaseDevices; i++ {
		in.DeviceKeys = append(in.DeviceKeys, purchaseKeys(t, ring))
	}
	got, replayed, err := svc.Purchase(ctx, in)
	if err != nil || replayed || len(got.DeviceIDs) != MaxPurchaseDevices {
		t.Fatalf("maximum valid batch: %+v %v %v", got, replayed, err)
	}
	in.Key = "too-many"
	in.DeviceKeys = append(in.DeviceKeys, purchaseKeys(t, ring))
	if _, _, err := svc.Purchase(ctx, in); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("oversized key batch accepted: %v", err)
	}
}

func TestPurchaseRecoversStoredSingleDeviceResult(t *testing.T) {
	svc, _, ring := purchaseEnv(t)
	ctx := context.Background()
	products, _ := svc.Plans.List(ctx)
	in := PurchaseInput{Key: "saved-single", Username: "saved-single", TemplateID: products[0].ID,
		DeviceKeys: []device.KeyMaterial{purchaseKeys(t, ring)}}
	first, _, err := svc.Purchase(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the fingerprint and response shape already persisted by the
	// published single-device purchase implementation.
	canonical, err := json.Marshal(struct{ Username, TemplateID, DeviceName string }{
		in.Username, in.TemplateID, "device-1"})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(canonical)
	first.DeviceIDs = nil
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE integration_operations SET request_hash = ?, result_json = ? WHERE id = ?`,
		hex.EncodeToString(hash[:]), string(raw), first.ID); err != nil {
		t.Fatal(err)
	}
	got, replayed, err := svc.Purchase(ctx, in)
	if err != nil || !replayed || got.DeviceID != first.DeviceID || got.DeviceIDs != nil {
		t.Fatalf("stored single-device operation cannot replay: %+v %v %v", got, replayed, err)
	}
}
