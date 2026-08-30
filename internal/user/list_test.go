package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

func newServiceRecorder(t *testing.T) (*Service, *captureRecorder) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "u.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	rec := &captureRecorder{}
	svc.Recorder = rec
	return svc, rec
}

// newServiceClock provides a service whose clock advances one minute per
// call, so created_at ordering is strictly insertion order (real timestamps
// can collide within julianday's ~µs precision).
func newServiceClock(t *testing.T) *Service {
	svc, _ := newServiceRecorder(t)
	base := time.Now().UTC().Truncate(time.Minute)
	n := 0
	svc.now = func() time.Time { n++; return base.Add(time.Duration(n) * time.Minute) }
	return svc
}

// captureRecorder records events emitted through the service seam.
type captureRecorder struct {
	events []captureEvent
}

type captureEvent struct {
	eventType string
	data      map[string]any
}

func (c *captureRecorder) RecordTx(_ *sql.Tx, eventType string, data map[string]any) error {
	c.events = append(c.events, captureEvent{eventType, data})
	return nil
}

func TestCreateBulkNamesAndSkips(t *testing.T) {
	svc, _ := newServiceRecorder(t)
	ctx := context.Background()
	res, err := svc.CreateBulk(ctx, "gs", 3, 1, 3, Input{DurationSeconds: i64Ptr(86400)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Users) != 3 || res.Skipped != 0 {
		t.Fatalf("bulk: %d created, %d skipped", len(res.Users), res.Skipped)
	}
	for i, u := range res.Users {
		want := []string{"gs001", "gs002", "gs003"}[i]
		if u.Username != want {
			t.Fatalf("username[%d] = %q, want %q", i, u.Username, want)
		}
	}
	// Reserved names (soft-deleted) and already-taken names are skipped, not fatal.
	u := res.Users[0] // gs001
	if err := svc.SoftDelete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	res2, err := svc.CreateBulk(ctx, "gs", 4, 1, 3, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Users) != 1 || res2.Skipped != 3 || res2.Users[0].Username != "gs004" {
		t.Fatalf("skip behavior: %d users %d skipped (%+v)", len(res2.Users), res2.Skipped, res2.Users)
	}
	// Bounds.
	if _, err := svc.CreateBulk(ctx, "gs", 501, 0, 3, Input{}); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("count>500 accepted: %v", err)
	}
}

