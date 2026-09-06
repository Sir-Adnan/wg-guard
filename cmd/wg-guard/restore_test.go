package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real archive/database work with only service-manager and installed-layout I/O
// replaced. A routed Docker restore must stay on this host after container down.
type restoreCLIHost struct {
	install.Host
	files      map[string][]byte
	commands   []string
	locked     bool
	afterStart func()
}

func (h *restoreCLIHost) IsRoot() bool { return true }
func (h *restoreCLIHost) LockLifecycle() (func(), error) {
	if h.locked {
		return nil, fs.ErrPermission
	}
	h.locked = true
	return func() { h.locked = false }, nil
}
func (h *restoreCLIHost) Open(p string) (io.ReadCloser, error) {
	b, e := h.ReadFile(p)
	return io.NopCloser(bytes.NewReader(b)), e
}
func (h *restoreCLIHost) ReadFile(p string) ([]byte, error) {
	if b, ok := h.files[p]; ok {
		return b, nil
	}
	return nil, fs.ErrNotExist
}
func (h *restoreCLIHost) WriteFile(p string, b []byte, _ fs.FileMode) error {
	h.files[p] = append([]byte(nil), b...)
	return nil
}
func (h *restoreCLIHost) Rename(a, b string) error {
	v, ok := h.files[a]
	if !ok {
		return fs.ErrNotExist
	}
	h.files[b] = v
	delete(h.files, a)
	return nil
}
func (h *restoreCLIHost) Remove(p string) error              { delete(h.files, p); return nil }
func (h *restoreCLIHost) MkdirAll(string, fs.FileMode) error { return nil }
func (h *restoreCLIHost) Stat(p string) (fs.FileInfo, error) {
	if _, ok := h.files[p]; ok {
		return os.Stat(os.Args[0])
	}
	return nil, fs.ErrNotExist
}
func (h *restoreCLIHost) Run(_ context.Context, args []string, _ time.Duration) error {
	cmd := strings.Join(args, " ")
	h.commands = append(h.commands, cmd)
	if strings.Contains(cmd, " restart ") || strings.Contains(cmd, " up -d") {
		if h.afterStart != nil {
			h.afterStart()
		}
	}
	return nil
}
func (h *restoreCLIHost) Output(_ context.Context, args []string, _ time.Duration) (string, error) {
	h.commands = append(h.commands, strings.Join(args, " "))
	if args[0] == "systemctl" {
		return "LoadState=loaded\nActiveState=inactive\n", nil
	}
	return "", nil
}

