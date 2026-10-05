package install

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
)

type initialDataFixture struct {
	report backup.RestoreReport
	apply  func(context.Context, *config.Config, string) error
}

func (f initialDataFixture) RestoreReport() *backup.RestoreReport { return &f.report }
func (f initialDataFixture) ApplyInitialData(ctx context.Context, cfg *config.Config, path string) error {
	return f.apply(ctx, cfg, path)
}

func TestInitialArchiveAppliesBeforeListenerWithoutDefaultSeeding(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed-restore"}[failure], func(t *testing.T) {
			h := newMemHost()
			p := Defaults()
			p.PanelPort = healthServer(t, http.StatusOK)
			applied := false
			initial := initialDataFixture{report: backup.RestoreReport{Archive: "synthetic.wgg", Endpoint: "vpn.source.example.test"}, apply: func(ctx context.Context, cfg *config.Config, path string) error {
				applied = true
				if path != ConfigPath || cfg.DataDir != DataDir || cfg.DatabasePath != DataDir+"/wg-guard.db" || cfg.MasterKeyFile != DataDir+"/master.key" {
					t.Fatal("target paths not passed to archive engine")
				}
				for _, cmd := range h.commands {
					line := strings.Join(cmd.argv, " ")
					if strings.Contains(line, " up -d") || strings.Contains(line, "settings set") || strings.Contains(line, "owner-bootstrap") {
						t.Fatal("default credentials/data or listener existed before restore")
					}
				}
				if failure {
					return errors.New("synthetic initial restore failure")
				}
				return nil
			}}
			_, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, InitialData: initial, Stdout: io.Discard})
			if !applied || (err != nil) != failure {
				t.Fatal("initial data result", err)
			}
			if failure && h.ran("docker", "compose", "-f", ComposePth, "up", "-d") {
				t.Fatal("restore failure exposed listener")
			}
		})
	}
}

func TestInitialArchiveRefusesExistingDataAndOwnerOverridesBeforeChanges(t *testing.T) {
	for _, kind := range []string{"existing-data", "owner-override", "missing-kernel"} {
		t.Run(kind, func(t *testing.T) {
			h := newMemHost()
			p := Defaults()
			p.PanelPort = healthServer(t, http.StatusOK)
			initial := initialDataFixture{apply: func(context.Context, *config.Config, string) error { t.Fatal("unsafe restore applied"); return nil }}
			o := InstallOptions{Plan: p, Yes: true, InitialData: initial, Stdout: io.Discard}
			switch kind {
			case "existing-data":
				h.files[p.BootConfig().DatabasePath] = memFile{data: []byte("original data"), perm: 0600}
			case "owner-override":
				o.Owner.Username = "replacement"
			case "missing-kernel":
				o.SkipModule = true
				initial.report.Inventory.KernelInterfaces = 1
				o.InitialData = initial
			}
			if _, err := Install(context.Background(), h, o); err == nil {
				t.Fatal("unsafe install accepted")
			}
			if _, ok := h.files[JournalPath]; ok {
				t.Fatal("rejected install created lifecycle state")
			}
		})
	}
}

func TestInitialArchiveDoesNotPromoteNginxBeforeApply(t *testing.T) {
	h := newMemHost()
	h.output["systemctl is-active nginx.service"] = "active\n"
	h.output["nginx -T"] = "http { include /etc/nginx/conf.d/*.conf; }\n"
	p := Defaults()
	p.Exposure = ExposureNginx
	p.Certificate = CertificateCloudflareOrigin
	p.Domain = "panel.example.test"
	p.PanelPort = healthServer(t, http.StatusOK)
	p.PanelPortExplicit = true
	p.CertFile = "/root/source-cert"
	p.KeyFile = "/root/source-key"
	cert, key := certificateFixture(t, p.Domain)
	h.files[p.CertFile] = memFile{data: cert, perm: 0600}
	h.files[p.KeyFile] = memFile{data: key, perm: 0600}
	called := false
	initial := initialDataFixture{apply: func(context.Context, *config.Config, string) error {
		called = true
		if string(h.files[NginxConfigPath].data) != renderNginxChallenge(p) {
			t.Fatal("public proxy promoted before account restore")
		}
		return nil
	}}
	if _, err := Install(context.Background(), h, InstallOptions{Plan: p, InitialData: initial, Yes: true, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("archive was not applied")
	}
}
