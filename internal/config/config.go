// Package config loads the boot configuration: the small, restart-required
// layer (paths, listener, TLS mode) stored as TOML. Everything tunable at
// runtime lives in the settings registry instead. See
// docs/operations/deployment.md for the documented modes.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// TLSMode selects how the panel terminates transport security.
type TLSMode string

const (
	TLSModeACME   TLSMode = "acme"   // built-in autocert, HTTP-01 on port 80
	TLSModeManual TLSMode = "manual" // administrator-provided cert/key
	TLSModeProxy  TLSMode = "proxy"  // HTTP behind an external reverse proxy
	TLSModeDev    TLSMode = "dev"    // loopback-only plaintext, loud warnings
)

func (m TLSMode) Valid() bool {
	switch m {
	case TLSModeACME, TLSModeManual, TLSModeProxy, TLSModeDev:
		return true
	}
	return false
}

// Config is the boot (restart-required) configuration.
type Config struct {
	DataDir       string `toml:"data_dir"`
	DatabasePath  string `toml:"database_path"`
	MasterKeyFile string `toml:"master_key_file"`
	HTTPListen    string `toml:"http_listen"`

	TLS     TLSConfig     `toml:"tls"`
	Log     LogConfig     `toml:"log"`
	Metrics MetricsConfig `toml:"metrics"`
}

// MetricsConfig gates the Prometheus-style /metrics endpoint. It is off by
// default: the endpoint exposes node topology (interfaces, request rates)
// and belongs behind the operator's own monitoring decision, not on by
// default on a public listener (docs/operations/security.md).
type MetricsConfig struct {
	Enabled bool `toml:"enabled"`
}

type TLSConfig struct {
	Mode     TLSMode `toml:"mode"`
	Domain   string  `toml:"domain"`    // required for acme
	CertFile string  `toml:"cert_file"` // required for manual
	KeyFile  string  `toml:"key_file"`  // required for manual

	// ACMEHTTPPort is the dedicated plain-HTTP listener that serves the ACME
	// HTTP-01 challenge and redirects visitors to the TLS listener (ADR-0011:
	// port 80 must stay reachable for issuance/renewal). 0 = 80.
	ACMEHTTPPort int `toml:"acme_http_port"`
}

type LogConfig struct {
	Level  string `toml:"level"`  // debug|info|warn|error
	Format string `toml:"format"` // text|json
}

// Defaults returns the built-in defaults (paths follow the documented layout
// /etc/wg-guard + /var/lib/wg-guard; callers may replace DataDir).
func Defaults() *Config {
	return &Config{
		DataDir:    "/var/lib/wg-guard",
		HTTPListen: "127.0.0.1:8080",
		TLS:        TLSConfig{Mode: TLSModeDev},
		Log:        LogConfig{Level: "info", Format: "text"},
	}
}

// Complete derives dependent paths from DataDir when unset.
func (c *Config) Complete() {
	if c.DatabasePath == "" {
		c.DatabasePath = filepath.Join(c.DataDir, "wg-guard.db")
	}
	if c.MasterKeyFile == "" {
		c.MasterKeyFile = filepath.Join(c.DataDir, "master.key")
	}
	if c.TLS.ACMEHTTPPort == 0 && c.TLS.Mode == TLSModeACME {
		c.TLS.ACMEHTTPPort = defaultACMEHTTPPort
	}
}

// defaultACMEHTTPPort is the ACME HTTP-01 challenge port (ADR-0011).
const defaultACMEHTTPPort = 80

