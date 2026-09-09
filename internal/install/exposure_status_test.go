package install

import (
	"context"
	"strings"
	"testing"
	"time"
)

func managedNginxDiagnosticFixture(t *testing.T) (*memHost, *State, Plan) {
	t.Helper()
	h := installedFixture(t, ModeDocker)
	p := Defaults()
	p.Mode = ModeDocker
	p.Image = "ghcr.io/example/wg-guard:test"
	p.Exposure = ExposureNginx
	p.Certificate = CertificateWebroot
	p.Domain = "panel.example.com"
	p.PanelPort = 8087
	p.PanelPortExplicit = true
	p.PublicPort = 443
	p.TLSMode = "proxy"
	p.CertFile = ManagedCertPath
	p.KeyFile = ManagedKeyPath

	boot, err := renderBootConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	h.files[ConfigPath] = memFile{data: boot, perm: 0o600}
	cert, key := certificateFixture(t, p.Domain)
	h.files[ManagedCertPath] = memFile{data: cert, perm: 0o644}
	h.files[ManagedKeyPath] = memFile{data: key, perm: 0o600}
	h.files[CertbotDeployHookPath] = memFile{data: []byte(certbotDeployHook), perm: 0o700}
	lineage := CertificateLineage(p.Domain)
	h.files["/etc/letsencrypt/renewal/"+lineage+".conf"] = memFile{data: []byte("authenticator = webroot\n"), perm: 0o600}
	h.files[NginxConfigPath] = memFile{data: []byte(renderNginxProxy(p)), perm: 0o644}
	h.dirs[ACMEWebrootPath] = true
	h.output["systemctl is-enabled snap.certbot.renew.timer"] = "enabled\n"
	h.output["systemctl is-active snap.certbot.renew.timer"] = "active\n"
	h.output["systemctl is-active nginx.service"] = "active\n"
	h.output["nginx -T"] = "http { include /etc/nginx/conf.d/*.conf; }\n"

	st, err := LoadState(h)
	if err != nil {
		t.Fatal(err)
	}
	st.Schema = StateSchema
	st.Image = p.Image
	st.Exposure = p.ExposureRecord()
	st.Exposure.Lineage = lineage
	st.TLSReadiness = "verified"
	if err := saveState(h, st); err != nil {
		t.Fatal(err)
	}
	return h, st, p
}

func exposureCheck(report *ExposureHealthReport, name string) ExposureHealthCheck {
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	return ExposureHealthCheck{}
}

func TestDiagnoseExposureReportsCertificateRenewalAndNginxHealth(t *testing.T) {
	h, st, _ := managedNginxDiagnosticFixture(t)
	report, err := DiagnoseExposure(context.Background(), h, st, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.Identity != "panel.example.com" || report.CertificateIssuer == "" || report.CertificateExpiresAt.IsZero() {
		t.Fatalf("certificate summary incomplete: %+v", report)
	}
	for _, name := range []string{"certificate", "renewal-lineage", "renewal-hook", "renewal-timer", "nginx"} {
		check := exposureCheck(report, name)
		if check.Name == "" || check.Status == ExposureHealthFail {
			t.Fatalf("%s check = %+v", name, check)
		}
	}
	if !strings.Contains(report.CertificateSANs, "panel.example.com") {
		t.Fatalf("certificate SANs = %q", report.CertificateSANs)
	}
}

func TestDiagnoseExposureSurfacesDriftWithoutReadingSecretContents(t *testing.T) {
	h, st, _ := managedNginxDiagnosticFixture(t)
	const privateHookBytes = "TOP_SECRET_HOOK_BYTES"
	h.files[CertbotDeployHookPath] = memFile{data: []byte(privateHookBytes), perm: 0o755}
	h.files[NginxConfigPath] = memFile{data: []byte("# foreign drift\n"), perm: 0o644}
	h.output["systemctl is-active snap.certbot.renew.timer"] = "inactive\n"

	report, err := DiagnoseExposure(context.Background(), h, st, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"renewal-hook", "renewal-timer", "nginx"} {
		if check := exposureCheck(report, name); check.Status != ExposureHealthFail {
			t.Fatalf("%s drift not failed: %+v", name, check)
		}
	}
	joined := ""
	for _, check := range report.Checks {
		joined += check.Detail + check.Remedy
	}
	if strings.Contains(joined, privateHookBytes) {
		t.Fatal("diagnostics exposed file contents")
	}
}

func TestDiagnoseExposureLabelsPrivateAndExternalOwnershipHonestly(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	st, _ := LoadState(h)
	report, err := DiagnoseExposure(context.Background(), h, st, time.Now())
	if err != nil || exposureCheck(report, "panel-access").Status != ExposureHealthPass {
		t.Fatalf("private report = %+v, %v", report, err)
	}

	p := Defaults()
	p.Mode = ModeDocker
	p.Exposure = ExposureExternalProxy
	p.Certificate = CertificateExternal
	p.Domain = "panel.example.com"
	p.PanelPort = 8080
	p.PublicPort = 443
	p.TLSMode = "proxy"
	boot, _ := renderBootConfig(p)
	h.files[ConfigPath] = memFile{data: boot, perm: 0o600}
	st.Exposure = p.ExposureRecord()
	st.TLSReadiness = "external-unverified"
	report, err = DiagnoseExposure(context.Background(), h, st, time.Now())
	if err != nil || exposureCheck(report, "certificate").Status != ExposureHealthWarn {
		t.Fatalf("external ownership report = %+v, %v", report, err)
	}
}

func TestDiagnoseExposureDetectsAProxyListenerThatEscapedLoopback(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	st, _ := LoadState(h)
	cfg, err := ReadBootConfig(h, ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(h.files[ConfigPath].data)
	from := `http_listen = "` + cfg.HTTPListen + `"`
	raw = strings.Replace(raw, from, `http_listen = "0.0.0.0:8080"`, 1)
	if !strings.Contains(raw, `http_listen = "0.0.0.0:8080"`) {
		t.Fatal("fixture did not rewrite the boot listener")
	}
	h.files[ConfigPath] = memFile{data: []byte(raw), perm: 0o600}
	report, err := DiagnoseExposure(context.Background(), h, st, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if check := exposureCheck(report, "panel-listener"); check.Status != ExposureHealthFail {
		t.Fatalf("public plaintext drift not detected: %+v", check)
	}
}
