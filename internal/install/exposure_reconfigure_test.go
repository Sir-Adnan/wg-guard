package install

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func originNginxPlan(t *testing.T, h *memHost) Plan {
	t.Helper()
	st, err := LoadState(h)
	if err != nil {
		t.Fatal(err)
	}
	current, err := installedPlan(h, st)
	if err != nil {
		t.Fatal(err)
	}
	p := Defaults()
	p.Exposure = ExposureNginx
	p.Certificate = CertificateCloudflareOrigin
	p.Domain = "panel.example.com"
	p.PanelPort = current.PanelPort
	p.PanelPortExplicit = true
	p.CertFile = "/root/panel.pem"
	p.KeyFile = "/root/panel.key"
	cert, key := certificateFixture(t, p.Domain)
	h.files[p.CertFile] = memFile{data: cert, perm: 0o600}
	h.files[p.KeyFile] = memFile{data: key, perm: 0o600}
	h.output["systemctl is-active nginx.service"] = "active\n"
	h.output["nginx -T"] = "http { include /etc/nginx/conf.d/*.conf; }\n"
	return p
}

func TestReconfigurePrivateToNginxAndBackKeepsTheNodeHealthy(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	p := originNginxPlan(t, h)
	next, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: p, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if next.Exposure.Mode != ExposureNginx || next.TLSReadiness != "origin-proxy-unverified" || !strings.HasPrefix(string(h.files[NginxConfigPath].data), nginxManagedMarker) {
		t.Fatalf("Nginx access not committed: %+v", next.Exposure)
	}

	private := Defaults()
	private.Exposure = ExposurePrivate
	private.Certificate = CertificateAuto
	private.PanelPort = p.PanelPort
	private.PanelPortExplicit = true
	next, err = ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: private, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if next.Exposure.Mode != ExposurePrivate || next.TLSReadiness != "not-applicable" {
		t.Fatalf("private access not committed: %+v", next.Exposure)
	}
	for _, path := range []string{NginxConfigPath, ManagedCertPath, ManagedKeyPath} {
		if _, exists := h.files[path]; exists {
			t.Fatalf("retired access artifact remains: %s", path)
		}
	}
	if h.dirs[ACMEWebrootPath] {
		t.Fatal("retired ACME webroot remains")
	}
}

func TestEquivalentAccessTreatsNewCertificateInputsAsIntentionalRotation(t *testing.T) {
	current := Plan{Exposure: ExposureNginx, Certificate: CertificateCloudflareDNS, Domain: "panel.example.com", PanelPort: 8080, PublicPort: 443}
	if !equivalentAccess(current, current) {
		t.Fatal("identical access plans were not equivalent")
	}
	withToken := current
	withToken.CloudflareToken = "synthetic_cloudflare_token_123456"
	if equivalentAccess(current, withToken) {
		t.Fatal("a newly supplied DNS credential was discarded as a no-op")
	}
	withEmail := current
	withEmail.ACMEEmail = "ops@example.com"
	if equivalentAccess(current, withEmail) {
		t.Fatal("a newly supplied ACME account email was discarded as a no-op")
	}
}

func TestReconfigureReplacesManagedNginxCertificateAndHostname(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	first := originNginxPlan(t, h)
	if _, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: first, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Domain = "admin.example.com"
	second.Certificate = CertificateManual
	second.CertFile = "/root/admin.pem"
	second.KeyFile = "/root/admin.key"
	cert, key := certificateFixture(t, second.Domain)
	h.files[second.CertFile] = memFile{data: cert, perm: 0o600}
	h.files[second.KeyFile] = memFile{data: key, perm: 0o600}
	originalProof := proveExposureCertificate
	proveExposureCertificate = func(context.Context, Plan, time.Duration) error { return nil }
	defer func() { proveExposureCertificate = originalProof }()
	next, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: second, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	nginx := string(h.files[NginxConfigPath].data)
	if next.Exposure.Certificate != CertificateManual || !strings.Contains(nginx, "server_name admin.example.com;") || strings.Contains(nginx, "server_name panel.example.com;") {
		t.Fatalf("managed Nginx replacement incomplete:\n%s", nginx)
	}
}

func TestReconfigurePrivateToDirectAndBackRewritesDockerTLSMounts(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	privateState, _ := LoadState(h)
	privatePlan, _ := installedPlan(h, privateState)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	directPort := portOf(server.Listener.Addr().String())
	domain := "panel.example.com"
	cert, key := certificateFixture(t, domain)
	h.files["/root/direct.pem"] = memFile{data: cert, perm: 0o600}
	h.files["/root/direct.key"] = memFile{data: key, perm: 0o600}
	direct := Defaults()
	direct.Exposure = ExposureDirect
	direct.Certificate = CertificateManual
	direct.Domain = domain
	direct.PanelPort = directPort
	direct.PanelPortExplicit = true
	direct.CertFile = "/root/direct.pem"
	direct.KeyFile = "/root/direct.key"
	originalProof := proveExposureCertificate
	proveExposureCertificate = func(context.Context, Plan, time.Duration) error { return nil }
	defer func() { proveExposureCertificate = originalProof }()
	next, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: direct, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	compose := string(h.files[ComposePth].data)
	if next.Exposure.Mode != ExposureDirect || !strings.Contains(compose, ManagedCertPath+":"+ManagedCertPath+":ro") || !strings.Contains(compose, ManagedKeyPath+":"+ManagedKeyPath+":ro") {
		t.Fatalf("Docker manual-TLS mounts missing:\n%s", compose)
	}

	private := Defaults()
	private.Exposure = ExposurePrivate
	private.Certificate = CertificateAuto
	private.PanelPort = privatePlan.PanelPort
	private.PanelPortExplicit = true
	if _, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: private, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	compose = string(h.files[ComposePth].data)
	if strings.Contains(compose, ManagedCertPath) || strings.Contains(compose, ManagedKeyPath) {
		t.Fatalf("private Docker runtime retained TLS mounts:\n%s", compose)
	}
}

