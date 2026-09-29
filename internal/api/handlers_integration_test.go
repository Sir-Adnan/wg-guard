package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/reconcile"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
)

type failingAccessReconciler struct{}

func (failingAccessReconciler) Run(context.Context) (*reconcile.Report, error) {
	return nil, errors.New("synthetic reconcile failure")
}

func TestPurchaseResultAndResellerIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ifc, err := e.ifaces.Create(ctx, iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	duration := int64(86400)
	p, err := e.plans.Create(ctx, plan.Input{Name: "Basic", InterfaceID: domain.OptString{Set: true, Value: ifc.ID},
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 1000000}, DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	send := func(token, method, path, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}
	path := "/api/v1/purchases"
	body := `{"plan_id":"` + p.ID + `","username":"owner-customer"}`
	if rec := send(e.plainTok, http.MethodPost, path, "", body); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing operation key: %d", rec.Code)
	}
	first := send(e.plainTok, http.MethodPost, path, "order-1", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("owner purchase: %d %s", first.Code, first.Body.String())
	}
	firstBody := decodeBody(t, first)
	if firstBody["user_id"] == nil || firstBody["device_id"] == nil {
		t.Fatalf("purchase omitted identifiers: %v", firstBody)
	}
	ownerLink := send(e.plainTok, http.MethodGet, "/api/v1/users/"+firstBody["user_id"].(string)+"/subscription", "", "")
	if ownerLink.Code != http.StatusOK || ownerLink.Header().Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(decodeBody(t, ownerLink)["path"].(string), "/sub/") {
		t.Fatalf("owner customer link delivery: %d", ownerLink.Code)
	}
	oldLinkPath := decodeBody(t, ownerLink)["path"].(string)
	oldDevice, err := e.devices.Get(ctx, firstBody["device_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	rotated := send(e.plainTok, http.MethodPost,
		"/api/v1/users/"+firstBody["user_id"].(string)+"/subscription/rotate", "", "")
	if rotated.Code != http.StatusOK || rotated.Header().Get("Cache-Control") != "no-store" ||
		decodeBody(t, rotated)["path"] == oldLinkPath {
		t.Fatalf("customer access rotation: %d", rotated.Code)
	}
	newDevice, err := e.devices.Get(ctx, firstBody["device_id"].(string))
	if err != nil || newDevice.PublicKey == oldDevice.PublicKey {
		t.Fatalf("device credential survived rotation: %v", err)
	}
	if _, err := e.srv.Links.Resolve(ctx, strings.TrimPrefix(oldLinkPath, "/sub/")); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("old link survived rotation: %v", err)
	}
	committedPath := decodeBody(t, rotated)["path"].(string)
	e.srv.Reconciler = failingAccessReconciler{}
	failedRuntime := send(e.plainTok, http.MethodPost,
		"/api/v1/users/"+firstBody["user_id"].(string)+"/subscription/rotate", "", "")
	if failedRuntime.Code != http.StatusServiceUnavailable || errCode(t, failedRuntime) != domain.CodeNodeUnavailable {
		t.Fatalf("reconcile failure response: %d", failedRuntime.Code)
	}
	e.srv.Reconciler = nil
	if _, err := e.srv.Links.Resolve(ctx, strings.TrimPrefix(committedPath, "/sub/")); domain.CodeOf(err) != domain.CodeUserNotFound {
		t.Fatalf("failed runtime revived previous link: %v", err)
	}
	currentLink := send(e.plainTok, http.MethodGet, "/api/v1/users/"+firstBody["user_id"].(string)+"/subscription", "", "")
	if currentLink.Code != http.StatusOK || decodeBody(t, currentLink)["path"] == committedPath {
		t.Fatalf("committed rotation was rolled back: %d", currentLink.Code)
	}
	if rec := send("", http.MethodPost, path, "order-1", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous replay: %d", rec.Code)
	}
	replay := send(e.plainTok, http.MethodPost, path, "order-1", body)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" ||
		decodeBody(t, replay)["operation_id"] != firstBody["operation_id"] {
		t.Fatalf("replay result: %d", replay.Code)
	}
	if rec := send(e.plainTok, http.MethodGet, "/api/v1/operations/result", "order-1", ""); rec.Code != http.StatusOK || decodeBody(t, rec)["user_id"] != firstBody["user_id"] {
		t.Fatalf("lookup result: %d", rec.Code)
	}
	if rec := send(e.plainTok, http.MethodPost, path, "order-1", `{"plan_id":"`+p.ID+`","username":"other"}`); rec.Code != http.StatusConflict || errCode(t, rec) != domain.CodeIdempotencyKeyReused {
		t.Fatalf("key payload conflict: %d", rec.Code)
	}
	r, err := reseller.NewService(e.db).Create(ctx, "north", "North", []string{"purchases.create", "operations.read", "users.read", "subscriptions.read", "subscriptions.rotate", "traffic.update"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.ExecContext(ctx, `INSERT INTO admins
		(id, username, password_hash, role, created_at, updated_at)
		VALUES ('owner-1', 'owner', 'x', 'owner', 'test', 'test')`); err != nil {
		t.Fatal(err)
	}
	resellerToken, err := func() (string, error) {
		_, secret, err := e.tokens.CreateForAdmin(ctx, "owner-1", &r.ID, "north bot",
			[]string{"purchases.create", "operations.read", "users.read", "subscriptions.read", "subscriptions.rotate", "traffic.update"}, nil, "")
		return secret, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	resellerBody := `{"plan_id":"` + p.ID + `"}`
	if rec := send(resellerToken, http.MethodGet, "/api/v1/operations/result", "order-1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("owner result exposed to reseller: %d", rec.Code)
	}
	if rec := send(resellerToken, http.MethodGet, "/api/v1/users/"+firstBody["user_id"].(string)+"/subscription", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("owner customer link exposed to reseller: %d", rec.Code)
	}
	if rec := send(resellerToken, http.MethodPost, "/api/v1/users/"+firstBody["user_id"].(string)+"/subscription/rotate", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("owner customer access rotated by reseller: %d", rec.Code)
	}
	if rec := send(resellerToken, http.MethodPost, "/api/v1/users/"+firstBody["user_id"].(string)+"/traffic/reset", "reset-foreign", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("owner usage reset by reseller: %d", rec.Code)
	}
	if rec := send(resellerToken, http.MethodPost, path, "order-1", resellerBody); rec.Code != http.StatusForbidden {
		t.Fatalf("unassigned plan: %d", rec.Code)
	}
	if err := reseller.NewService(e.db).SetPlans(ctx, r.ID, []string{p.ID}); err != nil {
		t.Fatal(err)
	}
	owned := send(resellerToken, http.MethodPost, path, "order-1", resellerBody)
	if owned.Code != http.StatusCreated || decodeBody(t, owned)["user_id"] == firstBody["user_id"] {
		t.Fatalf("reseller purchase: %d %s", owned.Code, owned.Body.String())
	}
	if rec := send(resellerToken, http.MethodGet, "/api/v1/users/"+decodeBody(t, owned)["user_id"].(string)+"/subscription", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("owned customer link: %d", rec.Code)
	}
	ownedID := decodeBody(t, owned)["user_id"].(string)
	if rec := send(resellerToken, http.MethodPost, "/api/v1/users/"+ownedID+"/traffic/reset", "reset-owned", ""); rec.Code != http.StatusOK {
		t.Fatalf("owned usage reset: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(resellerToken, http.MethodPost, "/api/v1/users/"+ownedID+"/traffic/reset", "reset-owned", ""); rec.Code != http.StatusOK || rec.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("owned usage reset replay: %d", rec.Code)
	}
	if rec := send(resellerToken, http.MethodGet, "/api/v1/operations/result", "owner-only", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign result exposed: %d", rec.Code)
	}
	if rec := send(resellerToken, http.MethodGet, "/api/v1/operations/result", "order-1", ""); rec.Code != http.StatusOK || decodeBody(t, rec)["user_id"] != decodeBody(t, owned)["user_id"] {
		t.Fatalf("own result lookup: %d", rec.Code)
	}
}

func TestNextPlanRoutesAreScopedAndRecoverable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ifc, err := e.ifaces.Create(ctx, iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	duration := int64(86400)
	current, err := e.plans.Create(ctx, plan.Input{Name: "Current", InterfaceID: domain.OptString{Set: true, Value: ifc.ID},
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 1000000}, DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.plans.Create(ctx, plan.Input{Name: "Next", InterfaceID: domain.OptString{Set: true, Value: ifc.ID},
		TrafficLimitBytes: domain.OptInt64{Set: true, Value: 2000000}, DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	send := func(token, method, path, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		e.handler.ServeHTTP(w, req)
		return w
	}
	created := send(e.plainTok, http.MethodPost, "/api/v1/purchases", "next-order", `{"plan_id":"`+current.ID+`"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("purchase: %d %s", created.Code, created.Body.String())
	}
	uid := decodeBody(t, created)["user_id"].(string)
	path := "/api/v1/users/" + uid + "/next-plan"
	if rec := send(e.plainTok, http.MethodGet, path, "", ""); rec.Code != http.StatusOK || decodeBody(t, rec)["next_plan"] != nil {
		t.Fatalf("empty queue: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(e.plainTok, http.MethodGet, path+"/activations", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("empty activation history: %d", rec.Code)
	}
	queuedBody := `{"plan_id":"` + next.ID + `"}`
	if rec := send(e.plainTok, http.MethodPut, path, "queue-1", queuedBody); rec.Code != http.StatusOK || decodeBody(t, rec)["state"] != "queued" {
		t.Fatalf("queue: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(e.plainTok, http.MethodPut, path, "queue-1", queuedBody); rec.Code != http.StatusOK || rec.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("queue replay: %d", rec.Code)
	}
	r, err := reseller.NewService(e.db).Create(ctx, "north-next", "North", []string{"next_plans.read", "next_plans.write"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.ExecContext(ctx, `INSERT INTO admins
		(id, username, password_hash, role, created_at, updated_at)
		VALUES ('owner-next', 'owner-next', 'x', 'owner', 'test', 'test')`); err != nil {
		t.Fatal(err)
	}
	_, tokenSecret, err := e.tokens.CreateForAdmin(ctx, "owner-next", &r.ID, "next bot", []string{"next_plans.read", "next_plans.write"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		body := ""
		if method == http.MethodPut {
			body = queuedBody
		}
		if rec := send(tokenSecret, method, path, "", body); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign next-plan %s: %d", method, rec.Code)
		}
	}
	if rec := send(tokenSecret, http.MethodGet, path+"/activations", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign activation history: %d", rec.Code)
	}
	if rec := send(e.plainTok, http.MethodDelete, path, "cancel-1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d", rec.Code)
	}
	if rec := send(e.plainTok, http.MethodGet, path, "", ""); rec.Code != http.StatusOK || decodeBody(t, rec)["next_plan"] != nil {
		t.Fatalf("queue survived cancel: %d", rec.Code)
	}
}
