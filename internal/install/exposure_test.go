package install

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

func TestResolveExposureSelectsOnlySafeTopologies(t *testing.T) {
	cases := []struct {
		name      string
		plan      Plan
		facts     ExposureFacts
		mode      ExposureMode
		cert      CertificateSource
		tls       config.TLSMode
		listen    string
		publicURL string
		wantErr   bool
	}{
		{
			name: "blank domain stays private",
			plan: Defaults(), facts: ExposureFacts{BackendPort: 8080},
			mode: ExposurePrivate, tls: config.TLSModeDev, listen: "127.0.0.1:8080",
		},
		{
			name:  "free domain uses built in ACME",
			plan:  func() Plan { p := Defaults(); p.Domain = "panel.example.com"; return p }(),
			facts: ExposureFacts{HTTPPortFree: true, HTTPSPortFree: true, BackendPort: 8080},
			mode:  ExposureDirect, cert: CertificateBuiltin, tls: config.TLSModeACME,
			listen: "0.0.0.0:443", publicURL: "https://panel.example.com",
		},
		{
			name:  "active standard nginx uses webroot and loopback",
			plan:  func() Plan { p := Defaults(); p.Domain = "panel.example.com"; return p }(),
			facts: ExposureFacts{NginxInstalled: true, NginxActive: true, NginxStandard: true, BackendPort: 8081},
			mode:  ExposureNginx, cert: CertificateWebroot, tls: config.TLSModeProxy,
			listen: "127.0.0.1:8081", publicURL: "https://panel.example.com",
		},
		{
			name:    "domain conflict refuses coexistence",
			plan:    func() Plan { p := Defaults(); p.Domain = "panel.example.com"; return p }(),
			facts:   ExposureFacts{NginxInstalled: true, NginxActive: true, NginxStandard: true, NginxDomainConflict: true, BackendPort: 8081},
			wantErr: true,
		},
		{
			name:  "unknown public listener is never stopped",
			plan:  func() Plan { p := Defaults(); p.Domain = "panel.example.com"; return p }(),
			facts: ExposureFacts{BackendPort: 8081}, wantErr: true,
		},
		{
			name: "explicit public IP HTTPS",
			plan: func() Plan {
				p := Defaults()
				p.Exposure = ExposureDirect
				p.Certificate = CertificateIP
				p.PublicIP = "8.8.8.8"
				return p
			}(),
			facts: ExposureFacts{HTTPPortFree: true, HTTPSPortFree: true, BackendPort: 8080},
			mode:  ExposureDirect, cert: CertificateIP, tls: config.TLSModeManual,
			listen: "0.0.0.0:443", publicURL: "https://8.8.8.8",
		},
		{
			name: "operator proxy remains loopback",
			plan: func() Plan {
				p := Defaults()
				p.Domain = "panel.example.com"
				p.Exposure = ExposureExternalProxy
				p.Certificate = CertificateExternal
				return p
			}(),
			facts: ExposureFacts{BackendPort: 8090}, mode: ExposureExternalProxy,
			cert: CertificateExternal, tls: config.TLSModeProxy, listen: "127.0.0.1:8090",
			publicURL: "https://panel.example.com",
		},
		{
			name: "public plaintext cannot be requested",
			plan: func() Plan {
				p := Defaults()
				p.Domain = "panel.example.com"
				p.Exposure = ExposureDirect
				p.Certificate = CertificateExternal
				return p
			}(),
			facts: ExposureFacts{HTTPPortFree: true, HTTPSPortFree: true, BackendPort: 8080}, wantErr: true,
		},
		{
			name: "origin CA cannot be direct browser TLS",
			plan: func() Plan {
				p := Defaults()
				p.Domain = "panel.example.com"
				p.Exposure = ExposureDirect
				p.Certificate = CertificateCloudflareOrigin
				p.CertFile = "/root/origin.pem"
				p.KeyFile = "/root/origin.key"
				return p
			}(),
			facts: ExposureFacts{HTTPPortFree: true, HTTPSPortFree: true, BackendPort: 8080}, wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveExposure(tc.plan, tc.facts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("unsafe topology accepted: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err = got.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			if got.Exposure != tc.mode || got.Certificate != tc.cert || got.TLSMode != tc.tls || got.HTTPListen() != tc.listen || got.PublicURL() != tc.publicURL {
				t.Fatalf("resolved = exposure=%s cert=%s tls=%s listen=%s public=%s", got.Exposure, got.Certificate, got.TLSMode, got.HTTPListen(), got.PublicURL())
			}
		})
	}
}

