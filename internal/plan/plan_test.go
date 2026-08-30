package plan

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "p.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return NewService(db)
}

func TestCreateAndRoundTrip(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	dur := int64(30 * 86400)
	traffic := int64(50 << 30)
	devices := 5
	speed := 20480
	p, err := svc.Create(ctx, Input{
		Name: "monthly-50", TrafficLimitBytes: domain.OptInt64{Set: true, Value: traffic},
		DurationSeconds: &dur,
		StartPolicy:     domain.StartFirstConnection, DeviceLimit: domain.OptInt{Set: true, Value: devices},
		SpeedLimitDownKbps: domain.OptInt{Set: true, Value: speed},
		SpeedLimitUpKbps:   domain.OptInt{Set: true, Value: speed / 2},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "monthly-50" || got.TrafficLimitBytes == nil || *got.TrafficLimitBytes != traffic ||
		got.DeviceLimit == nil || *got.DeviceLimit != 5 ||
		got.SpeedLimitDownKbps == nil || *got.SpeedLimitDownKbps != speed ||
		got.SpeedLimitUpKbps == nil || *got.SpeedLimitUpKbps != speed/2 {
		t.Fatalf("round trip broken: %+v", got)
	}
	if got.StartPolicy != domain.StartFirstConnection {
		t.Fatalf("start policy lost: %q", got.StartPolicy)
	}
	if !got.Enabled {
		t.Fatal("enabled lost")
	}
}

func TestValidation(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	negative := int64(-1)
	zero := 0
	cases := []struct {
		name string
		in   Input
	}{
		{"empty name", Input{Name: "  "}},
		{"negative traffic", Input{Name: "x", TrafficLimitBytes: domain.OptInt64{Set: true, Value: negative}}},
		{"zero duration", Input{Name: "x", DurationSeconds: i64Ptr(0)}},
		{"zero devices", Input{Name: "x", DeviceLimit: domain.OptInt{Set: true, Value: zero}}},
		{"zero speed down", Input{Name: "x", SpeedLimitDownKbps: domain.OptInt{Set: true, Value: 0}}},
		{"bad policy", Input{Name: "x", StartPolicy: "later"}},
	}
	for _, tc := range cases {
		if _, err := svc.Create(ctx, tc.in); err == nil {
			t.Errorf("%s accepted", tc.name)
		}
	}
}

func TestNameUniquenessAndDelete(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "gold"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, Input{Name: "gold"}); err == nil {
		t.Fatal("duplicate name accepted")
	}

	// Deletion blocked while referenced by a user.
	p, _ := svc.List(ctx)
	_, _ = svc.db.Exec(`INSERT INTO users (id, username, status, plan_id, created_at, updated_at)
		VALUES ('u1', 'alice', 'active', ?, 'now', 'now')`, p[0].ID)
	if err := svc.Delete(ctx, p[0].ID); domain.CodeOf(err) != domain.CodePlanInUse {
		t.Fatalf("want PLAN_IN_USE, got %v", err)
	}
	if _, err := svc.db.Exec(`DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, p[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, p[0].ID); domain.CodeOf(err) != domain.CodePlanNotFound {
		t.Fatalf("want PLAN_NOT_FOUND, got %v", err)
	}
}

func TestUpdatePartial(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	p, err := svc.Create(ctx, Input{Name: "silver"})
	if err != nil {
		t.Fatal(err)
	}
	enabled := false
	updated, err := svc.Update(ctx, p.ID, Input{DeviceLimit: domain.OptInt{Set: true, Value: 2}, Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DeviceLimit == nil || *updated.DeviceLimit != 2 || updated.Enabled {
		t.Fatalf("partial update wrong: %+v", updated)
	}
	if updated.TrafficLimitBytes != nil {
		t.Fatal("unspecified field must stay untouched")
	}

	// Null clears to unlimited; absent keeps.
	upDown := 100
	if _, err := svc.Update(ctx, p.ID, Input{SpeedLimitDownKbps: domain.OptInt{Set: true, Value: upDown}}); err != nil {
		t.Fatal(err)
	}
	cleared, err := svc.Update(ctx, p.ID, Input{SpeedLimitDownKbps: domain.OptInt{Set: true, Null: true}})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.SpeedLimitDownKbps != nil {
		t.Fatalf("null must clear the down limit: %+v", cleared.SpeedLimitDownKbps)
	}
	if _, err := svc.Update(ctx, p.ID, Input{Name: ""}); err != nil {
		t.Fatal(err)
	}
}

func i64Ptr(v int64) *int64 { return &v }
