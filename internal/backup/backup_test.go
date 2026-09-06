package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

// newService builds a fully wired Service over a temp data dir.
func newService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wg-guard.db")
	keyFile := filepath.Join(dir, "master.key")
	db, err := database.Open(dbPath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.DatabasePath = dbPath
	cfg.MasterKeyFile = keyFile
	cfg.Complete()
	auditSvc := audit.NewService(db)
	return &Service{
		DB: db, Reg: reg, Audit: auditSvc, Cfg: cfg,
		ConfigPath: filepath.Join(dir, "wg-guard.toml"),
		Version:    "test",
		Now:        func() time.Time { return time.Now().UTC() },
	}, dir
}

func writeBootConfig(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "wg-guard.toml")
	if err := os.WriteFile(p, []byte("http_listen = \"127.0.0.1:8080\"\n\n[tls]\nmode = \"dev\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRoundTripPlain(t *testing.T) {
	svc, dir := newService(t)
	writeBootConfig(t, dir)
	ctx := context.Background()

	res, err := svc.Create(ctx, CreateOpts{Reason: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Encrypted {
		t.Fatal("plain archive reported encrypted")
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	if !strings.HasPrefix(res.Name, ArchivePrefix) || !strings.HasSuffix(res.Name, ArchiveExt) {
		t.Fatalf("bad archive name %q", res.Name)
	}

	// Listing shows it, unencrypted, newest-first-ready.
	arcs, err := svc.List()
	if err != nil || len(arcs) != 1 {
		t.Fatalf("list = %v, %v", arcs, err)
	}
	if arcs[0].Encrypted {
		t.Fatal("plain archive sniffed as encrypted")
	}

	// Stage + apply round-trip: apply happens with the DB closed (the
	// service-stopped contract), then the restored copy must reopen clean.
	pr, report, err := svc.Stage(ctx, res.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Encrypted || !report.HasKey {
		t.Fatalf("report flags wrong: encrypted=%v hasKey=%v", report.Encrypted, report.HasKey)
	}
	if report.TLSMode != "dev" || report.Listen != "127.0.0.1:8080" {
		t.Fatalf("config head not parsed: mode=%q listen=%q", report.TLSMode, report.Listen)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("unexpected report warnings: %v", report.Warnings)
	}
	if err := svc.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(pr.PreviewID()); err != nil {
		t.Fatal(err)
	}
	applied, err := svc.ApplyStaged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = pr
	if len(applied.Warnings) == 0 || !strings.Contains(applied.Warnings[0].String(), ".restored") {
		t.Fatalf("expected archived-config note, got %v", applied.Warnings)
	}
	if _, err := os.Stat(filepath.Join(dir, "restore.pending")); !os.IsNotExist(err) {
		t.Fatalf("staging dir not cleaned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "restore.previous", DBMember)); err != nil {
		t.Fatalf("safety snapshot missing: %v", err)
	}
	restored, err := database.Open(filepath.Join(dir, "wg-guard.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var integrity string
	if err := restored.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("restored db integrity = %q, %v", integrity, err)
	}
}

func TestArchiveRoundTripEncrypted(t *testing.T) {
	svc, _ := newService(t)
	writeBootConfig(t, svc.Cfg.DataDir)
	ctx := context.Background()

	res, err := svc.Create(ctx, CreateOpts{Password: "correct horse battery", Reason: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Encrypted {
		t.Fatal("archive not encrypted")
	}
	arcs, err := svc.List()
	if err != nil || len(arcs) != 1 || !arcs[0].Encrypted {
		t.Fatalf("sniff did not detect the age container: %+v, %v", arcs, err)
	}

	// Wrong password rejected; missing password rejected; correct restores.
	if _, _, err := svc.Stage(ctx, res.Path, "wrong-password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, _, err := svc.Stage(ctx, res.Path, ""); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("missing password: %v", err)
	}
	if _, _, err := svc.Stage(ctx, res.Path, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
}

func TestShortPasswordRejected(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.Create(context.Background(), CreateOpts{Password: "short"}); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestCorruptArchiveRejected(t *testing.T) {
	svc, dir := newService(t)
	writeBootConfig(t, dir)
	ctx := context.Background()
	res, err := svc.Create(ctx, CreateOpts{Reason: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Two deterministic corruption classes:
	//  1. a flipped byte in the gzip CRC trailer — the container checksum
	//     must reject it (the drain in readArchive enforces this);
	//  2. truncation — every tar/gzip read must fail loudly.
	// (A single mid-stream byte flip is deliberately NOT asserted: deflate
	// is bit-synchronous and on highly redundant data a flipped extra-bit
	// can decode byte-identically — the output and CRC genuinely match.)
	b[len(b)-2] ^= 0xFF
	corrupt := filepath.Join(dir, "wg-guard-corrupt.wgg")
	if err := os.WriteFile(corrupt, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Stage(ctx, corrupt, ""); err == nil {
		t.Fatal("archive with a bad container checksum accepted")
	}
	if err := os.WriteFile(corrupt, b[:len(b)-64], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Stage(ctx, corrupt, ""); err == nil {
		t.Fatal("truncated archive accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "restore.pending")); !os.IsNotExist(err) {
		t.Fatal("staging dir left behind after failed stage")
	}
}

// handBuiltArchive exercises the refusal paths: a foreign DB member and a
// manifest claiming a newer schema.
func handBuiltArchive(t *testing.T, svc *Service, dbMemberPath string, manifest Manifest) string {
	t.Helper()
	mj, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "wg-guard-handmade.wgg")
	if err := svc.writeArchive(p, "", []member{
		{name: DBMember, path: dbMemberPath},
		{name: ManifestName, data: mj},
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestForeignDatabaseRejected(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	foreign := filepath.Join(t.TempDir(), "foreign.db")
	fdb, err := sql.Open("sqlite", foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fdb.Exec(`CREATE TABLE app (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	fdb.Close()

	arc := handBuiltArchive(t, svc, foreign, Manifest{
		Schema: SchemaVersion, AppVersion: "test",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Hostname:  "elsewhere", Files: map[string]string{DBMember: fileHash(foreign)},
	})
	if _, _, err := svc.Stage(ctx, arc, ""); err == nil ||
		!strings.Contains(err.Error(), "not a WG-Guard database") {
		t.Fatalf("foreign database accepted: %v", err)
	}
}

func TestNewerSchemaRejected(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	res, err := svc.Create(ctx, CreateOpts{Reason: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	// Same members, manifest from the future.
	tmpSnap := filepath.Join(t.TempDir(), "real.db")
	if err := svc.snapshotDB(ctx, tmpSnap); err != nil {
		t.Fatal(err)
	}
	arc := handBuiltArchive(t, svc, tmpSnap, Manifest{
		Schema: SchemaVersion + 1, AppVersion: "future",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Hostname:  "h", Files: map[string]string{DBMember: fileHash(tmpSnap)},
	})
	if _, _, err := svc.Stage(ctx, arc, ""); err == nil ||
		!strings.Contains(err.Error(), "newer") {
		t.Fatalf("future schema accepted: %v", err)
	}
	_ = res
}

func TestRetentionPrune(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		svc.Now = func() time.Time { return time.Now().UTC().Add(time.Duration(i) * time.Minute) }
		if _, err := svc.Create(ctx, CreateOpts{Reason: "manual", Retention: 3}); err != nil {
			t.Fatal(err)
		}
	}
	arcs, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(arcs) != 3 {
		t.Fatalf("retention kept %d archives, want 3", len(arcs))
	}
	if !arcs[0].ModTime.After(arcs[1].ModTime) {
		t.Fatal("listing not sorted newest first")
	}
}

func TestDeleteAndOpenNameValidation(t *testing.T) {
	svc, _ := newService(t)
	if err := svc.Delete(context.Background(), "../escape.wgg"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := svc.Delete(context.Background(), "not-an-archive.conf"); err == nil {
		t.Fatal("foreign name accepted")
	}
	if err := svc.Delete(context.Background(), "wg-guard-00000000-000000.wgg"); err == nil {
		t.Fatal("missing archive delete should fail")
	}
}

// routingClient rewrites api.telegram.org onto the test server.
type routingClient struct {
	base  string
	inner HTTPDoer
}

func (c routingClient) Do(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.String(), telegramAPIBase) {
		u, err := url.Parse(c.base + strings.TrimPrefix(req.URL.String(), telegramAPIBase))
		if err != nil {
			return nil, err
		}
		req.URL = u
	}
	return c.inner.Do(req)
}

func TestTelegramSinkShape(t *testing.T) {
	svc, dir := newService(t)
	ctx := context.Background()

	var gotChat, gotCT, gotName string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The real route is /bot<token>/sendDocument; the failure-path
		// client routes under /bad/... and must fall through to 404.
		if !strings.HasPrefix(r.URL.Path, "/bot") {
			http.NotFound(w, r)
			return
		}
		gotCT = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return
		}
		gotChat = r.FormValue("chat_id")
		file, hdr, err := r.FormFile("document")
		if err != nil {
			t.Errorf("document field: %v", err)
			return
		}
		file.Close()
		gotName = hdr.Filename
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}))
	defer ts.Close()

	if err := svc.Reg.Set(ctx, "backup.telegram_chat", "123456789"); err != nil {
		t.Fatal(err)
	}

	tg := &TelegramSink{
		Token: "12345:ABC-token", Chat: "123456789",
		HTTP: routingClient{base: ts.URL, inner: ts.Client()},
	}
	probe := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(probe, []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tg.Deliver(ctx, probe, "wg-guard-20260101-000000.wgg"); err != nil {
		t.Fatal(err)
	}
	if gotChat != "123456789" {
		t.Fatalf("chat_id = %q", gotChat)
	}
	if gotName != "wg-guard-20260101-000000.wgg" {
		t.Fatalf("filename = %q", gotName)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data; boundary=") {
		t.Fatalf("content type = %q", gotCT)
	}

	// Failure path surfaces the API description, never the token.
	bad := &TelegramSink{
		Token: "12345:ABC-token", Chat: "123456789",
		HTTP: routingClient{base: ts.URL + "/bad", inner: ts.Client()},
	}
	err := bad.TestDelivery(ctx)
	if err == nil || strings.Contains(err.Error(), "ABC-token") {
		t.Fatalf("failure must not leak the token: %v", err)
	}
}

func TestScheduleNextRunAndDue(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	base := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC) // a Monday
	svc.Now = func() time.Time { return base }

	daily := &Schedule{Name: "nightly", Kind: KindDaily, TimeOfDay: "03:00", Enabled: true}
	created, err := svc.CreateSchedule(ctx, daily)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC); !created.NextRunAt.Equal(want) {
		t.Fatalf("daily next = %v, want %v", created.NextRunAt, want)
	}

	weekly := &Schedule{Name: "sunday", Kind: KindWeekly, TimeOfDay: "22:30", Weekday: 0, Enabled: true}
	createdW, err := svc.CreateSchedule(ctx, weekly)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 6, 22, 30, 0, 0, time.UTC); !createdW.NextRunAt.Equal(want) {
		t.Fatalf("weekly next = %v, want %v", createdW.NextRunAt, want)
	}

	interval := &Schedule{Name: "4h", Kind: KindInterval, IntervalHours: 4, Enabled: true}
	createdI, err := svc.CreateSchedule(ctx, interval)
	if err != nil {
		t.Fatal(err)
	}
	if want := base.Add(4 * time.Hour); !createdI.NextRunAt.Equal(want) {
		t.Fatalf("interval next = %v, want %v", createdI.NextRunAt, want)
	}

	// Force the daily schedule due and run it once.
	if _, err := svc.DB.ExecContext(ctx,
		`UPDATE backup_schedules SET next_run_at=? WHERE id=?`,
		formatTime(base.Add(-time.Minute)), created.ID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.RunDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("RunDue fired %d schedules, want 1", n)
	}
	arcs, _ := svc.List()
	if len(arcs) != 1 {
		t.Fatalf("scheduled run produced %d archives", len(arcs))
	}
	got, err := svc.GetSchedule(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastStatus != "ok" || got.LastRunAt == nil || !got.NextRunAt.After(base) {
		t.Fatalf("schedule not advanced: %+v", got)
	}
	// The missed window does not re-fire.
	if n, err := svc.RunDue(ctx); err != nil || n != 0 {
		t.Fatalf("re-fire: ran %d, %v", n, err)
	}
}

func TestScheduleValidationMatrix(t *testing.T) {
	cases := []struct {
		name string
		sc   Schedule
		ok   bool
	}{
		{"daily ok", Schedule{Name: "n", Kind: KindDaily, TimeOfDay: "03:00"}, true},
		{"daily bad time", Schedule{Name: "n", Kind: KindDaily, TimeOfDay: "24:00"}, false},
		{"weekly ok", Schedule{Name: "n", Kind: KindWeekly, TimeOfDay: "08:15", Weekday: 6}, true},
		{"weekly bad weekday", Schedule{Name: "n", Kind: KindWeekly, TimeOfDay: "08:15", Weekday: 7}, false},
		{"interval ok", Schedule{Name: "n", Kind: KindInterval, IntervalHours: 12}, true},
		{"interval zero", Schedule{Name: "n", Kind: KindInterval, IntervalHours: 0}, false},
		{"bad kind", Schedule{Name: "n", Kind: "monthly"}, false},
		{"empty name", Schedule{Name: "", Kind: KindDaily, TimeOfDay: "03:00"}, false},
	}
	for _, c := range cases {
		if err := c.sc.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: validate = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestPendingLifecycle(t *testing.T) {
	svc, _ := newService(t)
	writeBootConfig(t, svc.Cfg.DataDir)
	ctx := context.Background()

	pr, err := svc.Pending()
	if err != nil || pr != nil {
		t.Fatalf("no pending expected, got %v, %v", pr, err)
	}
	res, err := svc.Create(ctx, CreateOpts{Reason: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	staged, _, err := svc.Stage(ctx, res.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(staged.PreviewID()); err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Pending()
	if err != nil || pending == nil || pending.Archive != staged.Archive {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	var meta struct {
		Manifest Manifest `json:"manifest"`
	}
	raw, _ := os.ReadFile(filepath.Join(pending.Dir, pendingMeta))
	_ = json.Unmarshal(raw, &meta)
	if meta.Manifest.Schema != SchemaVersion {
		t.Fatalf("staged manifest schema = %d", meta.Manifest.Schema)
	}
	if err := svc.DiscardPending(); err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.Pending(); p != nil {
		t.Fatal("discard left a pending restore")
	}
}

func ctxBG() context.Context { return context.Background() }
