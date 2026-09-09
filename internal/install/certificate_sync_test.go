package install

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Sir-Adnan/wg-guard/internal/config"
)

func TestRenewManagedCertificateChecksOnlyRecordedLineageWithoutForcing(t *testing.T) {
	h, _, _, _ := installedDirectCertificateFixture(t)
	h.files[CertbotDeployHookPath] = memFile{data: []byte(certbotDeployHook), perm: 0o700}
	h.commands = nil

	if err := RenewManagedCertificate(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState(h)
	want := []string{CertbotPath, "renew", "--cert-name", st.Exposure.Lineage, "--no-random-sleep-on-renew"}
	commands := h.ranCommands()
	if len(commands) != 1 || strings.Join(commands[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("renew command = %v, want %v", commands, want)
	}
	if strings.Contains(strings.Join(commands[0], " "), "force") {
		t.Fatal("interactive renewal bypassed the CA-safe due check")
	}
}

func TestRenewManagedCertificateRefusesMissingHookAndPendingLifecycle(t *testing.T) {
	t.Run("missing hook", func(t *testing.T) {
		h, _, _, _ := installedDirectCertificateFixture(t)
		h.commands = nil
		if err := RenewManagedCertificate(context.Background(), h); err == nil {
			t.Fatal("renewal without the deploy hook was accepted")
		}
		if len(h.commands) != 0 {
			t.Fatalf("renewal mutated host before safety check: %v", h.ranCommands())
		}
	})

	t.Run("pending lifecycle", func(t *testing.T) {
		h, _, _, _ := installedDirectCertificateFixture(t)
		h.files[CertbotDeployHookPath] = memFile{data: []byte(certbotDeployHook), perm: 0o700}
		if err := (&Journal{Schema: 1, ID: "pending", Operation: "update"}).save(h, "started"); err != nil {
			t.Fatal(err)
		}
		h.commands = nil
		if err := RenewManagedCertificate(context.Background(), h); err == nil {
			t.Fatal("renewal raced a pending lifecycle operation")
		}
		if len(h.commands) != 0 {
			t.Fatalf("pending renewal ran Certbot: %v", h.ranCommands())
		}
	})
}

func TestPrepareCertificateHookIsFixedPrivateAndReversible(t *testing.T) {
	h := newMemHost()
	p := Plan{Certificate: CertificateCloudflareDNS}
	cleanup, err := PrepareCertificateHook(h, p)
	if err != nil {
		t.Fatal(err)
	}
	hook := h.files[CertbotDeployHookPath]
	if hook.perm != 0o700 || !strings.Contains(string(hook.data), BinPath+" certificate-sync") || !strings.Contains(string(hook.data), `"${RENEWED_LINEAGE:-}"`) || !strings.Contains(string(hook.data), ">/dev/null 2>&1") || !strings.Contains(string(hook.data), "certificate synchronization failed") {
		t.Fatalf("unsafe deploy hook: mode=%o\n%s", hook.perm, hook.data)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, exists := h.files[CertbotDeployHookPath]; exists {
		t.Fatal("hook cleanup retained a newly-created hook")
	}
}

func installedDirectCertificateFixture(t *testing.T) (*memHost, string, []byte, []byte) {
	t.Helper()
	h := installedFixture(t, ModeDocker)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(server.Close)
	port := portOf(server.Listener.Addr().String())
	identifier := "8.8.8.8"
	lineage := CertificateLineage(identifier)
	oldCert, oldKey := certificateFixture(t, identifier)
	newCert, newKey := certificateFixture(t, identifier)
	h.files[ManagedCertPath] = memFile{data: oldCert, perm: 0o644}
	h.files[ManagedKeyPath] = memFile{data: oldKey, perm: 0o600}
	h.files[CertbotLivePath(lineage)+"/fullchain.pem"] = memFile{data: newCert, perm: 0o644}
	h.files[CertbotLivePath(lineage)+"/privkey.pem"] = memFile{data: newKey, perm: 0o600}
	cfg := config.Defaults()
	cfg.HTTPListen = "0.0.0.0:" + strconv.Itoa(port)
	cfg.TLS.Mode = config.TLSModeManual
	cfg.TLS.CertFile = ManagedCertPath
	cfg.TLS.KeyFile = ManagedKeyPath
	var rendered bytes.Buffer
	if err := toml.NewEncoder(&rendered).Encode(cfg); err != nil {
		t.Fatal(err)
	}
	h.files[ConfigPath] = memFile{data: rendered.Bytes(), perm: 0o600}
	st, err := LoadState(h)
	if err != nil {
		t.Fatal(err)
	}
	st.Schema = StateSchema
	st.PublicIP = identifier
	st.Exposure = (Plan{Exposure: ExposureDirect, Certificate: CertificateIP, PublicIP: identifier, PanelPort: port, PublicPort: port}).ExposureRecord()
	st.TLSReadiness = "verified"
	if err := saveState(h, st); err != nil {
		t.Fatal(err)
	}
	return h, CertbotLivePath(lineage), newCert, oldCert
}

func TestSyncManagedCertificateIgnoresUnrelatedLineage(t *testing.T) {
	h, _, _, oldCert := installedDirectCertificateFixture(t)
	h.commands = nil
	if err := SyncManagedCertificate(context.Background(), h, "/etc/letsencrypt/live/unrelated"); err != nil {
		t.Fatal(err)
	}
	if string(h.files[ManagedCertPath].data) != string(oldCert) || len(h.commands) != 0 {
		t.Fatal("unrelated Certbot lineage changed the node")
	}
}

func TestSyncManagedCertificateCopiesPairAndRestartsDocker(t *testing.T) {
	h, lineagePath, newCert, _ := installedDirectCertificateFixture(t)
	originalProof := proveExposureCertificate
	proveExposureCertificate = func(context.Context, Plan, time.Duration) error { return nil }
	t.Cleanup(func() { proveExposureCertificate = originalProof })
	h.commands = nil
	if err := SyncManagedCertificate(context.Background(), h, lineagePath); err != nil {
		t.Fatal(err)
	}
	if string(h.files[ManagedCertPath].data) != string(newCert) || h.files[ManagedKeyPath].perm != 0o600 {
		t.Fatal("renewed certificate pair was not installed privately")
	}
	if !h.ran("docker", "compose", "-f", ComposePth, "down") || !h.ran("docker", "compose", "-f", ComposePth, "up") {
		t.Fatal("Docker bind-mounted certificate was not remounted through a restart")
	}
	st, err := LoadState(h)
	if err != nil || st.TLSReadiness != "verified" {
		t.Fatalf("certificate readiness not committed: %+v %v", st, err)
	}
}

func TestSyncManagedCertificateRejectsWrongIdentityAndKeepsOldPair(t *testing.T) {
	h, lineagePath, _, oldCert := installedDirectCertificateFixture(t)
	st, _ := LoadState(h)
	wrongCert, wrongKey := certificateFixture(t, "other.example.com")
	h.files[CertbotLivePath(st.Exposure.Lineage)+"/fullchain.pem"] = memFile{data: wrongCert, perm: 0o644}
	h.files[CertbotLivePath(st.Exposure.Lineage)+"/privkey.pem"] = memFile{data: wrongKey, perm: 0o600}
	if err := SyncManagedCertificate(context.Background(), h, lineagePath); err == nil {
		t.Fatal("renewed certificate with wrong identity accepted")
	}
	if string(h.files[ManagedCertPath].data) != string(oldCert) {
		t.Fatal("failed renewal changed the old certificate")
	}
}
