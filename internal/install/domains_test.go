package install

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domainqueue"
	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/layout"
)

func domainFixture(t *testing.T) *memHost {
	t.Helper()
	h := installedFixture(t, ModeDocker)
	contractFixture(h)
	st, _ := LoadState(h)
	a, err := retainCurrent(context.Background(), h, st)
	if err != nil {
		t.Fatal(err)
	}
	st.Current = a
	p := Defaults()
	p.Image = st.Image
	p.Domain = "panel.example.test"
	p.TLSMode = config.TLSModeACME
	p.Exposure = ExposureDirect
	p.Certificate = CertificateBuiltin
	p.PanelPort = 443
	p.ACMEHTTPPort = healthServer(t, http.StatusOK)
	st.Exposure = p.ExposureRecord()
	if err := writeAccessRuntime(context.Background(), h, p, st); err != nil {
		t.Fatal(err)
	}
	if err := saveState(h, st); err != nil {
		t.Fatal(err)
	}
	h.commands = nil
	old := proveDomainCertificate
	proveDomainCertificate = func(context.Context, Host, Plan, *x509.CertPool) error { return nil }
	t.Cleanup(func() { proveDomainCertificate = old })
	return h
}

func domainOptions(t *testing.T, role domaintls.Role, origin string) DomainOptions {
	t.Helper()
	o, err := domaintls.ParseOrigin(origin)
	if err != nil {
		t.Fatal(err)
	}
	cert, key := certificateFixture(t, o.Host)
	block, _ := pem.Decode(cert)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return DomainOptions{Role: role, Origin: origin, Method: domaintls.Manual, ExpectedRevision: "legacy", CertPEM: cert, KeyPEM: key, Roots: roots}
}

func TestDomainActivationKeepsDataAndPinsPriorCustomerOrigin(t *testing.T) {
	h := domainFixture(t)
	h.files[DataDir+"/customer-fixture"] = memFile{data: []byte("opaque customer keys and token"), perm: 0600}
	before := append([]byte(nil), h.files[DataDir+"/customer-fixture"].data...)
	o := domainOptions(t, domaintls.Panel, "https://new-panel.example.test")
	i, err := ConfigureDomain(context.Background(), h, o)
	if err != nil {
		t.Fatal(err)
	}
	if i.PanelOrigin != o.Origin || i.SubscriptionOrigin != "https://panel.example.test" {
		t.Fatal("panel change replaced customer origin")
	}
	if !bytes.Equal(before, h.files[DataDir+"/customer-fixture"].data) {
		t.Fatal("customer data changed")
	}
	p, err := readDomainPolicy(h)
	if err != nil || len(p.Sites) != 2 {
		t.Fatal("active policy", err)
	}
	if strings.Contains(string(h.files[JournalPath].data), "PRIVATE KEY") || strings.Contains(string(h.files[layout.DomainPolicy].data), "PRIVATE KEY") {
		t.Fatal("private material entered receipt")
	}
	st, err := LoadState(h)
	if err != nil || st.Exposure.Certificate != CertificateDomains || st.TLSReadiness != "verified" {
		t.Fatal("state did not describe domain ownership", err)
	}
	if _, err := ConfigureDomain(context.Background(), h, o); err == nil {
		t.Fatal("stale revision accepted")
	}
	oldContract := CurrentContract()
	oldContract.DomainProtocol = 0
	if err := checkDomainRuntimeCompatibility(h, oldContract); err == nil {
		t.Fatal("rollback removed route isolation")
	}
	if _, err := ResolveReconfigurePlan(context.Background(), h, mustInstalledPlan(t, h), Defaults()); err == nil {
		t.Fatal("legacy wizard discarded independent domains")
	}
}