func TestInspectExposureRecognizesOnlyStandardConflictFreeNginx(t *testing.T) {
	h := newMemHost()
	h.portFree = func(addr string) bool { return addr == "127.0.0.1:8082" }
	h.output["systemctl is-active nginx.service"] = "active\n"
	h.output["nginx -T"] = "http { include /etc/nginx/conf.d/*.conf; }\nserver { listen 443 ssl; server_name other.example.com; }\n"
	facts, err := InspectExposure(context.Background(), h, "panel.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !facts.NginxInstalled || !facts.NginxActive || !facts.NginxStandard || facts.NginxDomainConflict || facts.BackendPort != 8082 {
		t.Fatalf("facts = %+v", facts)
	}

	h.output["nginx -T"] += "server { server_name panel.example.com; }\n"
	facts, err = InspectExposure(context.Background(), h, "panel.example.com")
	if err != nil || !facts.NginxDomainConflict {
		t.Fatalf("exact domain conflict missed: %+v %v", facts, err)
	}

	h.failCmd["nginx"] = exec.ErrNotFound
	facts, err = InspectExposure(context.Background(), h, "panel.example.com")
	if err != nil || facts.NginxInstalled {
		t.Fatalf("absent nginx misdetected: %+v %v", facts, err)
	}
}

func TestStateSchemaThreeValidatesExposureWithoutSecrets(t *testing.T) {
	base := &State{
		Schema: StateSchema, Mode: ModeDocker, ConfigPath: ConfigPath, DataDir: DataDir,
		ComposePath: ComposePth,
		Exposure: ExposureState{Mode: ExposureNginx, Certificate: CertificateWebroot,
			PublicURL: "https://panel.example.com", BackendPort: 8080, PublicPort: 443,
			NginxConfigPath: NginxConfigPath, ACMEWebroot: ACMEWebrootPath,
			CertFile: ManagedCertPath, KeyFile: ManagedKeyPath, DeployHook: CertbotDeployHookPath,
			Lineage: CertificateLineage("panel.example.com")},
	}
	if err := validateState(base); err != nil {
		t.Fatal(err)
	}
	raw := strings.ToLower(base.Exposure.PublicURL + base.Exposure.CertFile + base.Exposure.KeyFile)
	if strings.Contains(raw, "token") || strings.Contains(raw, "secret") {
		t.Fatal("state surface unexpectedly carries secret material")
	}

	bad := *base
	bad.Exposure.PublicURL = "http://panel.example.com"
	if validateState(&bad) == nil {
		t.Fatal("public plaintext state accepted")
	}
	legacy := *base
	legacy.Schema = 2
	legacy.Exposure = ExposureState{}
	if err := validateState(&legacy); err != nil {
		t.Fatalf("schema-two state lost compatibility: %v", err)
	}
}

func TestCloudflareTokenFileMustBePrivateAndNeverAppearsInErrors(t *testing.T) {
	h := newMemHost()
	const token = "synthetic_cloudflare_token_123456"
	h.files["/root/cf-token"] = memFile{data: []byte(token + "\n"), perm: 0o600}
	got, err := ReadCloudflareToken(h, "/root/cf-token")
	if err != nil || got != token {
		t.Fatalf("protected token read = %q, %v", got, err)
	}
	h.files["/root/cf-token"] = memFile{data: []byte(token), perm: 0o644}
	_, err = ReadCloudflareToken(h, "/root/cf-token")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("unsafe token file accepted or leaked: %v", err)
	}
}
