package boot

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

// fakeRunner answers every command through a hook and records the calls, so
// boot tests can script sysctl/nft/ufw without any host state.
type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	respond func(argv []string) subprocess.Result
	errs    map[string]error // keyed on argv[0]+argv[1]
}

func (f *fakeRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), argv...))
	if err, ok := f.errs[strings.Join(argv[:2], " ")]; ok {
		return subprocess.Result{}, err
	}
	if f.respond != nil {
		return f.respond(argv), nil
	}
	return subprocess.Result{}, nil
}

func (f *fakeRunner) joined() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var sb strings.Builder
	for _, c := range f.calls {
		sb.WriteString(strings.Join(c, " "))
		sb.WriteString("\n")
	}
	return sb.String()
}

type deps struct {
	db      *database.DB
	ring    *secrets.KeyRing
	runner  *fakeRunner
	backend *fake.Backend
}

func newDeps(t *testing.T) *deps {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), slog.Default()); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	_ = reg
	f := &fakeRunner{
		errs: map[string]error{
			// nft probe: table absent (ExitError → absent, not broken).
			"nft list": &subprocess.ExitError{Name: "nft", ExitCode: 1, Stderr: "Error: No such file or directory"},
			// firewalld not installed (ufw also absent: no responder answer
			// would make it look installed, so it must exit non-zero).
			"firewall-cmd --state": &subprocess.ExitError{Name: "firewall-cmd", ExitCode: 1, Stderr: "not running"},
		},
		respond: func(argv []string) subprocess.Result {
			// sysctl read: forwarding off → the boot must switch it on.
			if strings.Join(argv, " ") == "sysctl -n net.ipv4.ip_forward" {
				return subprocess.Result{Stdout: []byte("0\n")}
			}
			return subprocess.Result{}
		},
	}
	return &deps{db: db, ring: ring, runner: f, backend: fake.New()}
}

// seedInterface inserts one enabled plain profile with a real encrypted key.
func (d *deps) seedInterface(t *testing.T, name, subnet string, port int) {
	t.Helper()
	privEnc, err := d.ring.Encrypt([]byte(testPriv))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = d.db.Exec(`INSERT INTO tunnel_interfaces
		(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted,
		 preset_name, enabled, backend_mode, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 'kernel', ?, ?)`,
		"id-"+name, name, port, subnet, 1420, testPub, privEnc, "plain", now, now)
	if err != nil {
		t.Fatal(err)
	}
}

const (
	testPriv = "6BqSrxh0chPmL0pD5t62Lh7cSQtU4hS37xjKORTLLkc="
	testPub  = "eR0MzG2HjgbJHmBEnpjVACJ0mmC4WdLwYfUY3ZiT7Uw="
)

func TestBringUpFreshNode(t *testing.T) {
	ctx := context.Background()
	d := newDeps(t)
	d.seedInterface(t, "awg0", "10.8.0.0/24", 40001)

	var auditSvc *audit.Service = audit.NewService(d.db)
	res, err := BringUp(ctx, Deps{
		DB: d.db, Ring: d.ring, Backend: d.backend, Run: d.runner,
		Settings: mustRegistry(t, d), Audit: auditSvc,
	})
	if err != nil {
		t.Fatalf("bring up: %v", err)
	}

	if !res.ForwardingChanged {
		t.Fatal("forwarding should have been switched on")
	}
	if res.Reconcile == nil || res.Reconcile.InterfacesCreated != 1 {
		t.Fatalf("reconcile = %+v", res.Reconcile)
	}
	if res.ManagedIfaces != 1 {
		t.Fatalf("managed = %d", res.ManagedIfaces)
	}
	// The engine must hand the gateway address to the backend.
	ops := d.backend.Ops()
	if len(ops) == 0 || ops[0].Kind != "create" {
		t.Fatalf("ops = %+v", ops)
	}
	// Firewall: rendered from the enabled interface.
	if !strings.Contains(d.runner.joined(), "nft -f") {
		t.Fatalf("nft apply missing:\n%s", d.runner.joined())
	}
	// No ufw installed → no route rules, no findings.
	if len(res.UfwRoutes) != 0 || len(res.Findings) != 0 {
		t.Fatalf("routes=%v findings=%v", res.UfwRoutes, res.Findings)
	}
	// Audit entry recorded.
	rec, err := auditSvc.Recent(ctx, 10)
	if err != nil || len(rec) != 1 || rec[0].Action != "node.reconcile" {
		t.Fatalf("audit records = %+v err=%v", rec, err)
	}
}