func mustInstalledPlan(t *testing.T, h Host) Plan {
	t.Helper()
	st, err := LoadState(h)
	if err != nil {
		t.Fatal(err)
	}
	p, err := installedPlan(h, st)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDomainInvalidMaterialDoesNotInterruptWorkingListener(t *testing.T) {
	for _, kind := range []string{"key", "name", "trust", "port"} {
		t.Run(kind, func(t *testing.T) {
			h := domainFixture(t)
			o := domainOptions(t, domaintls.Subscription, "https://sub.example.test")
			switch kind {
			case "key":
				_, o.KeyPEM = certificateFixture(t, "sub.example.test")
			case "name":
				o.Origin = "https://other.example.test"
			case "trust":
				o.Roots = x509.NewCertPool()
			case "port":
				o.Origin += ":8443"
			}
			before := append([]byte(nil), h.files[ConfigPath].data...)
			if _, err := ConfigureDomain(context.Background(), h, o); err == nil {
				t.Fatal("unsafe activation accepted")
			}
			if h.ran("docker", "compose", "-f", ComposePth, "down") || !bytes.Equal(before, h.files[ConfigPath].data) {
				t.Fatal("validation interrupted active runtime")
			}
			j, err := LoadJournal(h)
			if err != nil || !j.terminal() {
				t.Fatal("preparation failure left recovery pending", err)
			}
		})
	}
}

func TestDomainActivationFailureRestoresBootComposePolicyAndState(t *testing.T) {
	for _, kind := range []string{"start", "policy", "state", "proof", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			m := domainFixture(t)
			h := &faultHost{memHost: m}
			oldCfg := append([]byte(nil), m.files[ConfigPath].data...)
			oldCompose := append([]byte(nil), m.files[ComposePth].data...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "start":
				h.failRun = " up -d"
			case "policy":
				h.failRename = layout.DomainPolicy
			case "state":
				h.failRename = StatePath
			case "proof":
				proveDomainCertificate = func(context.Context, Host, Plan, *x509.CertPool) error { return errors.New("synthetic proof failure") }
			case "cancel":
				h.cancel = cancel
			}
			_, activationErr := ConfigureDomain(ctx, h, domainOptions(t, domaintls.Subscription, "https://sub.example.test"))
			if activationErr == nil {
				t.Fatal("activation failure ignored")
			}
			if !bytes.Equal(oldCfg, m.files[ConfigPath].data) || !bytes.Equal(oldCompose, m.files[ComposePth].data) {
				t.Fatal("runtime configuration not recovered")
			}
			if _, exists := m.files[layout.DomainPolicy]; exists {
				t.Fatal("failed candidate remained active")
			}
			j, err := LoadJournal(h)
			if err != nil || j.Stage != "rolled-back" {
				t.Fatalf("recovery not completed: stage=%s error=%v activation=%v", j.Stage, err, activationErr)
			}
			if len(m.files[DataDir+"/customer-fixture"].data) > 0 {
				t.Fatal("unexpected data mutation")
			}
		})
	}
}

