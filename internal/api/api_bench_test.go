package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// benchEnv is the testing.T-free twin of the unit-test env: benchmarks fail
// via b.Fatal on setup only.
type benchEnv struct {
	b        *testing.B
	handler  http.Handler
	users    *user.Service
	ifaces   *iface.Service
	plainTok string
}

func newBenchEnv(b *testing.B) *benchEnv {
	b.Helper()
	db, err := database.Open(filepath.Join(b.TempDir(), "bench.db"), database.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		b.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(b.TempDir(), "master.key"))
	if err != nil {
		b.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		b.Fatal(err)
	}
	tokens := token.NewService(db)
	users := user.NewService(db)
	devices := device.NewService(db, ring)
	rec := webhook.NewRecorder()
	users.Recorder = rec
	devices.Recorder = rec
	acct := accounting.NewService(db, fake.New(), audit.NewService(db), nil, reg)
	acct.Recorder = rec
	_, plaintext, err := tokens.Create(context.Background(), "bench", auth.AllScopes(), nil, "")
	if err != nil {
		b.Fatal(err)
	}
	if err := reg.Set(context.Background(), "node.id", "bench"); err != nil {
		b.Fatal(err)
	}
	// Sustained synthetic traffic would trip the per-token limit; benchmarks
	// measure handler cost, not the limiter (0 = unlimited per the setting).
	if err := reg.Set(context.Background(), "api.rate_limit_per_minute", 0); err != nil {
		b.Fatal(err)
	}
	srv := New(Deps{
		DB: db, Tokens: tokens, Users: users, Devices: devices,
		Plans: plan.NewService(db), Ifaces: iface.NewService(db, reg, ring),
		Settings: reg, Ring: ring, Audit: audit.NewService(db), Accounting: acct,
		Webhooks: webhook.NewService(db, ring), Metrics: metrics.New(),
		NodeID: "bench", ToolsVersion: "bench",
	})
	return &benchEnv{
		b: b, handler: srv.Handler(), users: users,
		ifaces: iface.NewService(db, reg, ring), plainTok: plaintext,
	}
}

func (e *benchEnv) do(method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+e.plainTok)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// seedUsers creates n users through the bulk service (chunks of 500 — the
// bulk cap — each one transaction, name ranges never overlapping).
func (e *benchEnv) seedUsers(n int) {
	e.b.Helper()
	next := 1
	for remaining := n; remaining > 0; {
		chunk := min(remaining, 500)
		if _, err := e.users.CreateBulk(context.Background(), "benchu", chunk, next, 5,
			user.Input{StartPolicy: "immediate"}); err != nil {
			e.b.Fatal(err)
		}
		remaining -= chunk
		next += chunk
	}
}

func benchStatus(b *testing.B, rec *httptest.ResponseRecorder, want int) {
	b.Helper()
	if rec.Code != want {
		b.Fatalf("status %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}

// --- The Phase 4 benchmark set (recorded in docs/development/status.md) ---

// BenchmarkUserList1000 measures one page (20 rows) of keyset-paginated
// listing over 1000 users: SQL keyset seek + JSON marshal.
func BenchmarkUserList1000(b *testing.B) {
	e := newBenchEnv(b)
	e.seedUsers(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchStatus(b, e.do("GET", "/api/v1/users?limit=20", ""), 200)
	}
}

// BenchmarkUserListSearch1000 measures a LIKE search over the same set.
func BenchmarkUserListSearch1000(b *testing.B) {
	e := newBenchEnv(b)
	e.seedUsers(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchStatus(b, e.do("GET", "/api/v1/users?limit=20&search=benchu099", ""), 200)
	}
}

// BenchmarkUserCursorWalk1000 walks the full 1000-user set in pages of 20
// (the complete keyset path incl. cursor encode/decode per page).
func BenchmarkUserCursorWalk1000(b *testing.B) {
	e := newBenchEnv(b)
	e.seedUsers(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		cursor := ""
		b.StartTimer()
		for page := 0; page < 60; page++ {
			p := "/api/v1/users?limit=20"
			if cursor != "" {
				p += "&cursor=" + cursor
			}
			rec := e.do("GET", p, "")
			benchStatus(b, rec, 200)
			var parsed struct {
				NextCursor string `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				b.Fatal(err)
			}
			if cursor = parsed.NextCursor; cursor == "" {
				break
			}
		}
	}
}

// BenchmarkBulkCreate100 measures the bulk endpoint (100 users, one txn,
// fresh prefix per iteration so rows always land).
func BenchmarkBulkCreate100(b *testing.B) {
	e := newBenchEnv(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := fmt.Sprintf(`{"count":100,"prefix":"b%d","start_index":1,"width":4,"duration_seconds":86400}`, i)
		benchStatus(b, e.do("POST", "/api/v1/users/bulk", body), 201)
	}
}

// BenchmarkDeviceConfigRender measures the full client-config path: device
// load, key decrypt (AES-GCM via the ring), settings read, render.
func BenchmarkDeviceConfigRender(b *testing.B) {
	e := newBenchEnv(b)
	// A device needs an interface to attach its peer to (the server side of
	// the rendered config comes from the profile).
	ifc, err := e.ifaces.Create(context.Background(), iface.CreateInput{Name: "awg1", ListenPort: 39011})
	if err != nil {
		b.Fatal(err)
	}
	rec := e.do("POST", "/api/v1/users", fmt.Sprintf(`{"username":"cfguser","interface_id":%q}`, ifc.ID))
	benchStatus(b, rec, 201)
	var u struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		b.Fatal(err)
	}

	rec = e.do("POST", "/api/v1/users/"+u.ID+"/devices", `{"name":"phone"}`)
	benchStatus(b, rec, 201)
	var d struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchStatus(b, e.do("GET", "/api/v1/devices/"+d.ID+"/config", ""), 200)
	}
}
