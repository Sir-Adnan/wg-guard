package install

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Sir-Adnan/wg-guard/internal/config"
)

// WaitCertificate proves a trusted certificate for the requested name/IP on
// the local TLS listener. This may trigger ACME issuance or reuse its cache;
// it never calls either case newly issued without separate issuance evidence.
func WaitCertificate(ctx context.Context, p Plan, within time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	for {
		if err := probeCertificate(ctx, p, nil); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			if p.TLSMode == config.TLSModeManual {
				return terminalError("install.error.manual_pending", ctx.Err())
			}
			return terminalError("install.error.health.1", p.ACMEHTTPPort, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func probeCertificate(ctx context.Context, p Plan, roots *x509.CertPool) error {
	name := p.VPNEndpoint()
	if name == "" {
		return terminalError("install.error.health.2")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	dialer := tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: name, RootCAs: roots}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p.PanelPort)))
	if err != nil {
		return err
	}
	return conn.Close()
}

// CheckInstalledTLS retries readiness without reinstalling or changing the
// running service. Pending state is retained on failure for later recovery.
func CheckInstalledTLS(ctx context.Context, h Host, within time.Duration) error {
	unlock, err := h.LockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := LoadState(h)
	if err != nil {
		return err
	}
	if st == nil {
		return terminalError("install.error.health.3")
	}
	cfg, err := ReadBootConfig(h, st.ConfigPath)
	if err != nil {
		return err
	}
	p := Defaults()
	p.TLSMode = cfg.TLS.Mode
	p.Domain = cfg.TLS.Domain
	p.PublicIP = st.PublicIP
	p.PanelPort = portOf(cfg.HTTPListen)
	p.ACMEHTTPPort = cfg.TLS.ACMEHTTPPort
	if p.TLSMode != config.TLSModeACME && p.TLSMode != config.TLSModeManual {
		return terminalError("install.error.health.4")
	}
	if err := WaitCertificate(ctx, p, within); err != nil {
		return err
	}
	st.TLSReadiness = "verified"
	if err := saveState(h, st); err != nil {
		return err
	}
	j, err := LoadJournal(h)
	if err != nil {
		return err
	}
	if j != nil && j.Operation == "install" && j.After != nil && j.After.TLSReadiness == "pending" {
		j.After = st
		st.Recovery = ""
		if err := saveState(h, st); err != nil {
			return err
		}
		return j.save(h, "complete")
	}
	return nil
}

func saveState(h Host, st *State) error {
	return writeJSON(h, StatePath, st)
}

// probeHealth performs one health probe. 200 is healthy; 302 is healthy on
// the ACME sidecar (it redirects everything to the TLS listener — reaching
// it proves the node is serving).
func ProbeHealth(ctx context.Context, url string, skipVerify bool) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	if skipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit operator choice for manual certs
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
		return nil
	}
	return terminalError("install.error.health.5", resp.StatusCode)
}

// HealthProbeLabel is the probe URL for display.
func (p Plan) HealthProbeLabel() string {
	url, _, _ := p.HealthProbeURL()
	return url
}

// LoadState reads install-state.json. A missing file returns (nil, nil) —
// "not installed by the installer" — which is distinct from a corrupt file.
func LoadState(h Host) (*State, error) {
	if _, ok := h.(realHost); ok {
		if err := safeHostPath(StatePath); err != nil {
			return nil, err
		}
	}
	data, err := readRecord(h, StatePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var st State
	if len(data) > 256<<10 {
		return nil, terminalError("install.error.state")
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, terminalError("install.error.health.6", StatePath, err)
	}
	if err := validateState(&st); err != nil {
		return nil, err
	}
	if _, ok := h.(realHost); ok {
		for _, p := range append([]string{st.ConfigPath, st.DataDir, st.BinPath, st.ComposePath, st.UnitPath, ArtifactDir}, st.ExtraFiles...) {
			if p != "" {
				if err := safeHostPath(p); err != nil {
					return nil, err
				}
			}
		}
		for _, a := range []*Artifact{st.Current, st.Previous} {
			if a != nil {
				for _, p := range []string{a.Binary, a.Compose} {
					if p != "" {
						if err := safeHostPath(p); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	return &st, nil
}

// readBootConfig parses the live boot config (update paths use its TLS
// posture for the health probe).
func ReadBootConfig(h Host, path string) (*config.Config, error) {
	data, err := h.ReadFile(path)
	if err != nil {
		return nil, terminalError("install.error.health.7", err)
	}
	cfg := config.Defaults()
	if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(cfg); err != nil {
		return nil, terminalError("install.error.health.8", err)
	}
	cfg.Complete()
	return cfg, nil
}

// portOf extracts the port from a host:port listen address (0 on parse
// failure — the probe then falls back to the documented defaults).
func portOf(listen string) int {
	_, p, err := net.SplitHostPort(listen)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 0
	}
	return n
}
