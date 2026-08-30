package user

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "u.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return NewService(db)
}

func TestCreateImmediate(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	dur := int64(30 * 24 * 3600)
	u, err := svc.Create(ctx, Input{
		Username: "alice", DisplayName: strPtr("Alice"), Tags: []string{"vip", "sold"},
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 100 << 30},
		DeviceLimit:       domain.OptInt{Set: true, Value: 3}, DurationSeconds: &dur,
		SpeedLimitDownKbps: domain.OptInt{Set: true, Value: 10240},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.Status != domain.UserActive {
		t.Fatalf("immediate user must be active, got %s", u.Status)
	}
	if u.ActivatedAt == nil || u.ExpiresAt == nil {
		t.Fatal("immediate user must have activation and expiry")
	}
	if !u.ExpiresAt.After(u.ActivatedAt.Add(29 * 24 * time.Hour)) {
		t.Fatalf("expiry wrong: %v", u.ExpiresAt)
	}
	// Round trip.
	got, err := svc.GetByUsername(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Alice" || len(got.Tags) != 2 || got.Tags[0] != "vip" {
		t.Fatalf("fields lost: %+v", got)
	}
	if got.TrafficLimitBytes == nil || *got.TrafficLimitBytes != 100<<30 {
		t.Fatalf("traffic limit lost: %v", got.TrafficLimitBytes)
	}
	if got.SpeedLimitDownKbps == nil || *got.SpeedLimitDownKbps != 10240 || got.SpeedLimitUpKbps != nil {
		t.Fatalf("speed limits lost: %v %v", got.SpeedLimitDownKbps, got.SpeedLimitUpKbps)
	}
	// Tri-state PATCH: absent keeps, null clears.
	if _, err := svc.Update(ctx, got.ID, Input{SpeedLimitUpKbps: domain.OptInt{Set: true, Value: 5120}}); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Get(ctx, got.ID)
	if got.SpeedLimitUpKbps == nil || *got.SpeedLimitUpKbps != 5120 || got.SpeedLimitDownKbps == nil || *got.SpeedLimitDownKbps != 10240 {
		t.Fatalf("independent limits broken: %v %v", got.SpeedLimitDownKbps, got.SpeedLimitUpKbps)
	}
	if _, err := svc.Update(ctx, got.ID, Input{SpeedLimitDownKbps: domain.OptInt{Set: true, Null: true}}); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Get(ctx, got.ID)
	if got.SpeedLimitDownKbps != nil || got.SpeedLimitUpKbps == nil {
		t.Fatalf("null must clear only the down limit: %v %v", got.SpeedLimitDownKbps, got.SpeedLimitUpKbps)
	}
}

func TestCreateFirstConnection(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	u, err := svc.Create(ctx, Input{Username: "bob", StartPolicy: domain.StartFirstConnection, DurationSeconds: i64Ptr(86400)})
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != domain.UserWaitingFirstConnection {
		t.Fatalf("first_connection user must wait, got %s", u.Status)
	}
	if u.ActivatedAt != nil || u.ExpiresAt != nil {
		t.Fatal("waiting user must not be activated")
	}
	// Activation itself is the accounting cycle's transactional job
	// (internal/accounting); here only the waiting state contract is tested.
}

func TestUsernameUniqueness(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Username: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, Input{Username: "alice"}); domain.CodeOf(err) != domain.CodeUsernameExists {
		t.Fatalf("want USERNAME_EXISTS, got %v", err)
	}
	// Even soft-deleted users keep the name reserved.
	u, _ := svc.GetByUsername(ctx, "alice")
	_ = svc.SoftDelete(ctx, u.ID)
	if _, err := svc.Create(ctx, Input{Username: "alice"}); domain.CodeOf(err) != domain.CodeUsernameExists {
		t.Fatal("deleted user's username must stay reserved")
	}
}

func TestUsernameValidation(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	for _, name := range []string{"ab", "", "has space", "عربي", "x-too-long-username-xxxxxxxxxxxxxxxxx"} {
		if _, err := svc.Create(ctx, Input{Username: name}); domain.CodeOf(err) != domain.CodeInvalidRequest {
			t.Errorf("username %q accepted", name)
		}
	}
}

func TestLifecycleTransitions(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	u, _ := svc.Create(ctx, Input{Username: "carol"})

	u, err := svc.SetStatus(ctx, u.ID, domain.UserSuspended, domain.DisableAdminAction)
	if err != nil || u.Status != domain.UserSuspended || u.DisableReason == nil {
		t.Fatalf("suspend: %v %+v", err, u)
	}
	u, err = svc.SetStatus(ctx, u.ID, domain.UserActive, "")
	if err != nil || u.Status != domain.UserActive || u.DisableReason != nil {
		t.Fatalf("reactivate: %v %+v", err, u)
	}
	// Suspend without a reason is refused.
	if _, err := svc.SetStatus(ctx, u.ID, domain.UserDisabled, ""); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("reasonless disable accepted: %v", err)
	}
}

func TestRenewModes(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	dur := int64(10 * 86400)
	u, _ := svc.Create(ctx, Input{Username: "dave", DurationSeconds: &dur})
	first := *u.ExpiresAt

	// from_expiration extends from the current expiry.
	u, err := svc.Renew(ctx, u.ID, "from_expiration", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !u.ExpiresAt.Equal(first.Add(10 * 86400 * time.Second)) {
		t.Fatalf("from_expiration wrong: %v != %v", u.ExpiresAt, first.Add(10*86400*time.Second))
	}
	// from_now ignores the existing expiry.
	u, err = svc.Renew(ctx, u.ID, "from_now", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if u.ExpiresAt.Before(first) {
		t.Fatalf("from_now produced a past expiry: %v", u.ExpiresAt)
	}
	// exact requires a date.
	if _, err := svc.Renew(ctx, u.ID, "exact", nil, nil); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("exact without date accepted: %v", err)
	}
	when := time.Now().Add(5 * 86400 * time.Hour).UTC()
	u, err = svc.Renew(ctx, u.ID, "exact", nil, &when)
	if err != nil || !u.ExpiresAt.Equal(when) {
		t.Fatalf("exact renew: %v %v", err, u.ExpiresAt)
	}

	// Renewal revives an expired account.
	if _, err := svc.db.Exec(`UPDATE users SET status='expired', disable_reason='expired' WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	u, err = svc.Renew(ctx, u.ID, "from_now", nil, nil)
	if err != nil || u.Status != domain.UserActive {
		t.Fatalf("renew must revive expired: %v %+v", err, u)
	}
}

func TestSoftDeleteRestore(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	u, _ := svc.Create(ctx, Input{Username: "erin"})
	if err := svc.SoftDelete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, u.ID); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("deleted user must 404: %v", err)
	}
	restored, err := svc.Restore(ctx, u.ID)
	if err != nil || restored.Status != domain.UserActive || restored.DeletedAt != nil {
		t.Fatalf("restore: %v %+v", err, restored)
	}
	// Restoring a live user is refused.
	if _, err := svc.Restore(ctx, u.ID); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("double restore accepted: %v", err)
	}
}

func strPtr(s string) *string { return &s }
func i64Ptr(v int64) *int64   { return &v }
func intPtr(v int) *int       { return &v }