// Load reads and validates boot config from file (optional) with environment
// overrides. Env vars win: WGG_DATA_DIR, WGG_DATABASE_PATH, WGG_MASTER_KEY_FILE,
// WGG_HTTP_LISTEN, WGG_TLS_MODE, WGG_TLS_DOMAIN, WGG_TLS_CERT_FILE,
// WGG_TLS_KEY_FILE, WGG_LOG_LEVEL.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, cfg); err != nil {
				return nil, domain.Wrap(err, domain.CodeConfigInvalid, "parse boot config %s", path)
			}
		} else if !os.IsNotExist(err) {
			return nil, domain.Wrap(err, domain.CodeConfigInvalid, "stat boot config %s", path)
		}
	}
	applyEnv(cfg, os.Getenv)
	cfg.Complete()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config, get func(string) string) {
	setString(&cfg.DataDir, get("WGG_DATA_DIR"))
	setString(&cfg.DatabasePath, get("WGG_DATABASE_PATH"))
	setString(&cfg.MasterKeyFile, get("WGG_MASTER_KEY_FILE"))
	setString(&cfg.HTTPListen, get("WGG_HTTP_LISTEN"))
	setString((*string)(&cfg.TLS.Mode), get("WGG_TLS_MODE"))
	setString(&cfg.TLS.Domain, get("WGG_TLS_DOMAIN"))
	setString(&cfg.TLS.CertFile, get("WGG_TLS_CERT_FILE"))
	setString(&cfg.TLS.KeyFile, get("WGG_TLS_KEY_FILE"))
	if v := get("WGG_TLS_ACME_HTTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.TLS.ACMEHTTPPort = p
		}
	}
	setString(&cfg.Log.Level, get("WGG_LOG_LEVEL"))
	if v := get("WGG_METRICS_ENABLED"); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			cfg.Metrics.Enabled = true
		case "0", "false", "no", "off":
			cfg.Metrics.Enabled = false
		}
	}
}

func setString(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

// Validate enforces the security posture: the installer/panel must never
// silently expose plaintext management beyond loopback (security.md).
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return domain.E(domain.CodeConfigInvalid, "data_dir must not be empty")
	}
	if c.HTTPListen == "" {
		return domain.E(domain.CodeConfigInvalid, "http_listen must not be empty")
	}
	if _, _, err := net.SplitHostPort(c.HTTPListen); err != nil {
		return domain.Wrap(err, domain.CodeConfigInvalid, "http_listen %q", c.HTTPListen)
	}
	if !c.TLS.Mode.Valid() {
		return domain.E(domain.CodeConfigInvalid, "tls.mode %q is not one of acme|manual|proxy|dev", c.TLS.Mode)
	}
	switch c.TLS.Mode {
	case TLSModeACME:
		d := strings.TrimSpace(c.TLS.Domain)
		if d == "" {
			return domain.E(domain.CodeConfigInvalid, "tls.domain is required for tls.mode=acme")
		}
		// tls.domain is a bare hostname: a scheme/port/path would end up in
		// certificate requests and redirect URLs.
		if strings.ContainsAny(d, "/:") {
			return domain.E(domain.CodeConfigInvalid, "tls.domain %q must be a bare hostname (no scheme, port or path)", c.TLS.Domain)
		}
		if c.TLS.ACMEHTTPPort < 0 || c.TLS.ACMEHTTPPort > 65535 {
			return domain.E(domain.CodeConfigInvalid, "tls.acme_http_port %d is out of range 1-65535", c.TLS.ACMEHTTPPort)
		}
	case TLSModeManual:
		if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
			return domain.E(domain.CodeConfigInvalid, "tls.cert_file and tls.key_file are required for tls.mode=manual")
		}
	case TLSModeDev:
		if !isLoopbackListen(c.HTTPListen) {
			return domain.E(domain.CodeConfigInvalid,
				"tls.mode=dev only serves loopback plaintext; http_listen %q is not loopback (use acme/manual/proxy for real deployments)", c.HTTPListen)
		}
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return domain.E(domain.CodeConfigInvalid, "log.level %q is not one of debug|info|warn|error", c.Log.Level)
	}
	switch strings.ToLower(c.Log.Format) {
	case "text", "json":
	default:
		return domain.E(domain.CodeConfigInvalid, "log.format %q is not one of text|json", c.Log.Format)
	}
	return nil
}

func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Save writes the config as TOML (installer use), creating parent dirs.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("config: create %s: %w", path, err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(c)
}
