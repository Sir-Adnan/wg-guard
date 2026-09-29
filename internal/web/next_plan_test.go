package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
)

func TestNextPlanOwnerPanelFlow(t *testing.T) {
	e := newEnv(t)
	uid, _, csrf, cookie := e.seedUserWithDevice()
	ctx := context.Background()
	if _, err := e.db.ExecContext(ctx, `UPDATE users SET traffic_limit_bytes = 1000000 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	var ifaceID string
	if err := e.db.QueryRowContext(ctx, `SELECT id FROM tunnel_interfaces LIMIT 1`).Scan(&ifaceID); err != nil {
		t.Fatal(err)
	}
	duration := int64(86400)
	p, err := e.srv.Plans.Create(ctx, plan.Input{Name: "Successor", InterfaceID: domain.OptString{Set: true, Value: ifaceID},
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 2000000}, DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	page := e.get("/users/"+uid, cookie)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `id="next-plan-id"`) {
		t.Fatalf("next-plan form unavailable: %d", page.Code)
	}
	queued := e.post("/users/"+uid+"/next-plan", url.Values{"plan_id": {p.ID}}, cookie, csrf)
	if queued.Code != http.StatusSeeOther {
		t.Fatalf("queue action: %d %s", queued.Code, queued.Body.String())
	}
	page = e.get("/users/"+uid, cookie)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Successor") {
		t.Fatalf("queued plan not visible: %d", page.Code)
	}
	canceled := e.post("/users/"+uid+"/next-plan/cancel", url.Values{}, cookie, csrf)
	if canceled.Code != http.StatusSeeOther {
		t.Fatalf("cancel action: %d", canceled.Code)
	}
	current, err := e.srv.Integration.NextPlanForUser(ctx, uid, nil)
	if err != nil || current != nil {
		t.Fatalf("queue survived cancel: %+v %v", current, err)
	}
}
