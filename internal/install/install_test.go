package install

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

// TestMain shrinks the health-check windows so rollback tests fail fast.
func TestMain(m *testing.M) {
	installHealthWindow = 3 * time.Second
	updateHealthWindow = 3 * time.Second
	os.Exit(m.Run())
}

func TestResolveDerivesACMEFromDomain(t *testing.T) {
	p := Defaults()
	p.Domain = "Panel.Example.COM" // case-normalized
	res, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if res.TLSMode != config.TLSModeACME {
		t.Fatalf("tls mode = %s, want acme", res.TLSMode)
	}
	if res.Domain != "panel.example.com" {
		t.Fatalf("domain = %s", res.Domain)
	}
	if res.PanelPort != 443 {
		t.Fatalf("panel port = %d, want 443", res.PanelPort)
	}
	if res.ACMEHTTPPort != 80 {
		t.Fatalf("acme port = %d, want 80", res.ACMEHTTPPort)
	}
	if res.HTTPListen() != "0.0.0.0:443" {
		t.Fatalf("listen = %s, want 0.0.0.0:443", res.HTTPListen())
	}
}

func TestResolveValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Plan)
		wantErr string
	}{
		{"acme needs domain", func(p *Plan) {
			p.TLSMode = config.TLSModeACME
			p.Domain = ""
		}, "domain"},
		{"manual needs cert", func(p *Plan) {
			p.TLSMode = config.TLSModeManual
		}, "cert-file"},
		{"domain with port", func(p *Plan) {
			p.Domain = "x.example.com:443"
		}, "bare hostname"},
		{"bad mode", func(p *Plan) {
			p.Mode = "kubernetes"
		}, "mode"},
		{"bad panel port", func(p *Plan) {
			p.PanelPort = 0
		}, "panel port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Defaults()
			tc.mutate(&p)
			if p.Domain != "" {
				// Force acme off where the case sets a domain-derived mode.
				if p.TLSMode == config.TLSModeACME && p.Domain == "" {
					p.TLSMode = config.TLSModeProxy
				}
			}
			_, err := p.Resolve()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestResolveProxyStaysLoopback(t *testing.T) {
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	res, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if res.HTTPListen() != "127.0.0.1:8080" {
		t.Fatalf("listen = %s, want loopback 8080", res.HTTPListen())
	}
	if got := res.PanelURL(); got != "http://<server-ip>:8080" {
		t.Fatalf("url = %s", got)
	}
}

func TestRenderComposeShape(t *testing.T) {
	p := Defaults()
	p.Domain = "panel.example.com"
	p.PanelPort = 443
	res, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	c := RenderCompose(res)
	for _, want := range []string{
		"network_mode: host",
		"- NET_ADMIN",
		"- /etc/wg-guard/wg-guard.toml:/etc/wg-guard/wg-guard.toml:ro",
		"- /var/lib/wg-guard:/var/lib/wg-guard",
		"restart: unless-stopped",
		"image: " + DefaultImage,
		// acme mode probes the plain-HTTP sidecar, not the TLS listener
		"http://127.0.0.1:80/healthz",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("compose missing %q\n%s", want, c)
		}
	}
}

func TestRenderUnitHardening(t *testing.T) {
	p := Defaults()
	p.TLSMode = config.TLSModeManual
	p.CertFile, p.KeyFile = "/x/c.pem", "/x/k.pem"
	res, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	u := RenderUnit(res)
	for _, want := range []string{
		"ExecStart=/usr/local/bin/wg-guard serve -config /etc/wg-guard/wg-guard.toml",
		"CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
		"ReadWritePaths=/var/lib/wg-guard /proc/sys/net/ipv4/ip_forward",
		"MemoryDenyWriteExecute=true",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q\n%s", want, u)
		}
	}
}

func TestRoute(t *testing.T) {
	cases := map[string]string{
		"serve":     "refuse",
		"install":   "host",
		"update":    "host",
		"uninstall": "host",
		"status":    "host",
		"doctor":    "host",
		"backup":    "container",
		"restore":   "container",
		"settings":  "container",
		"token":     "container",
		"secrets":   "container",
		"reconcile": "container",
	}
	for cmd, want := range cases {
		if got := Route(cmd); got != want {
			t.Errorf("Route(%q) = %q, want %q", cmd, got, want)
		}
	}
}

// healthServer serves /healthz on a real loopback port so the install/update
// flows can pass their final health check.
func healthServer(t *testing.T, status int) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return ln.Addr().(*net.TCPAddr).Port
}

