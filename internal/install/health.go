package install

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Sir-Adnan/wg-guard/internal/config"
)

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
	return fmt.Errorf("healthz answered %d", resp.StatusCode)
}

// HealthProbeLabel is the probe URL for display.
func (p Plan) HealthProbeLabel() string {
	url, _, _ := p.HealthProbeURL()
	return url
}

// LoadState reads install-state.json. A missing file returns (nil, nil) —
// "not installed by the installer" — which is distinct from a corrupt file.
func LoadState(h Host) (*State, error) {
	data, err := h.ReadFile(StatePath)
	if err != nil {
		return nil, nil //nolint:nilerr // absent state = not installed
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse %s: %w (remove it if this host was reinstalled)", StatePath, err)
	}
	return &st, nil
}

// readBootConfig parses the live boot config (update paths use its TLS
// posture for the health probe).
func ReadBootConfig(h Host, path string) (*config.Config, error) {
	data, err := h.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read boot config: %w", err)
	}
	cfg := config.Defaults()
	if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse boot config: %w", err)
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
