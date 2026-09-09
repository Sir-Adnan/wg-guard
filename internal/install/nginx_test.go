package install

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

func nginxFixture() *memHost {
	h := newMemHost()
	h.output["systemctl is-active nginx.service"] = "active\n"
	h.output["nginx -T"] = "http {\n    include /etc/nginx/conf.d/*.conf;\n}\n"
	return h
}

func nginxPlan() Plan {
	return Plan{
		Exposure: ExposureNginx, Certificate: CertificateWebroot,
		Domain: "panel.example.com", PanelPort: 8087, PublicPort: 443,
		CertFile: ManagedCertPath, KeyFile: ManagedKeyPath,
	}
}

func TestNginxConfigurationsKeepThePanelPrivateAndCanonical(t *testing.T) {
	p := nginxPlan()
	challenge := renderNginxChallenge(p)
	for _, want := range []string{
		nginxManagedMarker, "listen 80;", "server_name panel.example.com;",
		"location ^~ /.well-known/acme-challenge/", "root " + ACMEWebrootPath + ";",
		"location /", "return 404;",
	} {
		if !strings.Contains(challenge, want) {
			t.Fatalf("challenge configuration missing %q:\n%s", want, challenge)
		}
	}
	if strings.Contains(challenge, "proxy_pass") || strings.Contains(challenge, "ssl_certificate") {
		t.Fatalf("challenge configuration exposed the panel before TLS:\n%s", challenge)
	}

	final := renderNginxProxy(p)
	for _, want := range []string{
		"return 308 https://panel.example.com$request_uri;",
		"listen 443 ssl;", "ssl_protocols TLSv1.2 TLSv1.3;",
		"ssl_certificate " + ManagedCertPath + ";",
		"ssl_certificate_key " + ManagedKeyPath + ";",
		`add_header Strict-Transport-Security "max-age=31536000" always;`,
		"proxy_pass http://127.0.0.1:8087;",
		"proxy_set_header Host panel.example.com;",
		"proxy_set_header X-Forwarded-For $remote_addr;",
		"proxy_set_header X-Forwarded-Proto https;",
		"proxy_set_header X-Forwarded-Port 443;",
	} {
		if !strings.Contains(final, want) {
			t.Fatalf("final configuration missing %q:\n%s", want, final)
		}
	}
	if strings.Contains(final, "0.0.0.0:8087") {
		t.Fatalf("backend escaped loopback:\n%s", final)
	}
}