// TestInstallDockerHappyPath runs a full docker-mode install against the
// in-memory host with a real health endpoint: files land with the right
// permissions, the container is started, the state file is written last.
func TestInstallDockerHappyPath(t *testing.T) {
	h := newMemHost()
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.Mode = ModeDocker
	p.TLSMode = config.TLSModeProxy // plain-HTTP probe on the health port
	p.PanelPort = port

	st, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdin: strings.NewReader(""),
		Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if st.Mode != ModeDocker || st.ComposePath != ComposePth {
		t.Fatalf("state = %+v", st)
	}

	cfg := h.files[p.BootConfigPath()]
	if cfg.perm != 0o600 {
		t.Errorf("config perm = %o, want 600", cfg.perm)
	}
	if !strings.Contains(string(cfg.data), `mode = "proxy"`) {
		t.Errorf("config missing tls mode:\n%s", cfg.data)
	}
	if !strings.Contains(string(cfg.data), `http_listen = "127.0.0.1:`+fmt.Sprint(port)+`"`) {
		t.Errorf("config missing listen:\n%s", cfg.data)
	}
	if got := h.files[StatePath].perm; got != 0o644 {
		t.Errorf("state perm = %o, want 644", got)
	}
	for _, want := range [][]string{
		{"docker", "compose", "version"},
		{"docker", "compose", "-f", ComposePth, "up", "-d"},
	} {
		if !h.ran(want...) {
			t.Errorf("command not run: %v (ran: %v)", want, h.ranCommands())
		}
	}
	// Data dir created, state records the shim path.
	if !h.dirs[DataDir] {
		t.Error("data dir not created")
	}
}

// TestInstallNativeHappyPath checks the binary copy + unit + enable/start.
func TestInstallNativeHappyPath(t *testing.T) {
	h := newMemHost()
	// The running binary (SelfExe) must exist for the copy.
	h.files["/src/wg-guard"] = memFile{data: []byte("/src/wg-guard"), perm: 0o755}
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.Mode = ModeNative
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port

	st, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdin: strings.NewReader(""),
		Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if st.BinPath != BinPath || st.UnitPath != UnitPath {
		t.Fatalf("state = %+v", st)
	}
	if _, ok := h.files[UnitPath]; !ok {
		t.Error("unit not written")
	}
	if _, ok := h.files[BinPath]; !ok {
		t.Error("binary not copied")
	}
	for _, want := range [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", "wg-guard"},
	} {
		if !h.ran(want...) {
			t.Errorf("command not run: %v", want)
		}
	}
}

