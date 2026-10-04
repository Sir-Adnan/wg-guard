package install

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

var errReadinessRedirect = errors.New("readiness probe redirected")

// ProbeReadiness proves data/runtime readiness independently of TLS trust. URLs
// are derived only from the local installed plan, never an external redirect.
// New ACME sidecars answer loopback readiness without contacting a CA; retained
// older artifacts are checked on their actual TLS listener with the recorded SNI.
func ProbeReadiness(ctx context.Context, p Plan) error {
	url, skipVerify, err := p.HealthProbeURL()
	if err != nil {
		return err
	}
	url = strings.TrimSuffix(url, "/healthz") + "/readyz"
	err = probeReadinessURL(ctx, url, skipVerify, p.Domain)
	if errors.Is(err, errReadinessRedirect) && p.TLSMode == config.TLSModeACME {
		return probeReadinessURL(ctx, "https://127.0.0.1:"+strconv.Itoa(p.PanelPort)+"/readyz", true, p.Domain)
	}
	return err
}

func probeReadinessURL(ctx context.Context, url string, skipVerify bool, serverName string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if req.URL.Scheme == "https" && serverName != "" {
		req.Host = serverName
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12, ServerName: serverName,
		InsecureSkipVerify: skipVerify, //nolint:gosec // fixed loopback readiness only; trust is proved separately
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return errReadinessRedirect
	}
	if response.StatusCode != http.StatusOK {
		return terminalError("install.error.readiness", response.StatusCode)
	}
	var state struct {
		Status string `json:"status"`
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	if err != nil {
		return err
	}
	if len(body) > 1024 {
		return terminalError("install.error.readiness_invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || state.Status != "ready" || decoder.Decode(new(any)) != io.EOF {
		return terminalError("install.error.readiness_invalid")
	}
	return nil
}