func TestReconfigureWritesNativeRuntimeForExternalProxy(t *testing.T) {
	h := installedFixture(t, ModeNative)
	st, _ := LoadState(h)
	current, _ := installedPlan(h, st)
	p := Defaults()
	p.Exposure = ExposureExternalProxy
	p.Certificate = CertificateExternal
	p.Domain = "panel.example.com"
	p.PanelPort = current.PanelPort
	p.PanelPortExplicit = true
	next, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: p, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if next.Exposure.Mode != ExposureExternalProxy || next.TLSReadiness != "external-unverified" || !h.ran("systemctl", "daemon-reload") {
		t.Fatalf("native external-proxy transition incomplete: %+v", next.Exposure)
	}
	if !strings.Contains(string(h.files[ConfigPath].data), `http_listen = "127.0.0.1:`) {
		t.Fatal("external proxy backend is not loopback-only")
	}
}

func TestReconfigureFailureRestoresDurableStateAndArtifacts(t *testing.T) {
	base := installedFixture(t, ModeDocker)
	p := originNginxPlan(t, base)
	stateBefore := append([]byte(nil), base.files[StatePath].data...)
	configBefore := append([]byte(nil), base.files[ConfigPath].data...)
	h := &faultHost{memHost: base, failRun: " up -d"}
	if _, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: p, Stdout: io.Discard}); err == nil {
		t.Fatal("failed candidate start accepted")
	}
	if !bytes.Equal(base.files[StatePath].data, stateBefore) || !bytes.Equal(base.files[ConfigPath].data, configBefore) {
		t.Fatal("failed access transition did not restore state/config")
	}
	for _, path := range []string{NginxConfigPath, ManagedCertPath, ManagedKeyPath} {
		if _, exists := base.files[path]; exists {
			t.Fatalf("failed transition retained %s", path)
		}
	}
	journal, err := LoadJournal(base)
	if err != nil || journal.Stage != "rolled-back" {
		t.Fatalf("rollback was not committed: %+v %v", journal, err)
	}
}

func TestRecoverExposureRestoresInterruptedSnapshot(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	p := originNginxPlan(t, h)
	before, _ := LoadState(h)
	next := cloneState(before)
	next.Exposure = p.ExposureRecord()
	next.TLSReadiness = "origin-proxy-unverified"
	journal := &Journal{Schema: 1, ID: transactionID(), Operation: "exposure", Before: before, After: &next}
	if err := journal.save(h, "prepared"); err != nil {
		t.Fatal(err)
	}
	if err := createExposureBackup(h, journal.ID); err != nil {
		t.Fatal(err)
	}
	if err := journal.save(h, "snapshot-ready"); err != nil {
		t.Fatal(err)
	}
	h.files[ConfigPath] = memFile{data: []byte("interrupted"), perm: 0o600}
	h.files[NginxConfigPath] = memFile{data: []byte(renderNginxProxy(p)), perm: 0o644}
	h.files[ManagedCertPath] = memFile{data: []byte("candidate"), perm: 0o644}
	h.dirs[ACMEWebrootPath] = true
	if err := RecoverExposure(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadState(h)
	if err != nil || restored.Exposure.Mode != ExposurePrivate {
		t.Fatalf("interrupted access state not restored: %+v %v", restored, err)
	}
	if strings.Contains(string(h.files[ConfigPath].data), "interrupted") {
		t.Fatal("interrupted boot config survived recovery")
	}
	if _, exists := h.files[NginxConfigPath]; exists {
		t.Fatal("interrupted Nginx config survived recovery")
	}
	journal, _ = LoadJournal(h)
	if journal.Stage != "rolled-back" {
		t.Fatalf("recovery journal stage = %s", journal.Stage)
	}
}

func TestReconfigureRejectsOccupiedNewPanelPortBeforeMutation(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	h.portFree = func(address string) bool { return address != ":9443" }
	p := Defaults()
	p.Exposure = ExposureDirect
	p.Certificate = CertificateManual
	p.Domain = "panel.example.com"
	p.PanelPort = 9443
	p.PanelPortExplicit = true
	p.CertFile = "/root/panel.pem"
	p.KeyFile = "/root/panel.key"
	before := append([]byte(nil), h.files[StatePath].data...)
	if _, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: p, Stdout: io.Discard}); err == nil {
		t.Fatal("occupied panel port accepted")
	}
	if !bytes.Equal(before, h.files[StatePath].data) {
		t.Fatal("occupied-port refusal mutated state")
	}
}

func TestReconfigureHonorsLifecycleLock(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	unlock, err := h.LockLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := ReconfigureExposure(context.Background(), h, ReconfigureOptions{Plan: Defaults(), Stdout: io.Discard}); err == nil {
		t.Fatal("access reconfiguration bypassed the lifecycle lock")
	}
}