func TestListPagePaginationAndFilters(t *testing.T) {
	svc := newServiceClock(t)
	ctx := context.Background()
	for _, name := range []string{"anna", "bert", "carl", "dora", "emil"} {
		if _, err := svc.Create(ctx, Input{Username: name}); err != nil {
			t.Fatal(err)
		}
	}
	// Default: newest first (emil…anna).
	page, err := svc.ListPage(ctx, ListQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Username != "emil" || page.Items[1].Username != "dora" || page.NextCursor == "" {
		t.Fatalf("first page wrong: %v", usernames(page.Items))
	}
	// Walk the whole list via cursors; order must be stable and complete.
	seen := []string{"emil", "dora"}
	cursor := page.NextCursor
	for cursor != "" {
		p, err := svc.ListPage(ctx, ListQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range p.Items {
			seen = append(seen, u.Username)
		}
		if len(p.Items) == 0 {
			t.Fatal("cursor returned an empty page without signaling end")
		}
		cursor = p.NextCursor
	}
	if strings.Join(seen, ",") != "emil,dora,carl,bert,anna" {
		t.Fatalf("cursor walk broken: %v", seen)
	}
	// Garbage cursor is INVALID_REQUEST, not a 500.
	if _, err := svc.ListPage(ctx, ListQuery{Cursor: "!!!"}); domain.CodeOf(err) != domain.CodeInvalidRequest {
		t.Fatalf("bad cursor accepted: %v", err)
	}
	// Substring filter (case-insensitive; LIKE wildcards in the term are
	// treated as literals — searching "100_p" finds "x100_percent", and a
	// bare "%" finds nothing because it is escaped).
	if _, err := svc.Create(ctx, Input{Username: "x100_percent"}); err != nil {
		t.Fatal(err)
	}
	pct, err := svc.ListPage(ctx, ListQuery{Filter: ListFilter{Username: strPtr("100_p")}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(pct.Items) != 1 || pct.Items[0].Username != "x100_percent" {
		t.Fatalf("substring filter broken: %v", usernames(pct.Items))
	}
	if none, err := svc.ListPage(ctx, ListQuery{Filter: ListFilter{Username: strPtr("%")}, Limit: 10}); err != nil || len(none.Items) != 0 {
		t.Fatalf("wildcard must be literal: %v %v", usernames(none.Items), err)
	}
	// Status filter.
	if _, err := svc.Create(ctx, Input{Username: "waiting1", StartPolicy: domain.StartFirstConnection}); err != nil {
		t.Fatal(err)
	}
	st := domain.UserWaitingFirstConnection
	w, err := svc.ListPage(ctx, ListQuery{Filter: ListFilter{Status: &st}, Limit: 10})
	if err != nil || len(w.Items) != 1 {
		t.Fatalf("status filter: %v %v", w.Items, err)
	}
}

func TestListPageSorts(t *testing.T) {
	svc := newServiceClock(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Username: "zulu", DurationSeconds: i64Ptr(100)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, Input{Username: "alpha", DurationSeconds: i64Ptr(90000)}); err != nil {
		t.Fatal(err)
	}
	// username ASC.
	p, err := svc.ListPage(ctx, ListQuery{Sort: SortUsername, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(usernames(p.Items), ",") != "alpha,zulu" {
		t.Fatalf("username sort: %v", usernames(p.Items))
	}
	// expires_at ASC with NULLs last (zulu has the shorter duration).
	p, err = svc.ListPage(ctx, ListQuery{Sort: SortExpiresAt, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(usernames(p.Items), ",") != "zulu,alpha" {
		t.Fatalf("expires sort: %v", usernames(p.Items))
	}
}

func TestLifecycleEvents(t *testing.T) {
	svc, rec := newServiceRecorder(t)
	ctx := context.Background()
	u, err := svc.Create(ctx, Input{Username: "frank"})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.has("user.created") {
		t.Fatalf("missing user.created: %v", rec.events)
	}
	if _, err := svc.SetEnabledStatus(ctx, u.ID, false, domain.DisableManual); err != nil {
		t.Fatal(err)
	}
	if !rec.hasData("user.disabled", "reason", "manual") {
		t.Fatalf("missing user.disabled: %v", rec.events)
	}
	if _, err := svc.SetEnabledStatus(ctx, u.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if !rec.has("user.enabled") {
		t.Fatalf("missing user.enabled: %v", rec.events)
	}
	if _, err := svc.Renew(ctx, u.ID, "from_now", i64Ptr(3600), nil); err != nil {
		t.Fatal(err)
	}
	if !rec.has("user.updated") {
		t.Fatalf("missing user.updated: %v", rec.events)
	}
	if err := svc.SoftDelete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if !rec.hasData("user.updated", "deleted", true) {
		t.Fatalf("missing deleted=true update: %v", rec.events)
	}
}

func TestOptUnmarshal(t *testing.T) {
	// The domain Opt types carry the tri-state JSON contract; API DTOs map
	// their snake_case keys onto these fields with explicit tags.
	var o domain.OptInt64
	if err := json.Unmarshal([]byte("null"), &o); err != nil {
		t.Fatal(err)
	}
	if !o.Set || !o.Null {
		t.Fatalf("null opt wrong: %+v", o)
	}
	var v domain.OptInt
	if err := json.Unmarshal([]byte("7"), &v); err != nil {
		t.Fatal(err)
	}
	if !v.Set || v.Null || v.Value != 7 {
		t.Fatalf("value opt wrong: %+v", v)
	}
	var unset domain.OptInt
	if unset.Set {
		t.Fatal("zero Opt must be unset")
	}
}

func usernames(items []*User) []string {
	out := make([]string, 0, len(items))
	for _, u := range items {
		out = append(out, u.Username)
	}
	return out
}

func (c *captureRecorder) has(eventType string) bool {
	for _, e := range c.events {
		if e.eventType == eventType {
			return true
		}
	}
	return false
}

func (c *captureRecorder) hasData(eventType, key string, want any) bool {
	for _, e := range c.events {
		if e.eventType == eventType && e.data[key] == want {
			return true
		}
	}
	return false
}
