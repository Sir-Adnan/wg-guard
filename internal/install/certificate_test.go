package install

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

type certbotPreparationHost struct {
	*memHost
	installed bool
}

type issuanceFailureHost struct{ *memHost }

func (h *issuanceFailureHost) Run(ctx context.Context, argv []string, timeout time.Duration) error {
	if len(argv) > 1 && argv[0] == CertbotPath && argv[1] == "certonly" {
		h.commands = append(h.commands, memCmd{argv: argv})
		return fmt.Errorf("synthetic issuance failure")
	}
	return h.memHost.Run(ctx, argv, timeout)
}

func (h *certbotPreparationHost) Output(ctx context.Context, argv []string, timeout time.Duration) (string, error) {
	if strings.Join(argv, " ") == CertbotPath+" --version" {
		h.commands = append(h.commands, memCmd{argv: argv})
		if h.installed {
			return "certbot 5.4.0\n", nil
		}
		return "certbot 4.0.0\n", nil
	}
	return h.memHost.Output(ctx, argv, timeout)
}

func (h *certbotPreparationHost) Run(ctx context.Context, argv []string, timeout time.Duration) error {
	command := strings.Join(argv, " ")
	if command == "snap install --classic certbot" || command == "snap refresh certbot" {
		h.installed = true
	}
	return h.memHost.Run(ctx, argv, timeout)
}

func certificateFixture(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(30 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(name); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
}

func TestPrepareCertificateUsesExplicitCertbotStrategies(t *testing.T) {
	const token = "synthetic_cloudflare_token_123456"
	cases := []struct {
		name       string
		plan       Plan
		identifier string
		want       []string
	}{
		{
			name:       "nginx webroot",
			plan:       Plan{Exposure: ExposureNginx, Certificate: CertificateWebroot, Domain: "panel.example.com", ACMEEmail: "ops@example.com", PanelPort: 8080, PublicPort: 443},
			identifier: "panel.example.com", want: []string{"--webroot", "--webroot-path", ACMEWebrootPath, "-d", "panel.example.com", "--email", "ops@example.com"},
		},
		{
			name:       "cloudflare DNS",
			plan:       Plan{Exposure: ExposureNginx, Certificate: CertificateCloudflareDNS, Domain: "panel.example.com", CloudflareToken: token, PanelPort: 8080, PublicPort: 443},
			identifier: "panel.example.com", want: []string{"--dns-cloudflare", "--dns-cloudflare-credentials", CloudflareTokenPath, "-d", "panel.example.com"},
		},
		{
			name:       "short lived public IP",
			plan:       Plan{Exposure: ExposureDirect, Certificate: CertificateIP, PublicIP: "8.8.8.8", PanelPort: 443, PublicPort: 443, ACMEHTTPPort: 80},
			identifier: "8.8.8.8", want: []string{"--preferred-profile", "shortlived", "--ip-address", "8.8.8.8", "--standalone", "--http-01-port", "80"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newMemHost()
			h.output[CertbotPath+" --version"] = "certbot 5.4.0\n"
			cert, key := certificateFixture(t, tc.identifier)
			lineage := CertificateLineage(tc.identifier)
			h.files[CertbotLivePath(lineage)+"/fullchain.pem"] = memFile{data: cert, perm: 0o644}
			h.files[CertbotLivePath(lineage)+"/privkey.pem"] = memFile{data: key, perm: 0o600}
			st := &State{Exposure: tc.plan.ExposureRecord()}
			result, cleanup, err := PrepareCertificate(context.Background(), h, tc.plan, st, io.Discard)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Lineage != lineage || result.CertFile != ManagedCertPath || result.KeyFile != ManagedKeyPath {
				t.Fatalf("result = %+v", result)
			}
			var issuance []string
			for _, command := range h.ranCommands() {
				if len(command) > 1 && command[0] == CertbotPath && command[1] == "certonly" {
					issuance = command
				}
			}
			joined := strings.Join(issuance, " ")
			for _, value := range tc.want {
				if !strings.Contains(joined, value) {
					t.Fatalf("issuance missing %q: %v", value, issuance)
				}
			}
			if strings.Contains(joined, token) {
				t.Fatal("Cloudflare token leaked into argv")
			}
			if tc.plan.Certificate == CertificateCloudflareDNS {
				credentials := h.files[CloudflareTokenPath]
				if credentials.perm != 0o600 || !strings.Contains(string(credentials.data), token) {
					t.Fatal("Cloudflare token was not confined to the protected credentials file")
				}
			}
			if got := h.files[ManagedKeyPath].perm; got != 0o600 {
				t.Fatalf("managed key mode = %o", got)
			}
			stateJSON, _ := json.Marshal(st)
			if strings.Contains(string(stateJSON), token) {
				t.Fatal("Cloudflare token leaked into install state")
			}
		})
	}
}

func TestPrepareCertificateRejectsWrongIdentityAndPreservesPriorCopies(t *testing.T) {
	h := newMemHost()
	h.output[CertbotPath+" --version"] = "certbot 5.4.0\n"
	h.files[ManagedCertPath] = memFile{data: []byte("old-cert"), perm: 0o644}
	h.files[ManagedKeyPath] = memFile{data: []byte("old-key"), perm: 0o600}
	cert, key := certificateFixture(t, "other.example.com")
	lineage := CertificateLineage("panel.example.com")
	h.files[CertbotLivePath(lineage)+"/fullchain.pem"] = memFile{data: cert, perm: 0o644}
	h.files[CertbotLivePath(lineage)+"/privkey.pem"] = memFile{data: key, perm: 0o600}
	p := Plan{Exposure: ExposureNginx, Certificate: CertificateWebroot, Domain: "panel.example.com", PanelPort: 8080, PublicPort: 443}
	_, cleanup, err := PrepareCertificate(context.Background(), h, p, &State{}, io.Discard)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("certificate for the wrong hostname accepted")
	}
	if string(h.files[ManagedCertPath].data) != "old-cert" || string(h.files[ManagedKeyPath].data) != "old-key" {
		t.Fatal("failed certificate preparation damaged prior managed copies")
	}
}

