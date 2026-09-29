package integration

import (
	"context"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

func nextPlanFixture(t *testing.T) (*Service, *reseller.Service, *secrets.KeyRing, string, string) {
	t.Helper()
	svc, resellers, ring := purchaseEnv(t)
	ctx := context.Background()
	products, err := svc.Plans.List(ctx)
	if err != nil || len(products) != 1 {
		t.Fatalf("catalog fixture: %v %v", products, err)
	}
	duration := int64(2 * 86400)
	next, err := svc.Plans.Create(ctx, plan.Input{Name: "Successor", InterfaceID: domain.OptString{
		Set: true, Value: *products[0].InterfaceID}, TrafficLimitBytes: domain.OptInt64{Set: true, Value: 2_000_000},
		DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	return svc, resellers, ring, products[0].ID, next.ID
}

func buyNextPlanUser(t *testing.T, svc *Service, ring *secrets.KeyRing, planID, key string, resellerID *string) string {
	t.Helper()
	result, replay, err := svc.Purchase(context.Background(), PurchaseInput{
		Key: key, PlanID: planID, ResellerID: resellerID, Keys: purchaseKeys(t, ring),
	})
	if err != nil || replay {
		t.Fatalf("purchase: %+v %v %v", result, replay, err)
	}
	return result.UserID
}

func TestNextPlanQuotaActivationIsAtomicAndOneShot(t *testing.T) {
	svc, _, ring, currentID, nextID := nextPlanFixture(t)
	ctx := context.Background()
	uid := buyNextPlanUser(t, svc, ring, currentID, "next-plan-quota", nil)
	queued, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID})
	if err != nil || queued.State != "queued" || queued.Terms.TrafficLimitBytes == nil {
		t.Fatalf("queue: %+v %v", queued, err)
	}
	// Catalog edits after authorization must not rewrite queued terms.
	if _, err := svc.Plans.Update(ctx, nextID, plan.Input{TrafficLimitBytes: domain.OptInt64{Set: true, Value: 9_000_000}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE users SET traffic_used_rx = 600000,
		traffic_used_tx = 400000, status = 'traffic_exceeded' WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE devices SET rx_bytes = 600000, tx_bytes = 400000 WHERE user_id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	activated, err := svc.ActivateDueNextPlans(ctx)
	if err != nil || activated != 1 {
		t.Fatalf("activate: %d %v", activated, err)
	}
	u, err := svc.Users.Get(ctx, uid)
	if err != nil || u.PlanID == nil || *u.PlanID != nextID || u.TrafficLimitBytes == nil ||
		*u.TrafficLimitBytes != 2_000_000 || u.TrafficUsedRX != 0 || u.TrafficUsedTX != 0 ||
		u.Status != domain.UserActive || u.ExpiresAt == nil || !u.ExpiresAt.After(time.Now()) {
		t.Fatalf("activated entitlement: %+v %v", u, err)
	}
	var rx, tx, baselineRX, baselineTX int64
	if err := svc.DB.QueryRowContext(ctx, `SELECT rx_bytes, tx_bytes, last_rx, last_tx FROM devices WHERE user_id = ?`, uid).
		Scan(&rx, &tx, &baselineRX, &baselineTX); err != nil || rx != 0 || tx != 0 {
		t.Fatalf("device counters: %d %d %d %d %v", rx, tx, baselineRX, baselineTX, err)
	}
	queued, err = svc.NextPlanForUser(ctx, uid, nil)
	if err != nil || queued != nil {
		t.Fatalf("queue survived activation: %+v %v", queued, err)
	}
	activated, err = svc.ActivateDueNextPlans(ctx)
	if err != nil || activated != 0 {
		t.Fatalf("duplicate activation: %d %v", activated, err)
	}
	var history int
	_ = svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM next_plan_activations WHERE user_id = ?`, uid).Scan(&history)
	if history != 1 {
		t.Fatalf("activation history count: %d", history)
	}
	activations, err := svc.NextPlanActivations(ctx, uid, nil, 20)
	if err != nil || len(activations) != 1 || activations[0].Trigger != "quota" ||
		len(activations[0].Before) == 0 || len(activations[0].After) == 0 {
		t.Fatalf("recoverable activation: %+v %v", activations, err)
	}
	if pruned, err := PruneNextPlanHistory(ctx, svc.DB, time.Now()); err != nil || pruned != 0 {
		t.Fatalf("recent history pruned: %d %v", pruned, err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE next_plan_activations SET activated_at = ? WHERE user_id = ?`,
		time.Now().AddDate(-2, 0, 0).UTC().Format(time.RFC3339Nano), uid); err != nil {
		t.Fatal(err)
	}
	if pruned, err := PruneNextPlanHistory(ctx, svc.DB, time.Now()); err != nil || pruned != 1 {
		t.Fatalf("old history retention: %d %v", pruned, err)
	}
}

func TestNextPlanTimeCarryAndManualBlock(t *testing.T) {
	svc, _, ring, currentID, nextID := nextPlanFixture(t)
	ctx := context.Background()
	uid := buyNextPlanUser(t, svc, ring, currentID, "next-plan-time", nil)
	if _, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID, CarryUnusedTraffic: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE users SET traffic_used_rx = 200000,
		expires_at = ?, status = 'suspended', enabled = 0 WHERE id = ?`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), uid); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ActivateDueNextPlans(ctx); err != nil || n != 0 {
		t.Fatalf("manual block bypassed: %d %v", n, err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE users SET status = 'expired', enabled = 1 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ActivateDueNextPlans(ctx); err != nil || n != 1 {
		t.Fatalf("time activation: %d %v", n, err)
	}
	u, err := svc.Users.Get(ctx, uid)
	if err != nil || u.TrafficLimitBytes == nil || *u.TrafficLimitBytes != 2_800_000 {
		t.Fatalf("unused data carry: %+v %v", u, err)
	}
	var trigger string
	if err := svc.DB.QueryRowContext(ctx, `SELECT trigger_kind FROM next_plan_activations WHERE user_id = ?`, uid).Scan(&trigger); err != nil || trigger != "time" {
		t.Fatalf("trigger: %q %v", trigger, err)
	}
}

func TestNextPlanRequiresOwnedCustomerAndAssignedPlan(t *testing.T) {
	svc, resellers, ring, currentID, nextID := nextPlanFixture(t)
	ctx := context.Background()
	north, err := resellers.Create(ctx, "north", "North", []string{"users.read", "next_plans.read", "next_plans.write"})
	if err != nil {
		t.Fatal(err)
	}
	south, err := resellers.Create(ctx, "south", "South", []string{"users.read", "next_plans.read", "next_plans.write"})
	if err != nil {
		t.Fatal(err)
	}
	if err := resellers.SetPlans(ctx, north.ID, []string{currentID}); err != nil {
		t.Fatal(err)
	}
	uid := buyNextPlanUser(t, svc, ring, currentID, "north-next", &north.ID)
	if _, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID, ResellerID: &south.ID}); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("foreign queue: %v", err)
	}
	if _, err := svc.NextPlanForUser(ctx, uid, &south.ID); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("foreign read: %v", err)
	}
	if err := svc.CancelNextPlan(ctx, uid, &south.ID); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("foreign cancel: %v", err)
	}
	if _, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID, ResellerID: &north.ID}); domain.CodeOf(err) != domain.CodeForbidden {
		t.Fatalf("unassigned successor: %v", err)
	}
	if err := resellers.SetPlans(ctx, north.ID, []string{currentID, nextID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID, ResellerID: &north.ID}); err != nil {
		t.Fatalf("assigned successor: %v", err)
	}
	if err := resellers.SetPlans(ctx, north.ID, []string{currentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE users SET traffic_used_rx = 1000000 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ActivateDueNextPlans(ctx); err != nil || n != 1 {
		t.Fatalf("paid successor lost when catalog access was revoked: %d %v", n, err)
	}
}

func TestNextPlanChangedCurrentPlanNeedsReview(t *testing.T) {
	svc, _, ring, currentID, nextID := nextPlanFixture(t)
	ctx := context.Background()
	uid := buyNextPlanUser(t, svc, ring, currentID, "changed-current", nil)
	if _, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE users SET plan_id = ?, traffic_used_rx = 1000000 WHERE id = ?`, nextID, uid); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ActivateDueNextPlans(ctx); err != nil || n != 0 {
		t.Fatalf("changed plan activated stale queue: %d %v", n, err)
	}
	queued, err := svc.NextPlanForUser(ctx, uid, nil)
	if err != nil || queued == nil || queued.State != "needs_review" || queued.ReviewReason != "current_plan_changed" {
		t.Fatalf("review state: %+v %v", queued, err)
	}
}

func TestNextPlanUnavailableInterfaceNeedsReview(t *testing.T) {
	svc, _, ring, currentID, nextID := nextPlanFixture(t)
	ctx := context.Background()
	uid := buyNextPlanUser(t, svc, ring, currentID, "interface-review", nil)
	if _, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE tunnel_interfaces SET enabled = 0 WHERE id = (SELECT interface_id FROM plans WHERE id = ?)`, nextID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.ExecContext(ctx, `UPDATE users SET traffic_used_rx = 1000000 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ActivateDueNextPlans(ctx); err != nil || n != 0 {
		t.Fatalf("unavailable interface activated successor: %d %v", n, err)
	}
	queued, err := svc.NextPlanForUser(ctx, uid, nil)
	if err != nil || queued == nil || queued.ReviewReason != "interface_unavailable" {
		t.Fatalf("review state: %+v %v", queued, err)
	}
}

func TestNextPlanExactSecondExpiryIsNotSkipped(t *testing.T) {
	svc, _, ring, currentID, nextID := nextPlanFixture(t)
	ctx := context.Background()
	uid := buyNextPlanUser(t, svc, ring, currentID, "exact-second-expiry", nil)
	if _, err := svc.QueueNextPlan(ctx, QueueNextPlanInput{UserID: uid, PlanID: nextID}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second).Add(700 * time.Millisecond)
	svc.Now = func() time.Time { return now }
	if _, err := svc.DB.ExecContext(ctx, `UPDATE users SET expires_at = ? WHERE id = ?`,
		now.Truncate(time.Second).Format(time.RFC3339Nano), uid); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ActivateDueNextPlans(ctx); err != nil || n != 1 {
		t.Fatalf("exact-second expiry missed: %d %v", n, err)
	}
}
