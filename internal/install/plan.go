// Package install implements the deployment layer: the interactive
// `wg-guard install` wizard (Docker default, native secondary — ADR-0006),
// `update` with a pre-upgrade backup and health-checked rollback, and
// `uninstall --dry-run` that removes only WG-Guard-owned artifacts. Pure
// renderers and the plan type live here; host mutations go through the Host
// seam so the whole flow is unit-testable without a Linux box.
package install

import (
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Mode selects the deployment shape (ADR-0006): Docker is the default, the
// native systemd path is fully supported and shares the same data layout.
type Mode string

const (
	ModeDocker Mode = "docker"
	ModeNative Mode = "native"
)

func (m Mode) Valid() bool { return m == ModeDocker || m == ModeNative }

// Fixed layout (docs/operations/deployment.md): identical in both modes so
// backups, restore and mode switches are layout-independent.
const (
	EtcDir     = "/etc/wg-guard"
	DataDir    = "/var/lib/wg-guard"
	ConfigPath = EtcDir + "/wg-guard.toml"
	StatePath  = EtcDir + "/install-state.json"
	ComposePth = EtcDir + "/compose.yaml"
	UnitPath   = "/etc/systemd/system/wg-guard.service"
	BinPath    = "/usr/local/bin/wg-guard"
	Container  = "wg-guard"
)

// DefaultImage is the official image reference. The Phase 12 release pipeline
// publishes versioned tags; until then :latest tracks releases and the
// installer accepts --image for local/registry overrides.
const DefaultImage = "wgguard/wg-guard:latest"

// Plan is one resolved installation. Fields with zero values are filled by
// Resolve from flags + prompts before use.
type Plan struct {
	Mode Mode

	// TLS surface. Domain non-empty + TLSMode acme is the flagship path;
	// manual needs cert/key files; proxy/dev are explicit operator choices.
	Domain   string
	TLSMode  config.TLSMode
	CertFile string
	KeyFile  string

	// PanelPort is the TLS port (acme/manual) or HTTP port (proxy/dev).
	PanelPort         int
	PanelPortExplicit bool // CLI/prompt choice, including 8080, survives TLS defaults
	TLSModeExplicit   bool
	PublicIP          string // VPN endpoint and SSH destination, independent of panel bind address
	// ACMEHTTPPort is the HTTP-01 challenge port (acme only, default 80).
	ACMEHTTPPort int

	// Image is the container reference (docker mode).
	Image string

	// Runtime settings collected by the wizard (all optional; zero values
	// keep the registry defaults — nothing here is seeded unless the
	// administrator actually chose it). They are applied through the
	// installed CLI BEFORE the service first boots: the settings registry
	// caches values in memory, so post-boot writes would be invisible until
	// a restart. TelegramToken is a secret: it is transported via stdin only
	// and never appears in argv, output or the state file.
	PortMin, PortMax int    // AWG listen-port allocation range
	VPNSubnet        string // pool offered to the first interface (awg0)
	MTU              int    // client + interface MTU
	ClientDNS        string // comma-separated client DNS resolvers
	TelegramToken    string
	TelegramChat     string
	TelegramTime     string // daily backup time "HH:MM" UTC; "" = no schedule

	// EtcDir/DataDir are the fixed layout paths (fields for tests).
	EtcDir  string
	DataDir string
}

// Defaults returns the plan defaults: Docker, no domain (dev TLS on
// loopback), panel 8080. Resolve upgrades dev→acme when a domain is set.
func Defaults() Plan {
	return Plan{
		Mode:         ModeDocker,
		TLSMode:      config.TLSModeDev,
		PanelPort:    8080,
		ACMEHTTPPort: 80,
		Image:        DefaultImage,
		EtcDir:       EtcDir,
		DataDir:      DataDir,
	}
}

// Resolve validates and derives: a domain without an explicit TLS mode means
// ACME (the flagship path); acme/manual get a public listener, proxy/dev a
// loopback one (config.Validate enforces the same posture at boot).
func (p Plan) Resolve() (Plan, error) {
	if !p.Mode.Valid() {
		return p, terminalError("install.error.plan.1", p.Mode)
	}
	p.Domain = strings.ToLower(strings.TrimSpace(p.Domain))
	if p.Domain != "" && !validHostname(p.Domain) {
		return p, terminalError("install.error.plan.2")
	}
	if p.PublicIP != "" && !validPublicIP(p.PublicIP) {
		return p, terminalError("install.error.plan.3")
	}
	if p.TelegramChat != "" && !validChatID(p.TelegramChat) {
		return p, terminalError("install.error.plan.4")
	}
	if p.Domain != "" && p.TLSMode == config.TLSModeDev && !p.TLSModeExplicit {
		p.TLSMode = config.TLSModeACME
	}
	switch p.TLSMode {
	case config.TLSModeACME:
		if p.Domain == "" {
			return p, terminalError("install.error.plan.5")
		}
		if net.ParseIP(p.Domain) != nil {
			return p, terminalError("install.error.plan.6", p.Domain)
		}
		if p.PanelPort == 8080 && !p.PanelPortExplicit {
			p.PanelPort = 443 // a domain install defaults to the TLS port
		}
		if p.ACMEHTTPPort == 0 {
			p.ACMEHTTPPort = 80
		}
	case config.TLSModeManual:
		if p.CertFile == "" || p.KeyFile == "" {
			return p, terminalError("install.error.plan.7")
		}
		for _, file := range []string{p.CertFile, p.KeyFile} {
			if !path.IsAbs(file) || strings.ContainsAny(file, "\r\n\t :#\"'") {
				return p, terminalError("install.error.manual_path")
			}
		}
		if p.PanelPort == 8080 && !p.PanelPortExplicit {
			p.PanelPort = 443
		}
	case config.TLSModeProxy, config.TLSModeDev:
		// keep ports as given
	default:
		return p, terminalError("install.error.plan.8", p.TLSMode)
	}
	if p.PanelPort < 1 || p.PanelPort > 65535 {
		return p, terminalError("install.error.plan.9", p.PanelPort)
	}
	if p.TLSMode == config.TLSModeACME && (p.ACMEHTTPPort < 1 || p.ACMEHTTPPort > 65535) {
		return p, terminalError("install.error.plan.10", p.ACMEHTTPPort)
	}
	if p.TLSMode == config.TLSModeACME && p.PanelPort == p.ACMEHTTPPort {
		return p, terminalError("install.error.plan.11")
	}
	if p.Mode == ModeDocker && strings.TrimSpace(p.Image) == "" {
		p.Image = DefaultImage
	}
	return p, nil
}

// HTTPListen renders the boot-config listener for the plan: public for
// TLS-terminating modes, loopback behind a proxy / for local development.
func (p Plan) HTTPListen() string {
	host := "127.0.0.1"
	if p.TLSMode == config.TLSModeACME || p.TLSMode == config.TLSModeManual {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(p.PanelPort))
}

// BootConfig renders the wg-guard.toml the node will run with.
func (p Plan) BootConfig() *config.Config {
	cfg := config.Defaults()
	cfg.DataDir = p.DataDir
	cfg.HTTPListen = p.HTTPListen()
	cfg.TLS.Mode = p.TLSMode
	cfg.TLS.Domain = p.Domain
	switch p.TLSMode {
	case config.TLSModeACME:
		cfg.TLS.ACMEHTTPPort = p.ACMEHTTPPort
	case config.TLSModeManual:
		cfg.TLS.CertFile = p.CertFile
		cfg.TLS.KeyFile = p.KeyFile
	}
	return cfg
}

// PanelURL is the scheme://host:port the operator opens (the summary line).
func (p Plan) PanelURL() string {
	scheme := "http"
	if p.TLSMode == config.TLSModeACME || p.TLSMode == config.TLSModeManual {
		scheme = "https"
	}
	host := p.Domain
	if scheme == "http" {
		host = "127.0.0.1"
	}
	if host == "" {
		host = p.PublicIP
		if host == "" {
			host = "<server-ip>"
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := ""
	if !(scheme == "https" && p.PanelPort == 443) && !(scheme == "http" && p.PanelPort == 80) {
		port = ":" + strconv.Itoa(p.PanelPort)
	}
	return scheme + "://" + host + port
}

func validHostname(s string) bool {
	if len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// VPNEndpoint never derives an endpoint from the loopback management listener.
func (p Plan) VPNEndpoint() string {
	if p.Domain != "" {
		return p.Domain
	}
	return p.PublicIP
}

func (p Plan) SSHTunnel() string {
	host := p.VPNEndpoint()
	if host == "" {
		host = "<server-ip>"
	}
	return fmt.Sprintf("ssh -N -L %d:127.0.0.1:%d root@%s", p.PanelPort, p.PanelPort, host)
}

// HealthProbeURL is where the health check polls after (re)start, per mode:
// the ACME sidecar answers plain HTTP (the TLS listener defers certificate
// issuance to the first real request, which must not be triggered by a probe);
// manual certs are probed with a skip-verify TLS client; proxy/dev plain.
func (p Plan) HealthProbeURL() (url string, skipVerify bool, err error) {
	switch p.TLSMode {
	case config.TLSModeACME:
		return fmt.Sprintf("http://127.0.0.1:%d/healthz", p.ACMEHTTPPort), false, nil
	case config.TLSModeManual:
		return fmt.Sprintf("https://127.0.0.1:%d/healthz", p.PanelPort), true, nil
	case config.TLSModeProxy, config.TLSModeDev:
		return fmt.Sprintf("http://127.0.0.1:%d/healthz", p.PanelPort), false, nil
	}
	return "", false, domain.E(domain.CodeConfigInvalid, "unknown tls mode %q", p.TLSMode)
}

// State is the install record (install-state.json): the uninstall/update
// contract. It records everything WG-Guard owns so removal touches nothing
// else, including packages the installer itself installed.
type State struct {
	Platform          PlatformReport `json:"platform,omitempty"`
	Core              CoreReport     `json:"core,omitempty"`
	TLSReadiness      string         `json:"tls_readiness,omitempty"`
	PublicIP          string         `json:"public_ip,omitempty"`
	RepositoryChanges []string       `json:"repository_changes,omitempty"` // retained on uninstall; shared apt sources
	Schema            int            `json:"schema"`
	CreatedAt         string         `json:"created_at"`
	Version           string         `json:"wg_guard_version"`
	Mode              Mode           `json:"mode"`
	ConfigPath        string         `json:"config_path"`
	DataDir           string         `json:"data_dir"`
	Image             string         `json:"image,omitempty"`
	ComposePath       string         `json:"compose_path,omitempty"`
	BinPath           string         `json:"binary_path,omitempty"`
	UnitPath          string         `json:"unit_path,omitempty"`
	ExtraFiles        []string       `json:"extra_files,omitempty"`
	PackagesInstalled []string       `json:"packages_installed,omitempty"`
}

// StateSchema is the current install-state schema version.
const StateSchema = 1
