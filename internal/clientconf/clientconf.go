// Package clientconf renders AmneziaWG client configurations and QR codes.
// It is shared by the REST API and the admin panel (one renderer, two
// surfaces — no duplicated business logic): a config is a pure function of
// current settings and interface state, so endpoint/DNS/MTU changes
// propagate to every new download immediately.
package clientconf

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"strings"

	"rsc.io/qr"

	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

// Renderer builds client configs from the device, its interface and node
// settings. Services are shared instances — nothing here caches state.
type Renderer struct {
	Devices  *device.Service
	Ifaces   *iface.Service
	Settings *settings.Registry
}

// Render builds the AWG client configuration for one device.
//
// The private key is decrypted for the response only and never logged.
//
// Obfuscation parameters follow the pinned client↔server parity rule
// (docs/integrations/amneziawg.md): S1/S2/H1–H4 must match the server;
// Jc/Jmin/Jmax and I1–I5 are client-side (the server's values are offered as
// defaults; I1–I5 only when configured — iOS rejects I1–I5 configs, #115).
func (r *Renderer) Render(ctx context.Context, deviceID string) (string, error) {
	d, err := r.Devices.Get(ctx, deviceID)
	if err != nil {
		return "", err
	}
	ifc, err := r.Ifaces.Get(ctx, d.InterfaceID)
	if err != nil {
		return "", err
	}
	priv, err := r.Devices.PrivateKey(ctx, d)
	if err != nil {
		return "", err
	}
	psk, err := r.Devices.PresharedKey(ctx, d)
	if err != nil {
		return "", err
	}

	endpoint := ifc.EndpointOverride
	if endpoint == "" {
		endpoint, _ = r.Settings.GetString(ctx, "node.endpoint")
	}
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("[Interface]\nPrivateKey = %s\n", priv)
	w("Address = %s\n", d.IPv4)
	if dns, err := r.Settings.GetStringList(ctx, "network.dns_servers"); err == nil && len(dns) > 0 {
		w("DNS = %s\n", strings.Join(dns, ", "))
	}
	if mtu, err := r.Settings.GetInt(ctx, "network.mtu"); err == nil && mtu > 0 {
		w("MTU = %d\n", mtu)
	}
	b.WriteString("\n[Peer]\n")
	w("PublicKey = %s\n", ifc.PublicKey)
	if psk != "" {
		w("PresharedKey = %s\n", psk)
	}
	if allowed, err := r.Settings.GetString(ctx, "network.client_allowed_ips"); err == nil && allowed != "" {
		w("AllowedIPs = %s\n", strings.ReplaceAll(allowed, ",", ", "))
	}
	if endpoint != "" {
		w("Endpoint = %s\n", WithPort(endpoint, ifc.ListenPort))
	}
	if ka, err := r.Settings.GetInt(ctx, "network.client_keepalive_seconds"); err == nil && ka > 0 {
		w("PersistentKeepalive = %d\n", ka)
	}
	o := ifc.Obfuscation
	if o.Enabled {
		w("Jc = %d\nJmin = %d\nJmax = %d\n", o.Jc, o.Jmin, o.Jmax)
		w("S1 = %d\nS2 = %d\n", o.S1, o.S2)
		w("H1 = %d\nH2 = %d\nH3 = %d\nH4 = %d\n", o.H1, o.H2, o.H3, o.H4)
		for i, v := range []string{o.I1, o.I2, o.I3, o.I4, o.I5} {
			if v != "" {
				w("I%d = %s\n", i+1, v)
			}
		}
	}
	return b.String(), nil
}

// WithPort appends the interface listen port unless the endpoint already
// carries one. Bare IPv6 literals are bracketed (they would otherwise read
// as host:port garbage).
func WithPort(endpoint string, port int) string {
	if strings.Contains(endpoint, "]") { // [v6] or [v6]:port
		if strings.HasSuffix(endpoint, "]") {
			return fmt.Sprintf("%s:%d", endpoint, port)
		}
		return endpoint
	}
	if strings.Count(endpoint, ":") == 1 { // host:port already
		return endpoint
	}
	if strings.Contains(endpoint, ":") { // bare IPv6 literal
		return fmt.Sprintf("[%s]:%d", endpoint, port)
	}
	return fmt.Sprintf("%s:%d", endpoint, port)
}

// QR renders text as a PNG QR code. rsc.io/qr is the only QR option with
// zero transitive dependencies (justified in THIRD_PARTY.md: ~100 KB binary
// impact, BSD license, no network or cgo). Client configs are small
// (≤ ~2 KB) but hard-bounded here: oversized payloads are a client error,
// not a panic in the encoder.
func QR(text string) ([]byte, error) {
	const maxQRBytes = 2600 // QR version 40 byte-mode capacity is 2953
	if len(text) > maxQRBytes {
		return nil, domain.E(domain.CodeInvalidRequest, "device configuration too large for a QR code")
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, fmt.Errorf("qr encode: %w", err)
	}
	img := code.Image() // 6 px module border-included scale
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("qr png: %w", err)
	}
	return buf.Bytes(), nil
}