func TestEnsureCertbotUsesOnlyOfficialPinnedSnapPath(t *testing.T) {
	h := &certbotPreparationHost{memHost: newMemHost()}
	if err := ensureCertbot(context.Background(), h, true); err != nil {
		t.Fatal(err)
	}
	commands := h.ranCommands()
	joined := make([]string, 0, len(commands))
	for _, command := range commands {
		joined = append(joined, strings.Join(command, " "))
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{
		"snap install core",
		"snap refresh core",
		"snap install --classic certbot",
		"snap set certbot trust-plugin-with-root=ok",
		"snap install certbot-dns-cloudflare",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in commands:\n%s", want, all)
		}
	}
	if strings.Contains(all, "apt") || strings.Contains(all, "/usr/bin/certbot") {
		t.Fatalf("foreign Certbot ownership was touched:\n%s", all)
	}
}

func TestEnsureCertbotRefreshesExistingSnapWithoutReinstallingIt(t *testing.T) {
	h := &certbotPreparationHost{memHost: newMemHost()}
	h.output["snap list core"] = "Name  Version\ncore  1\n"
	h.output["snap list certbot"] = "Name     Version\ncertbot  4.0.0\n"
	if err := ensureCertbot(context.Background(), h, false); err != nil {
		t.Fatal(err)
	}
	var commands []string
	for _, command := range h.ranCommands() {
		commands = append(commands, strings.Join(command, " "))
	}
	all := strings.Join(commands, "\n")
	if !strings.Contains(all, "snap refresh core") || !strings.Contains(all, "snap refresh certbot") {
		t.Fatalf("existing snaps were not refreshed:\n%s", all)
	}
	if strings.Contains(all, "snap install core") || strings.Contains(all, "snap install --classic certbot") {
		t.Fatalf("existing snaps were needlessly reinstalled:\n%s", all)
	}
}

func TestPrepareCertificateErasesCloudflareCredentialAfterIssuanceFailure(t *testing.T) {
	const token = "synthetic_cloudflare_token_123456"
	h := &issuanceFailureHost{memHost: newMemHost()}
	h.output[CertbotPath+" --version"] = "certbot 5.4.0\n"
	p := Plan{Exposure: ExposureNginx, Certificate: CertificateCloudflareDNS, Domain: "panel.example.com", CloudflareToken: token, PanelPort: 8080, PublicPort: 443}
	var out strings.Builder
	_, _, err := PrepareCertificate(context.Background(), h, p, &State{}, &out)
	if err == nil {
		t.Fatal("issuance failure accepted")
	}
	if _, ok := h.files[CloudflareTokenPath]; ok {
		t.Fatal("failed issuance left Cloudflare credentials behind")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(out.String(), token) {
		t.Fatal("Cloudflare token leaked through an error or terminal output")
	}
}

func TestPrepareCertificateRefusesUnboundedPriorArtifactWithoutChangingIt(t *testing.T) {
	h := newMemHost()
	prior := bytes.Repeat([]byte("x"), (1<<20)+1)
	h.files[ManagedCertPath] = memFile{data: prior, perm: 0o644}
	cert, key := certificateFixture(t, "panel.example.com")
	h.files["/root/panel.pem"] = memFile{data: cert, perm: 0o600}
	h.files["/root/panel.key"] = memFile{data: key, perm: 0o600}
	p := Plan{Exposure: ExposureNginx, Certificate: CertificateManual, Domain: "panel.example.com", CertFile: "/root/panel.pem", KeyFile: "/root/panel.key", PanelPort: 8080, PublicPort: 443}
	if _, _, err := PrepareCertificate(context.Background(), h, p, &State{}, io.Discard); err == nil {
		t.Fatal("unbounded prior managed artifact was accepted as rollback input")
	}
	if got := h.files[ManagedCertPath].data; !bytes.Equal(got, prior) {
		t.Fatal("failed snapshot changed the prior managed artifact")
	}
}