func TestRestoreCryptoFailuresLocalizedBeforeServiceMutation(t *testing.T) {
	cfgPath := testTokenConfig(t)
	env, err := loadCLIEnv(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	svc := env.newBackupService()
	archive, err := svc.Create(context.Background(), backup.CreateOpts{Password: "synthetic-cli-password"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(t.TempDir(), "invalid.wgg")
	if err := os.WriteFile(invalid, []byte("age-encryption.org/v1\nsynthetic-sensitive-invalid-header\n"), 0600); err != nil {
		t.Fatal(err)
	}
	st := install.State{Schema: 1, Mode: install.ModeNative, ConfigPath: install.ConfigPath, DataDir: install.DataDir, BinPath: install.BinPath, UnitPath: install.UnitPath, ComposePath: install.ComposePth}
	state, _ := json.Marshal(st)
	for _, locale := range []string{"fa", "en"} {
		for _, kind := range []string{"missing", "wrong", "invalid"} {
			t.Run(locale+"/"+kind, func(t *testing.T) {
				h := &restoreCLIHost{files: map[string][]byte{install.StatePath: state, install.ConfigPath: []byte("")}}
				args := []string{archive.Path, "--lang", locale, "--yes"}
				input := ""
				if kind != "missing" {
					args = append(args, "--password")
					input = "wrong-password\n"
				}
				if kind == "invalid" {
					args[0] = invalid
				}
				var out bytes.Buffer
				err := runRestoreWithServiceFactory(context.Background(), args, strings.NewReader(input), &out, h, func(*config.Config) *backup.Service { return svc })
				want := "password"
				if locale == "fa" {
					want = "گذرواژه"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("untranslated restore crypto error: %v", err)
				}
				if strings.Contains(err.Error(), "synthetic-sensitive") || strings.Contains(err.Error(), "wrong-password") {
					t.Fatal("secret/header echoed")
				}
				if len(h.commands) != 0 {
					t.Fatal("service mutated on crypto refusal")
				}
				if _, ok := h.files[install.JournalPath]; ok {
					t.Fatal("restore journal written before crypto refusal")
				}
			})
		}
	}
}

func TestRestoreCLIUsesHostCoordinatorInNativeAndDockerModes(t *testing.T) {
	for _, mode := range []install.Mode{install.ModeNative, install.ModeDocker} {
		t.Run(string(mode), func(t *testing.T) {
			cfgPath := testTokenConfig(t)
			e, err := loadCLIEnv(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := e.Reg.SetRaw(ctx, "node.id", "archived"); err != nil {
				t.Fatal(err)
			}
			archive, err := e.newBackupService().Create(ctx, backup.CreateOpts{Password: "synthetic-cli-password"})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Reg.SetRaw(ctx, "node.id", "after-archive"); err != nil {
				t.Fatal(err)
			}
			e.Close()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))
			defer ts.Close()
			cfg := e.Cfg
			cfg.HTTPListen = strings.TrimPrefix(ts.URL, "http://")
			cfg.TLS.Mode = config.TLSModeDev
			managed := *cfg
			managed.DataDir = install.DataDir
			managed.DatabasePath = filepath.Join(install.DataDir, "wg-guard.db")
			managed.MasterKeyFile = filepath.Join(install.DataDir, "master.key")
			configCopy := filepath.Join(t.TempDir(), "node.toml")
			if err := managed.Save(configCopy); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(configCopy)
			if err != nil {
				t.Fatal(err)
			}
			state := &install.State{Schema: 1, Mode: mode, ConfigPath: install.ConfigPath, DataDir: install.DataDir, BinPath: install.BinPath, UnitPath: install.UnitPath, ComposePath: install.ComposePth}
			stateBytes, _ := json.Marshal(state)
			h := &restoreCLIHost{files: map[string][]byte{install.StatePath: stateBytes, install.ConfigPath: raw, install.ComposePth: []byte("fixture")}}
			h.afterStart = func() {
				env, err := loadCLIEnv(cfgPath)
				if err != nil {
					t.Fatal(err)
				}
				defer env.Close()
				id, err := env.Reg.GetString(ctx, "node.id")
				if err != nil || id != "archived" {
					t.Fatal("service started before archived database was restored")
				}
			}
			if install.Route("restore") != "host" {
				t.Fatal("restore routed into a container that it must stop")
			}
			var out bytes.Buffer
			if err := runRestoreWithServiceFactory(ctx, []string{archive.Path, "--password", "--yes"}, strings.NewReader("synthetic-cli-password\n"), &out, h, func(*config.Config) *backup.Service { return &backup.Service{Cfg: cfg, ConfigPath: cfgPath} }); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "synthetic-cli-password") {
				t.Fatal("secret in restore output")
			}
			if !strings.Contains(out.String(), "archived") {
				t.Fatal("validated node identity missing from review")
			}
			joined := strings.Join(h.commands, "\n")
			if strings.Contains(joined, "docker exec") {
				t.Fatal("offline restore depended on a running container")
			}
			if mode == install.ModeDocker && !strings.Contains(joined, " down") {
				t.Fatal("Docker service not stopped")
			}
			if mode == install.ModeNative && !strings.Contains(joined, "systemctl stop") {
				t.Fatal("native service not stopped")
			}
		})
	}
}
