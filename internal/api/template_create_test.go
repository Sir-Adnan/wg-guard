package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
)

func TestUserCreateAndBulkApplyTechnicalTemplate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	duration := int64(7 * 86400)
	p, err := e.plans.Create(ctx, plan.Input{Name: "7 days / 5 GB", DurationSeconds: &duration,
		StartPolicy:       domain.StartFirstConnection,
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 5_000_000_000},
		DeviceLimit:       domain.OptInt{Set: true, Value: 2}})
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+e.plainTok)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}
	created := send(http.MethodPost, "/api/v1/users", `{"username":"from-template","template_id":"`+p.ID+`",`+
		`"traffic_limit_bytes":1,"duration_seconds":60,"device_limit":1,"start_policy":"immediate"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create template: %d %s", created.Code, created.Body.String())
	}
	u, err := e.users.GetByUsername(ctx, "from-template")
	if err != nil || u.TemplateID == nil || *u.TemplateID != p.ID || u.TrafficLimitBytes == nil ||
		*u.TrafficLimitBytes != 5_000_000_000 || u.DurationSeconds == nil || *u.DurationSeconds != duration ||
		u.DeviceLimit == nil || *u.DeviceLimit != 2 || u.Status != domain.UserWaitingFirstConnection {
		t.Fatalf("template did not override conflicting create terms: %+v %v", u, err)
	}
	bulk := send(http.MethodPost, "/api/v1/users/bulk", `{"count":2,"prefix":"tpl-","template_id":"`+p.ID+`",`+
		`"traffic_limit_bytes":1,"duration_seconds":60}`)
	if bulk.Code != http.StatusCreated {
		t.Fatalf("bulk template: %d %s", bulk.Code, bulk.Body.String())
	}
	var bulkQuota int64
	if err := e.db.QueryRowContext(ctx, `SELECT traffic_limit_bytes FROM users WHERE username = 'tpl-001'`).Scan(&bulkQuota); err != nil || bulkQuota != 5_000_000_000 {
		t.Fatalf("bulk template quota: %d %v", bulkQuota, err)
	}
	if bad := send(http.MethodPost, "/api/v1/users", `{"username":"bad-template","template_id":""}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("empty template id accepted: %d", bad.Code)
	}
	falseValue := false
	disabled, err := e.plans.Create(ctx, plan.Input{Name: "Disabled", Enabled: &falseValue})
	if err != nil {
		t.Fatal(err)
	}
	if denied := send(http.MethodPost, "/api/v1/users", `{"username":"disabled-template","template_id":"`+disabled.ID+`"}`); denied.Code != http.StatusForbidden {
		t.Fatalf("disabled template accepted: %d", denied.Code)
	}
}