func TestBringUpAppliesShaper(t *testing.T) {
	ctx := context.Background()
	d := newDeps(t)
	d.seedInterface(t, "awg0", "10.8.0.0/24", 40001)

	// A user with a speed limit and one device.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := d.db.Exec(`INSERT INTO users (id, username, status, speed_limit_kbps, start_policy, enabled, created_at, updated_at)
		VALUES ('u1', 'limited', 'active', 2048, 'immediate', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.Exec(`INSERT INTO devices (id, user_id, interface_id, name, ipv4_address, public_key, private_key_encrypted, enabled, created_at, updated_at)
		VALUES ('d1', 'u1', 'id-awg0', 'phone', '10.8.0.2/32', 'devpub', x'00', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	sh := shaper.New(d.runner)
	res, err := BringUp(ctx, Deps{
		DB: d.db, Ring: d.ring, Backend: d.backend, Run: d.runner,
		Settings: mustRegistry(t, d), Shaper: sh,
	})
	if err != nil {
		t.Fatalf("bring up: %v", err)
	}
	if res.ShapedGroups != 1 {
		t.Fatalf("shaped groups = %d", res.ShapedGroups)
	}
	if !strings.Contains(d.runner.joined(), "tc -b") {
		t.Fatalf("tc batch missing:\n%s", d.runner.joined())
	}

	// A failing tc surfaces as a finding and never aborts bring-up.
	d2 := newDeps(t)
	d2.seedInterface(t, "awg0", "10.8.0.0/24", 40001)
	if _, err := d2.db.Exec(`INSERT INTO users (id, username, status, speed_limit_kbps, start_policy, enabled, created_at, updated_at)
		VALUES ('u1', 'limited', 'active', 2048, 'immediate', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.db.Exec(`INSERT INTO devices (id, user_id, interface_id, name, ipv4_address, public_key, private_key_encrypted, enabled, created_at, updated_at)
		VALUES ('d1', 'u1', 'id-awg0', 'phone', '10.8.0.2/32', 'devpub', x'00', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	d2.runner.errs["tc -b"] = &subprocess.ExitError{Name: "tc", ExitCode: 1, Stderr: "cannot find device"}
	res2, err := BringUp(ctx, Deps{
		DB: d2.db, Ring: d2.ring, Backend: d2.backend, Run: d2.runner,
		Settings: mustRegistry(t, d2), Shaper: shaper.New(d2.runner),
	})
	if err != nil {
		t.Fatalf("shaper failure must not abort bring-up: %v", err)
	}
	found := false
	for _, f := range res2.Findings {
		if f.Tool == "tc" && f.Blocking {
			found = true
		}
	}
	if !found {
		t.Fatalf("tc finding missing: %+v", res2.Findings)
	}
}

func TestBringUpUfwActive(t *testing.T) {
	ctx := context.Background()
	d := newDeps(t)
	d.seedInterface(t, "awg0", "10.8.0.0/24", 40001)
	d.runner.respond = func(argv []string) subprocess.Result {
		joined := strings.Join(argv, " ")
		switch {
		case joined == "sysctl -n net.ipv4.ip_forward":
			return subprocess.Result{Stdout: []byte("1\n")}
		case joined == "ufw status verbose":
			return subprocess.Result{Stdout: []byte("Status: active\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n")}
		case strings.HasPrefix(joined, "ufw route allow"):
			return subprocess.Result{Stdout: []byte("Rule added\n")}
		}
		return subprocess.Result{}
	}

	res, err := BringUp(ctx, Deps{
		DB: d.db, Ring: d.ring, Backend: d.backend, Run: d.runner,
		Settings: mustRegistry(t, d),
	})
	if err != nil {
		t.Fatalf("bring up: %v", err)
	}
	if len(res.UfwRoutes) != 1 || res.UfwRoutes[0] != "awg0" {
		t.Fatalf("routes = %v", res.UfwRoutes)
	}
	if len(res.Findings) != 1 || res.Findings[0].Tool != "ufw" || res.Findings[0].Blocking {
		t.Fatalf("findings = %+v", res.Findings)
	}
}

// A per-interface reconcile failure is collected and bring-up continues:
// the firewall is still applied for the healthy interfaces, and the errors
// travel in the result for the caller to surface (non-zero CLI exit).
func TestBringUpContinuesOnReconcileFailure(t *testing.T) {
	ctx := context.Background()
	d := newDeps(t)
	d.seedInterface(t, "awg0", "10.8.0.0/24", 40001)
	d.backend.FailOn[fake.OpCreate] = errBoom{}

	res, err := BringUp(ctx, Deps{
		DB: d.db, Ring: d.ring, Backend: d.backend, Run: d.runner,
		Settings: mustRegistry(t, d),
	})
	if err != nil {
		t.Fatalf("bring up must not abort: %v", err)
	}
	if len(res.Reconcile.Errors) != 1 {
		t.Fatalf("errors = %+v", res.Reconcile.Errors)
	}
	// The firewall was still applied for the (zero) reconciled interfaces.
	if !strings.Contains(d.runner.joined(), "nft -f") {
		t.Fatalf("firewall must still be applied:\n%s", d.runner.joined())
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func mustRegistry(t *testing.T, d *deps) *settings.Registry {
	t.Helper()
	reg, err := settings.New(d.db, d.ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	return reg
}