func TestDomainRemovalRetiresCertificateAndReusesPanelOrigin(t *testing.T) {
	h := domainFixture(t)
	i, err := ConfigureDomain(context.Background(), h, domainOptions(t, domaintls.Subscription, "https://sub.example.test"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := readDomainPolicy(h)
	sub, _ := p.Site(domaintls.Subscription)
	cert, key, _ := domaintls.PairFiles(layout.DomainPolicy, sub.CertificateID)
	i, err = ConfigureDomain(context.Background(), h, DomainOptions{Role: domaintls.Subscription, Remove: true, ExpectedRevision: i.Revision})
	if err != nil || i.SubscriptionOrigin != i.PanelOrigin {
		t.Fatal("removal", err)
	}
	if _, ok := h.files[cert]; ok {
		t.Fatal("retired certificate not pruned")
	}
	if _, ok := h.files[key]; ok {
		t.Fatal("retired private key not pruned")
	}
}

func TestControlledDomainInputsAndPrivateFilePermissions(t *testing.T) {
	h := newMemHost()
	st := &State{}
	cert := filepath.Join(layout.ConfigDir, "certificate-import", "subscription", "fullchain.pem")
	key := filepath.Join(filepath.Dir(cert), "privkey.pem")
	h.files[cert] = memFile{data: []byte("synthetic cert"), perm: 0644}
	h.files[key] = memFile{data: []byte("synthetic key"), perm: 0600}
	r := domainqueue.Request{Input: domainqueue.Input{Operation: "configure", Role: domaintls.Subscription, Method: domaintls.Manual, CertSource: cert, KeySource: key}}
	if _, err := ReadDomainInput(h, st, r); err != nil {
		t.Fatal("private controlled source rejected", err)
	}
	h.files[key] = memFile{data: []byte("synthetic key"), perm: 0644}
	if _, err := ReadDomainInput(h, st, r); err == nil {
		t.Fatal("public private-key source accepted")
	}
	r.CertSource = "/root/other-secret"
	if _, err := ReadDomainInput(h, st, r); err == nil {
		t.Fatal("arbitrary root path admitted")
	}
}

func TestCertbotSymlinkStaysInsideRecordedArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive")
	live := filepath.Join(dir, "live")
	if err := os.MkdirAll(archive, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(live, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(archive, "fullchain1.pem")
	if err := os.WriteFile(target, []byte("synthetic public material"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(live, "fullchain.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if got, err := resolveCertbotMaterial(link, archive); err != nil || got != target {
		t.Fatal("recorded Certbot symlink rejected", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "private.pem"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveCertbotMaterial(link, archive); err == nil {
		t.Fatal("archive escape admitted")
	}
}

func TestDomainOwnerReauthorizationHoldsLeaseAndRejectsRevocation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, DatabasePath: filepath.Join(dir, "wg-guard.db"), MasterKeyFile: filepath.Join(dir, "master.key")}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	a, err := admin.NewService(db, nil).Create(ctx, "domain-owner", "synthetic-password-only", auth.RoleOwner, nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := authorizeDomainData(ctx, cfg, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lease, err := (&backup.Service{Cfg: cfg}).OpenData(true); err == nil {
		lease.Close()
		t.Fatal("domain approval allowed concurrent restore")
	}
	release()
	if _, err := db.Exec(`UPDATE admins SET enabled=0 WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if release, err := authorizeDomainData(ctx, cfg, a.ID); !errors.Is(err, ErrMaintenanceAuthorization) {
		if release != nil {
			release()
		}
		t.Fatal("disabled owner kept root authority")
	}
	if _, err := os.Stat(cfg.MasterKeyFile); !os.IsNotExist(err) {
		t.Fatal("authorization generated or read a master key")
	}
}

func TestDomainBrokerUninstallRefusesForeignUnitsAndStopsOwnedWorker(t *testing.T) {
	h := domainFixture(t)
	if err := EnsureDomainBroker(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	h.files[DomainBrokerTimerPath] = memFile{data: []byte("foreign timer"), perm: 0644}
	if _, err := Uninstall(context.Background(), h, UninstallOptions{DryRun: true, Stdout: io.Discard}); err == nil {
		t.Fatal("foreign timer admitted")
	}
	h.files[DomainBrokerTimerPath] = memFile{data: []byte(RenderDomainBrokerTimer()), perm: 0644}
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if !h.ran("systemctl", "stop", "wg-guard-domains.service") {
		t.Fatal("root worker survived uninstall")
	}
	if _, ok := h.files[DomainBrokerServicePath]; ok {
		t.Fatal("owned unit survived uninstall")
	}
}

func TestAutomaticDomainIssuancePreparesOwnedChallengeBeforeActivation(t *testing.T) {
	for _, challenge := range []string{"http", "cloudflare"} {
		t.Run(challenge, func(t *testing.T) {
			m := domainFixture(t)
			h := &certbotPreparationHost{memHost: m, installed: true}
			o := domainOptions(t, domaintls.Subscription, "https://sub.example.test")
			o.Method = domaintls.Automatic
			o.Challenge = challenge
			base := CertbotLivePath(DomainLineage("sub.example.test"))
			m.files[base+"/fullchain.pem"] = memFile{data: o.CertPEM, perm: 0644}
			m.files[base+"/privkey.pem"] = memFile{data: o.KeyPEM, perm: 0600}
			o.CertPEM, o.KeyPEM = nil, nil
			m.files[CloudflareTokenPath] = memFile{data: []byte("dns_cloudflare_api_token = synthetic_fixture_only"), perm: 0600}
			if _, err := ConfigureDomain(context.Background(), h, o); err != nil {
				t.Fatal(err)
			}
			issued, stopped := -1, -1
			for i, c := range m.commands {
				command := strings.Join(c.argv, " ")
				if strings.HasPrefix(command, CertbotPath+" certonly") {
					issued = i
					if strings.Contains(command, "--force-renewal") {
						t.Fatal("forced CA renewal")
					}
					if challenge == "http" && !strings.Contains(command, "--webroot-path "+layout.DomainChallenges) {
						t.Fatal("owned HTTP challenge not used")
					}
				}
				if strings.Contains(command, "compose -f "+ComposePth+" down") {
					stopped = i
				}
			}
			if issued < 0 || stopped < issued {
				t.Fatal("certificate was not prepared before stopping listener")
			}
			if _, ok := m.files[filepath.Join(layout.DomainChallenges, "enrollment.json")]; ok {
				t.Fatal("temporary enrollment survived issuance")
			}
		})
	}
}

func TestRenewalRevalidatesKeyAndIgnoresUnrelatedLineage(t *testing.T) {
	h := domainFixture(t)
	o := domainOptions(t, domaintls.Subscription, "https://sub.example.test")
	o.Method = domaintls.Automatic
	o.Challenge = "http"
	if _, err := ConfigureDomain(context.Background(), h, o); err != nil {
		t.Fatal(err)
	}
	base := CertbotLivePath(DomainLineage("sub.example.test"))
	h.files[base+"/fullchain.pem"] = memFile{data: o.CertPEM, perm: 0644}
	h.files[base+"/privkey.pem"] = memFile{data: o.KeyPEM, perm: 0600}
	h.commands = nil
	if handled, err := syncDomainRenewal(context.Background(), h, "/unrelated/secret", o.Roots); err != nil || handled {
		t.Fatal("unrelated lineage handled", err)
	}
	if handled, err := syncDomainRenewal(context.Background(), h, base, o.Roots); err != nil || !handled {
		t.Fatal("unchanged pair rejected", err)
	}
	_, wrong := certificateFixture(t, "sub.example.test")
	h.files[base+"/privkey.pem"] = memFile{data: wrong, perm: 0600}
	if _, err := syncDomainRenewal(context.Background(), h, base, o.Roots); err == nil {
		t.Fatal("same public fingerprint skipped a mismatched key")
	}
	if h.ran("docker", "compose") {
		t.Fatal("bad/unchanged renewal interrupted listener")
	}
}

func TestDomainUninstallStopsOnlyOwnedNamedRenewal(t *testing.T) {
	h := domainFixture(t)
	o := domainOptions(t, domaintls.Subscription, "https://sub.example.test")
	o.Method = domaintls.Automatic
	o.Challenge = "http"
	if _, err := ConfigureDomain(context.Background(), h, o); err != nil {
		t.Fatal(err)
	}
	h.commands = nil
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Yes: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if !h.ran(CertbotPath, "delete", "--non-interactive", "--cert-name", DomainLineage("sub.example.test")) {
		t.Fatal("owned automatic lineage survived uninstall")
	}
	for _, cmd := range h.commands {
		if len(cmd.argv) > 1 && cmd.argv[0] == CertbotPath && cmd.argv[1] == "delete" && cmd.argv[len(cmd.argv)-1] != DomainLineage("sub.example.test") {
			t.Fatal("unrelated lineage was removed")
		}
	}
}