// TestInstallRefusesExistingAndBusyPort — difficult to misuse.
func TestInstallRefusesExistingAndBusyPort(t *testing.T) {
	h := newMemHost()
	// Existing install.
	if err := h.WriteFile(StatePath, []byte(`{"schema":1,"mode":"docker"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Install(context.Background(), h, InstallOptions{
		Plan: Defaults(), Yes: true, Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	})
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("existing install: want refusal, got %v", err)
	}

	// Busy panel port.
	h2 := newMemHost()
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = 8080
	h2.portFree = func(string) bool { return false }
	_, err = Install(context.Background(), h2, InstallOptions{
		Plan: p, Yes: true, Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("busy port: want refusal, got %v", err)
	}
}

// TestUninstallDockerKeepsDataByDefault.
func TestUninstallDockerKeepsDataByDefault(t *testing.T) {
	h := newMemHost()
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	if _, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}

	// Dry-run changes nothing.
	if _, err := Uninstall(context.Background(), h, UninstallOptions{
		DryRun: true, Yes: true, Stdout: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.files[StatePath]; !ok {
		t.Fatal("dry-run removed the state file")
	}

	// Real uninstall (confirmed non-interactively).
	if _, err := Uninstall(context.Background(), h, UninstallOptions{
		Yes: true, Stdin: strings.NewReader("uninstall\n"), Stdout: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}
	if !h.ran("docker", "compose", "-f", ComposePth, "down") {
		t.Errorf("compose down not run: %v", h.ranCommands())
	}
	for _, path := range []string{StatePath, ComposePth, ConfigPath, BinPath} {
		if _, ok := h.files[path]; ok {
			t.Errorf("%s still present after uninstall", path)
		}
	}
	if !h.dirs[DataDir] {
		t.Error("data dir missing after uninstall without --purge-data")
	}
}

// TestUninstallPurgeData removes the data dir when explicitly asked.
func TestUninstallPurgeData(t *testing.T) {
	h := newMemHost()
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	if _, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(context.Background(), h, UninstallOptions{
		Yes: true, PurgeData: true, Stdin: strings.NewReader("uninstall\n"), Stdout: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}
	if h.dirs[DataDir] {
		t.Error("data dir kept despite --purge-data")
	}
}

// TestUpdateDockerRollbackOnUnhealthy: a failing health check restores the
// previous compose content.
func TestUpdateDockerRollbackOnUnhealthy(t *testing.T) {
	h := newMemHost()
	// Install with a HEALTHY probe, then kill the health server for the
	// update: any probe now fails → rollback.
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	st, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The memHost probe is a real HTTP request; point the health URL at a
	// closed port to force failure without waiting 90s: rewrite the boot
	// config listen port to something closed.
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	badCfg := strings.Replace(string(h.files[ConfigPath].data),
		fmt.Sprint(port), fmt.Sprint(closedPort), 1)
	if err := h.WriteFile(ConfigPath, []byte(badCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCompose := string(h.files[ComposePth].data)

	err = Update(context.Background(), h, UpdateOptions{
		Image: "wgguard/wg-guard:v9", Stdout: &strings.Builder{},
	})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("update: want rollback error, got %v", err)
	}
	if got := string(h.files[ComposePth].data); got != oldCompose {
		t.Error("compose not restored to the pre-update content")
	}
	for _, want := range [][]string{
		{"docker", "exec", Container, BinPath, "backup", "create", "--reason", "pre-upgrade"},
		{"docker", "compose", "-f", ComposePth, "pull"},
		{"docker", "compose", "-f", ComposePth, "up", "-d"},
	} {
		if !h.ran(want...) {
			t.Errorf("command not run: %v", want)
		}
	}
	if st.Image != DefaultImage {
		t.Fatalf("state image changed by a rolled-back update: %s", st.Image)
	}
}

// TestUpdateNativeRollback: a failing health check restores the previous
// binary.
func TestUpdateNativeRollback(t *testing.T) {
	h := newMemHost()
	h.files["/src/wg-guard"] = memFile{data: []byte("/src/wg-guard"), perm: 0o755}
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.Mode = ModeNative
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	if _, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}
	// Stage a "new" binary and break the health endpoint (closed port).
	if err := h.WriteFile("/tmp/new-wg-guard", []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	badCfg := strings.Replace(string(h.files[ConfigPath].data), fmt.Sprint(port), fmt.Sprint(closedPort), 1)
	if err := h.WriteFile(ConfigPath, []byte(badCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Update(context.Background(), h, UpdateOptions{
		BinaryPath: "/tmp/new-wg-guard", Stdout: &strings.Builder{},
	})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("update: want rollback, got %v", err)
	}
	if string(h.files[BinPath].data) != "/src/wg-guard" {
		t.Errorf("binary not restored: %q", h.files[BinPath].data)
	}
	if !h.ran("systemctl", "restart", "wg-guard") {
		t.Error("service not restarted")
	}
}

// TestUpdateNativeRequiresBinary.
func TestUpdateNativeRequiresBinary(t *testing.T) {
	h := newMemHost()
	h.files["/src/wg-guard"] = memFile{data: []byte("/src/wg-guard"), perm: 0o755}
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.Mode = ModeNative
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	if _, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}
	err := Update(context.Background(), h, UpdateOptions{Stdout: &strings.Builder{}})
	if err == nil || !strings.Contains(err.Error(), "--binary") {
		t.Fatalf("want --binary requirement, got %v", err)
	}
}

// TestPromptWizardScripted: scripted stdin drives the full wizard to the
// same plan flags would produce.
func TestPromptWizardScripted(t *testing.T) {
	h := newMemHost()
	q := newPrompt(strings.NewReader(
		"1\n"+ // mode: docker
			"vpn.example.com\n"+ // domain
			"1\n"+ // tls: acme
			"\n"+ // panel port: default 443
			"\n"+ // acme port: default 80
			"\n"+ // image: default
			"yes\n"), // confirm
		&strings.Builder{}, false)
	p := Defaults()
	p.Mode = "" // force the mode prompt
	if err := q.plan(&p, h); err != nil {
		t.Fatal(err)
	}
	if err := q.confirm(p); err != nil {
		t.Fatal(err)
	}
	res, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if res.TLSMode != config.TLSModeACME || res.Domain != "vpn.example.com" ||
		res.PanelPort != 443 || res.Mode != ModeDocker {
		t.Fatalf("resolved plan: %+v", res)
	}
}

// TestPromptYesNeverReadsStdin: --yes with a poisoned stdin still plans.
func TestPromptYesNeverReadsStdin(t *testing.T) {
	h := newMemHost()
	q := newPrompt(poisonReader{}, &strings.Builder{}, true)
	p := Defaults()
	if err := q.plan(&p, h); err != nil {
		t.Fatal(err)
	}
	if err := q.confirm(p); err != nil {
		t.Fatal(err)
	}
}

type poisonReader struct{}

func (poisonReader) Read([]byte) (int, error) {
	panic("stdin read under --yes")
}

// TestUpdateDockerRollbackFlag: update --rollback re-deploys the state-
// recorded image when compose points at something else (interrupted update).
func TestUpdateDockerRollbackFlag(t *testing.T) {
	h := newMemHost()
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	if _, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate an interrupted update: compose drifted to a broken image while
	// the state still records the good one.
	bad := strings.Replace(string(h.files[ComposePth].data), "image: "+DefaultImage, "image: wgguard/wg-guard:broken", 1)
	if err := h.WriteFile(ComposePth, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), h, UpdateOptions{Rollback: true, Stdout: &strings.Builder{}}); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := imageFromCompose(string(h.files[ComposePth].data)); got != DefaultImage {
		t.Fatalf("compose image after rollback = %s, want %s", got, DefaultImage)
	}
	// Already on the recorded image: refuse, not rewrite.
	if err := Update(context.Background(), h, UpdateOptions{Rollback: true, Stdout: &strings.Builder{}}); err == nil ||
		!strings.Contains(err.Error(), "already references") {
		t.Fatalf("second rollback: want refusal, got %v", err)
	}
}
