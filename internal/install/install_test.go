package install

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
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
	if got := res.PanelURL(); got != "http://127.0.0.1:8080" {
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
	if got := h.files[StatePath].perm; got != 0o600 {
		t.Errorf("state perm = %o, want 600", got)
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
	if err := h.WriteFile(StatePath, []byte(`{"schema":1,"mode":"docker","config_path":"/etc/wg-guard/wg-guard.toml","data_dir":"/var/lib/wg-guard","compose_path":"/etc/wg-guard/compose.yaml"}`), 0o644); err != nil {
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
	var out strings.Builder
	if _, err := Uninstall(context.Background(), h, UninstallOptions{
		Yes: true, Stdin: strings.NewReader("uninstall\n"), Stdout: &out,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Data kept at "+DataDir) {
		t.Errorf("summary must state the kept data path, got: %s", out.String())
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
	var out strings.Builder
	if _, err := Uninstall(context.Background(), h, UninstallOptions{
		Yes: true, PurgeData: true, Stdin: strings.NewReader("uninstall\n"), Stdout: &out,
	}); err != nil {
		t.Fatal(err)
	}
	if h.dirs[DataDir] {
		t.Error("data dir kept despite --purge-data")
	}
	if !strings.Contains(out.String(), "Data purged ("+DataDir+")") {
		t.Errorf("summary must state the purge in past tense, got: %s", out.String())
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
	contractFixture(h)

	err = Update(context.Background(), h, UpdateOptions{
		Image: "image:new", BinaryPath: "/tmp/candidate", SkipBackup: true, Stdout: &strings.Builder{},
	})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("update: want rollback error, got %v", err)
	}
	if got := string(h.files[ComposePth].data); got != oldCompose {
		t.Error("compose not restored to the pre-update content")
	}
	for _, want := range [][]string{
		{"docker", "pull", "image:new"},
		{"docker", "compose", "-f", ComposePth, "up", "-d"},
	} {
		if !h.ran(want...) {
			t.Errorf("command not run: %v", want)
		}
	}
	if st.Image != "sha256:"+strings.Repeat("a", 64) {
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
		BinaryPath: "/tmp/new-wg-guard", SkipBackup: true, Stdout: &strings.Builder{},
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
// same plan flags would produce (the optional sections default to skip).
func TestPromptWizardScripted(t *testing.T) {
	h := newMemHost()
	q := newPrompt(strings.NewReader(
		"1\n"+ // mode: docker
			"vpn.example.com\n"+ // domain
			"1\n"+ // tls: acme
			"\n"+ // panel port: default 443
			"\n"+ // acme port: default 80
			"\n"+ // network defaults gate: skip
			"\n"+ // telegram gate: skip
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
	if res.PortMin != 0 || res.MTU != 0 || res.TelegramToken != "" {
		t.Fatalf("skipped sections must leave the plan untouched: %+v", res)
	}
}

// TestPromptWizardCustomSettings: the optional sections collect the VPN
// network defaults and Telegram delivery; the token must never reach the
// output (it travels via stdin only).
func TestPromptWizardCustomSettings(t *testing.T) {
	h := newMemHost()
	var out strings.Builder
	const token = "777000:AAE_test_token_not_real"
	q := newPrompt(strings.NewReader(
		"1\n"+ // mode: docker
			"vpn.example.com\n"+ // domain
			"1\n"+ // tls: acme
			"\n"+ // panel port
			"\n"+ // acme port
			"y\n"+ // network gate
			"40000\n"+ // port range start
			"40500\n"+ // port range end
			"10.77.0.0/24\n"+ // subnet
			"1380\n"+ // MTU
			"9.9.9.9, 149.112.112.112\n"+ // DNS
			"y\n"+ // telegram gate
			token+"\n"+ // bot token (piped stdin: plain read)
			"123456789\n"+ // chat id
			"04:15\n"+ // daily time
			"\n"+ // image
			"yes\n"), // confirm
		&out, false)
	p := Defaults()
	p.Mode = ""
	if err := q.plan(&p, h); err != nil {
		t.Fatal(err)
	}
	if err := q.confirm(p); err != nil {
		t.Fatal(err)
	}
	if p.PortMin != 40000 || p.PortMax != 40500 || p.VPNSubnet != "10.77.0.0/24" ||
		p.MTU != 1380 || p.ClientDNS != "9.9.9.9, 149.112.112.112" ||
		p.TelegramChat != "123456789" || p.TelegramTime != "04:15" ||
		p.TelegramToken != token {
		t.Fatalf("plan: %+v", p)
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("token leaked to the wizard output")
	}
}

// TestPromptWizardEmptyTokenSkips: choosing the Telegram gate but entering
// no token skips the section instead of installing a broken sink.
func TestPromptWizardEmptyTokenSkips(t *testing.T) {
	h := newMemHost()
	q := newPrompt(strings.NewReader(
		"1\n"+ // mode
			"vpn.example.com\n"+ // domain
			"1\n"+ // tls: acme
			"\n"+ // panel port
			"\n"+ // acme port
			"\n"+ // network gate: skip
			"y\n"+ // telegram gate
			"\n"+ // empty token → skip
			"\n"+ // image
			"yes\n"), // confirm
		&strings.Builder{}, false)
	p := Defaults()
	p.Mode = ""
	if err := q.plan(&p, h); err != nil {
		t.Fatal(err)
	}
	if err := q.confirm(p); err != nil {
		t.Fatal(err)
	}
	if p.TelegramToken != "" || p.TelegramChat != "" || p.TelegramTime != "" {
		t.Fatalf("empty token must skip the whole section: %+v", p)
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

// TestPromptYesKeepsExplicitTLS: --yes must never override an explicit --tls
// with a prompt default (regression: the TLS askChoice under --yes flipped
// domain+proxy installs to ACME).
func TestPromptYesKeepsExplicitTLS(t *testing.T) {
	h := newMemHost()
	q := newPrompt(poisonReader{}, &strings.Builder{}, true)
	p := Defaults()
	p.TLSMode = config.TLSModeProxy
	p.Domain = "vpn.example.com"
	if err := q.plan(&p, h); err != nil {
		t.Fatal(err)
	}
	if p.TLSMode != config.TLSModeProxy || p.Domain != "vpn.example.com" {
		t.Fatalf("explicit tls/domain overridden: %+v", p)
	}
}

// TestPromptExplicitTLSNotReasked: an explicit --tls manual is honored —
// the wizard goes straight to the certificate prompts.
func TestPromptExplicitTLSNotReasked(t *testing.T) {
	h := newMemHost()
	q := newPrompt(strings.NewReader(
		"vpn.example.com\n"+ // domain
			"\n"+ // panel port: default 443
			"/etc/certs/fullchain.pem\n"+ // cert file
			"/etc/certs/key.pem\n"+ // key file
			"\n"+ // network gate: skip
			"\n"+ // telegram gate: skip
			"\n"), // image: default
		&strings.Builder{}, false)
	p := Defaults()
	p.TLSMode = config.TLSModeManual
	if err := q.plan(&p, h); err != nil {
		t.Fatal(err)
	}
	if p.TLSMode != config.TLSModeManual || p.CertFile != "/etc/certs/fullchain.pem" ||
		p.KeyFile != "/etc/certs/key.pem" || p.Domain != "vpn.example.com" {
		t.Fatalf("explicit tls plan: %+v", p)
	}
}

// A legacy drift without a journal or previous artifact cannot prove safe rollback.
func TestUpdateDockerRollbackFlag(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	before := string(h.files[ComposePth].data)
	if err := Update(context.Background(), h, UpdateOptions{Rollback: true, Stdout: io.Discard}); err == nil {
		t.Fatal("unjournaled legacy rollback accepted")
	}
	if string(h.files[ComposePth].data) != before {
		t.Fatal("refusal changed compose")
	}
}

// TestPlanSeeds: the seed builder maps plan choices to CLI invocations,
// skips values equal to the registry defaults, and transports the bot token
// via stdin only.
func TestPlanSeeds(t *testing.T) {
	// Domain-only: just the endpoint.
	got := planSeeds(Plan{Domain: "vpn.example.com"})
	if len(got) != 1 || !reflect.DeepEqual(got[0].argv,
		[]string{BinPath, "settings", "set", "node.endpoint", "vpn.example.com"}) {
		t.Fatalf("domain-only seeds: %+v", got)
	}

	// Answers equal to the registry defaults seed nothing extra.
	got = planSeeds(Plan{
		Domain: "vpn.example.com", PortMin: 30000, PortMax: 50000,
		VPNSubnet: "10.8.0.0/24", MTU: 1420, ClientDNS: "1.1.1.1, 1.0.0.1",
	})
	if len(got) != 1 {
		t.Fatalf("default values must not seed: %+v", got)
	}

	// Full customization: every choice lands in order.
	got = planSeeds(Plan{
		Domain: "vpn.example.com", PortMin: 40000, PortMax: 40500,
		VPNSubnet: "10.77.0.0/24", MTU: 1380, ClientDNS: "9.9.9.9",
		TelegramToken: "tok", TelegramChat: "123", TelegramTime: "03:30",
	})
	want := [][]string{
		{BinPath, "settings", "set", "node.endpoint", "vpn.example.com"},
		{BinPath, "settings", "set", "network.port_min", "40000"},
		{BinPath, "settings", "set", "network.port_max", "40500"},
		{BinPath, "settings", "set", "network.default_pool", "10.77.0.0/24"},
		{BinPath, "settings", "set", "network.mtu", "1380"},
		{BinPath, "settings", "set", "network.dns_servers", "9.9.9.9"},
		{BinPath, "settings", "set", "backup.telegram_token", "-stdin"},
		{BinPath, "settings", "set", "backup.telegram_chat", "123"},
		{BinPath, "backup", "schedule-add", "-name", "installer-daily", "-kind", "daily", "-time", "03:30"},
	}
	argvs := make([][]string, len(got))
	for i, s := range got {
		argvs[i] = s.argv
	}
	if !reflect.DeepEqual(argvs, want) {
		t.Fatalf("seeds:\n got %v\nwant %v", argvs, want)
	}
	if string(got[6].stdin) != "tok" {
		t.Fatalf("token stdin payload = %q", got[6].stdin)
	}

	// A schedule without a complete Telegram sink is not created.
	got = planSeeds(Plan{TelegramChat: "123", TelegramTime: "03:30"})
	for _, s := range got {
		if s.argv[1] == "backup" {
			t.Fatalf("schedule requires token+chat: %+v", got)
		}
	}
}

// TestInstallSeedsSettings: the full install applies the wizard's choices
// through the installed CLI before the container starts, and the token
// never appears in argv or output.
func TestInstallSeedsSettings(t *testing.T) {
	h := newMemHost()
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.Mode = ModeDocker
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	p.Domain = "vpn.example.com"
	p.MTU = 1380
	const token = "777000:AAE_install_test_token"
	p.TelegramToken = token
	p.TelegramChat = "123456789"
	p.TelegramTime = "03:30"

	var out strings.Builder
	if _, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdin: strings.NewReader(""),
		Stdout: &out, Stderr: &strings.Builder{},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}

	for _, want := range [][]string{
		{BinPath, "settings", "set", "node.endpoint", "vpn.example.com"},
		{BinPath, "settings", "set", "network.mtu", "1380"},
		{BinPath, "settings", "set", "backup.telegram_token", "-stdin"},
		{BinPath, "settings", "set", "backup.telegram_chat", "123456789"},
		{BinPath, "backup", "schedule-add", "-name", "installer-daily", "-kind", "daily", "-time", "03:30"},
	} {
		if !h.ran(want...) {
			t.Errorf("seed not run: %v (ran: %v)", want, h.ranCommands())
		}
	}

	// Seeding happens BEFORE the service starts (registry caches in memory).
	cmds := h.ranCommands()
	find := func(match []string) int {
		for i, argv := range cmds {
			if reflect.DeepEqual(argv, match) {
				return i
			}
		}
		return -1
	}
	if seed, up := find([]string{BinPath, "settings", "set", "node.endpoint", "vpn.example.com"}),
		find([]string{"docker", "compose", "-f", ComposePth, "up", "-d"}); seed < 0 || up < 0 || seed > up {
		t.Fatalf("seeding must precede compose up (seed %d, up %d)", seed, up)
	}

	// Secret transport: token in the stdin payload, nowhere else.
	for _, c := range h.commands {
		joined := strings.Join(c.argv, " ")
		if strings.Contains(joined, token) {
			t.Fatal("token leaked into argv")
		}
		if strings.Contains(joined, "backup.telegram_token") && string(c.stdin) != token+"\n" {
			t.Fatalf("token stdin payload = %q", c.stdin)
		}
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("token leaked to the install output")
	}
}

// TestInstallSeedFailureAborts: a failed seeding aborts before the service
// starts and leaves no state file, so rerunning install stays possible.
func TestInstallSeedFailureAborts(t *testing.T) {
	h := newMemHost()
	port := healthServer(t, http.StatusOK)
	p := Defaults()
	p.Mode = ModeDocker
	p.TLSMode = config.TLSModeProxy
	p.PanelPort = port
	p.Domain = "vpn.example.com"
	h.failCmd[BinPath] = fmt.Errorf("seed backend down")

	_, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Version: "test", Stdin: strings.NewReader(""),
		Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
	})
	if err == nil || !strings.Contains(err.Error(), "apply public endpoint") {
		t.Fatalf("want seed failure, got %v", err)
	}
	if st, loadErr := LoadState(h); loadErr != nil || st == nil || st.Recovery != "install-incomplete" {
		t.Fatal("failed seed must durably record incomplete ownership")
	}
	if h.ran("docker", "compose", "-f", ComposePth, "up", "-d") {
		t.Fatal("container must not start after a failed seed")
	}
}