func TestPrepareAndFinalizeNginxAreTransactional(t *testing.T) {
	h := nginxFixture()
	p := nginxPlan()
	cleanup, err := PrepareNginx(context.Background(), h, p, &State{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(h.files[NginxConfigPath].data); got != renderNginxChallenge(p) {
		t.Fatalf("challenge configuration mismatch:\n%s", got)
	}
	if !h.dirs[ACMEWebrootPath] || !h.ran("nginx", "-t") || !h.ran("systemctl", "reload", "nginx.service") {
		t.Fatal("challenge configuration was not validated and activated")
	}
	if err := FinalizeNginx(context.Background(), h, p, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := string(h.files[NginxConfigPath].data); got != renderNginxProxy(p) {
		t.Fatalf("final configuration mismatch:\n%s", got)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.files[NginxConfigPath]; ok || h.dirs[ACMEWebrootPath] {
		t.Fatal("rollback retained newly-created Nginx artifacts")
	}
}

func TestPrepareNginxRefusesExistingOrConflictingOwnership(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configFile bool
		nginxDump  string
	}{
		{name: "managed path already exists", configFile: true},
		{name: "foreign virtual host owns domain", nginxDump: "http { include /etc/nginx/conf.d/*.conf; }\nserver { server_name panel.example.com; }"},
		{name: "nonstandard include", nginxDump: "http { include /srv/nginx/*.conf; }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := nginxFixture()
			if tc.configFile {
				h.files[NginxConfigPath] = memFile{data: []byte("foreign"), perm: 0o644}
			}
			if tc.nginxDump != "" {
				h.output["nginx -T"] = tc.nginxDump
			}
			before := append([]byte(nil), h.files[NginxConfigPath].data...)
			if _, err := PrepareNginx(context.Background(), h, nginxPlan(), &State{}, io.Discard); err == nil {
				t.Fatal("unsafe Nginx ownership accepted")
			}
			if got := h.files[NginxConfigPath].data; string(got) != string(before) {
				t.Fatal("refusal changed an existing Nginx configuration")
			}
		})
	}
}

func TestNginxActivationFailureRestoresExactPriorState(t *testing.T) {
	for _, failure := range []string{"nginx -t", "systemctl reload nginx.service"} {
		t.Run(failure, func(t *testing.T) {
			base := nginxFixture()
			h := &faultHost{memHost: base, failRun: failure}
			if _, err := PrepareNginx(context.Background(), h, nginxPlan(), &State{}, io.Discard); err == nil {
				t.Fatal("activation failure accepted")
			}
			if _, ok := base.files[NginxConfigPath]; ok || base.dirs[ACMEWebrootPath] {
				t.Fatal("activation failure retained new Nginx artifacts")
			}
		})
	}
}

func TestFinalizeNginxFailureRestoresChallengeConfiguration(t *testing.T) {
	base := nginxFixture()
	p := nginxPlan()
	cleanup, err := PrepareNginx(context.Background(), base, p, &State{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	h := &faultHost{memHost: base, failRun: "nginx -t"}
	if err := FinalizeNginx(context.Background(), h, p, io.Discard); err == nil {
		t.Fatal("invalid final configuration accepted")
	}
	if got := string(base.files[NginxConfigPath].data); got != renderNginxChallenge(p) {
		t.Fatal("failed finalization did not restore the active challenge configuration")
	}
}

func TestNginxReplacementAndDetachRestoreThePreviousProxy(t *testing.T) {
	h := nginxFixture()
	previous := nginxPlan()
	previous.Certificate = CertificateManual
	priorConfig := renderNginxProxy(previous)
	h.files[NginxConfigPath] = memFile{data: []byte(priorConfig), perm: 0o640}
	h.dirs[ACMEWebrootPath] = true
	h.output["nginx -T"] += "\nserver { server_name " + previous.Domain + "; }\n"

	next := nginxPlan()
	next.Domain = "new.example.com"
	cleanup, err := PrepareNginxReplacement(context.Background(), h, next, previous, &State{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := FinalizeNginxReplacement(context.Background(), h, next, previous, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if got := h.files[NginxConfigPath]; string(got.data) != priorConfig || got.perm != 0o640 {
		t.Fatal("replacement cleanup did not restore the exact prior proxy")
	}

	detachCleanup, err := DetachManagedNginx(context.Background(), h, previous.ExposureRecord())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := h.files[NginxConfigPath]; exists {
		t.Fatal("detach retained the managed proxy")
	}
	if err := detachCleanup(); err != nil {
		t.Fatal(err)
	}
	if got := string(h.files[NginxConfigPath].data); got != priorConfig {
		t.Fatal("detach cleanup did not restore the prior proxy")
	}
}

func TestNginxReplacementKeepsThePreviousEndpointUntilPromotion(t *testing.T) {
	h := nginxFixture()
	previous := nginxPlan()
	previous.Certificate = CertificateManual
	candidate := previous
	candidate.Domain = "admin.example.com"
	h.files[NginxConfigPath] = memFile{data: []byte(renderNginxProxy(previous)), perm: 0o644}
	h.dirs[ACMEWebrootPath] = true
	cleanup, err := PrepareNginxReplacement(context.Background(), h, candidate, previous, &State{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	transition := string(h.files[NginxConfigPath].data)
	if !strings.Contains(transition, "server_name panel.example.com;") || !strings.Contains(transition, "proxy_pass http://127.0.0.1:8087;") || !strings.Contains(transition, "server_name admin.example.com;") {
		t.Fatalf("transition did not retain the live endpoint and new challenge:\n%s", transition)
	}
	if strings.Count(transition, "proxy_pass") != 1 {
		t.Fatalf("candidate was proxied before certificate readiness:\n%s", transition)
	}
	if err := FinalizeNginxReplacement(context.Background(), h, candidate, previous, io.Discard); err != nil {
		t.Fatal(err)
	}
	final := string(h.files[NginxConfigPath].data)
	if strings.Contains(final, "server_name panel.example.com;") || !strings.Contains(final, "server_name admin.example.com;") {
		t.Fatalf("candidate was not promoted cleanly:\n%s", final)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if got := string(h.files[NginxConfigPath].data); got != renderNginxProxy(previous) {
		t.Fatal("replacement cleanup did not restore the exact previous proxy")
	}
}

func TestRemoveManagedNginxRestoresConfigWhenReloadFails(t *testing.T) {
	base := nginxFixture()
	p := nginxPlan()
	before := renderNginxProxy(p)
	base.files[NginxConfigPath] = memFile{data: []byte(before), perm: 0o644}
	base.dirs[ACMEWebrootPath] = true
	h := &faultHost{memHost: base, failRun: "systemctl reload nginx.service"}
	if err := RemoveManagedNginx(context.Background(), h, p.ExposureRecord()); err == nil {
		t.Fatal("failed removal reload accepted")
	}
	if got := string(base.files[NginxConfigPath].data); got != before || !base.dirs[ACMEWebrootPath] {
		t.Fatal("failed removal did not restore byte-exact Nginx state")
	}
}

func TestRemoveManagedNginxRefusesForeignReplacement(t *testing.T) {
	h := nginxFixture()
	h.files[NginxConfigPath] = memFile{data: []byte("# operator configuration\n"), perm: 0o644}
	if err := RemoveManagedNginx(context.Background(), h, nginxPlan().ExposureRecord()); err == nil {
		t.Fatal("foreign replacement was removed")
	}
	if string(h.files[NginxConfigPath].data) != "# operator configuration\n" {
		t.Fatal("foreign replacement was changed")
	}
}

func nginxInstallFixture(t *testing.T) (*memHost, Plan) {
	t.Helper()
	h := nginxFixture()
	p := Defaults()
	p.Exposure = ExposureNginx
	p.Certificate = CertificateCloudflareOrigin
	p.Domain = "panel.example.com"
	p.PanelPort = healthServer(t, 200)
	p.PanelPortExplicit = true
	p.PublicPort = 443
	p.CertFile = "/root/panel.pem"
	p.KeyFile = "/root/panel.key"
	cert, key := certificateFixture(t, p.Domain)
	h.files[p.CertFile] = memFile{data: cert, perm: 0o600}
	h.files[p.KeyFile] = memFile{data: key, perm: 0o600}
	return h, p
}

func TestInstallAndUninstallOwnTheCompleteNginxSurface(t *testing.T) {
	h, p := nginxInstallFixture(t)
	st, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if st.Exposure.NginxConfigPath != NginxConfigPath || string(h.files[NginxConfigPath].data) != renderNginxProxy(p) {
		t.Fatal("install did not commit the managed TLS proxy")
	}
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{NginxConfigPath, ManagedCertPath, ManagedKeyPath, StatePath} {
		if _, exists := h.files[path]; exists {
			t.Fatalf("uninstall retained managed file %s", path)
		}
	}
	if h.dirs[ACMEWebrootPath] {
		t.Fatal("uninstall retained the managed ACME webroot")
	}
}

func TestLaterInstallFailureRollsBackNginxAndCertificate(t *testing.T) {
	h, p := nginxInstallFixture(t)
	_, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Yes: true, Stdout: io.Discard,
		BeforeStart: func(context.Context, Host, Plan, *State) error {
			return fmt.Errorf("synthetic owner bootstrap failure")
		},
	})
	if err == nil {
		t.Fatal("later install failure accepted")
	}
	for _, path := range []string{NginxConfigPath, ManagedCertPath, ManagedKeyPath} {
		if _, exists := h.files[path]; exists {
			t.Fatalf("failed install retained managed file %s", path)
		}
	}
	if h.dirs[ACMEWebrootPath] {
		t.Fatal("failed install retained the managed ACME webroot")
	}
}
