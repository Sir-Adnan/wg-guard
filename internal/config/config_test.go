package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsValidate(t *testing.T) {
	cfg := Defaults()
	cfg.Complete()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	if cfg.DatabasePath == "" || cfg.MasterKeyFile == "" {
		t.Fatal("Complete must derive dependent paths")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wg-guard.toml")
	body := `
data_dir = "C:/wg-guard-data"
http_listen = "0.0.0.0:8080"

[tls]
mode = "acme"
domain = "sub.example.com"

[log]
level = "debug"
format = "json"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DataDir != "C:/wg-guard-data" {
		t.Fatalf("data_dir = %q", cfg.DataDir)
	}
	if cfg.TLS.Mode != TLSModeACME || cfg.TLS.Domain != "sub.example.com" {
		t.Fatalf("tls = %+v", cfg.TLS)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Fatalf("log = %+v", cfg.Log)
	}
	if cfg.DatabasePath != filepath.Join("C:/wg-guard-data", "wg-guard.db") {
		t.Fatalf("database_path = %q", cfg.DatabasePath)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("missing config file must not error: %v", err)
	}
	if cfg.HTTPListen != "127.0.0.1:8080" {
		t.Fatalf("unexpected default listen %q", cfg.HTTPListen)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("WGG_DATA_DIR", t.TempDir())
	t.Setenv("WGG_HTTP_LISTEN", "127.0.0.1:9999")
	t.Setenv("WGG_TLS_MODE", "proxy")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HTTPListen != "127.0.0.1:9999" || cfg.TLS.Mode != TLSModeProxy {
		t.Fatalf("env overrides not applied: %+v", cfg)
	}
}

func TestValidateSecurityPosture(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"dev mode on public interface", func(c *Config) {
			c.HTTPListen = "0.0.0.0:8080"
			c.TLS.Mode = TLSModeDev
		}, "loopback"},
		{"acme without domain", func(c *Config) {
			c.TLS.Mode = TLSModeACME
			c.TLS.Domain = ""
		}, "tls.domain"},
		{"manual without cert", func(c *Config) {
			c.TLS.Mode = TLSModeManual
		}, "cert_file"},
		{"unknown tls mode", func(c *Config) {
			c.TLS.Mode = "yolo"
		}, "tls.mode"},
		{"bad listen", func(c *Config) {
			c.HTTPListen = "no-port"
		}, "http_listen"},
		{"bad log level", func(c *Config) {
			c.Log.Level = "verbose"
		}, "log.level"},
		{"proxy on any interface is allowed", func(c *Config) {
			c.HTTPListen = "0.0.0.0:8080"
			c.TLS.Mode = TLSModeProxy
		}, ""},
		{"manual with cert+key allowed", func(c *Config) {
			c.TLS.Mode = TLSModeManual
			c.TLS.CertFile = "/x/c.pem"
			c.TLS.KeyFile = "/x/k.pem"
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.Complete()
			tc.mutate(cfg)
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
			if !strings.Contains(err.Error(), "(CONFIG_INVALID)") {
				t.Fatalf("expected CONFIG_INVALID code, got %v", err)
			}
		})
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "wg-guard.toml")
	cfg := Defaults()
	cfg.TLS = TLSConfig{Mode: TLSModeACME, Domain: "panel.example.org"}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TLS.Domain != "panel.example.org" || got.TLS.Mode != TLSModeACME {
		t.Fatalf("round trip mismatch: %+v", got.TLS)
	}
}
