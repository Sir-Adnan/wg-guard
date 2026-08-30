package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

func (s *Server) handleDeviceListForUser(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Users.Get(r.Context(), pathID(r, "id")); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	devs, err := s.Devices.ListForUser(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	items := make([]deviceDTO, 0, len(devs))
	for _, d := range devs {
		items = append(items, toDeviceDTO(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	userID := pathID(r, "id")
	var req struct {
		Name         string `json:"name"`
		InterfaceID  string `json:"interface_id"`
		PresharedKey bool   `json:"preshared_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	keys, err := s.generateKeys(r, req.PresharedKey)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	d, err := s.Devices.Create(r.Context(), userID, req.Name, *keys, req.InterfaceID)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "device.created", d.ID, map[string]any{"user_id": userID, "name": d.Name})
	s.reconcile(r) // the new peer must reach the backend now
	writeJSON(w, http.StatusCreated, toDeviceDTO(d))
}

// generateKeys mints a device keypair and encrypts the private key with the
// master key ring (plaintext keys never touch logs or responses).
func (s *Server) generateKeys(r *http.Request, withPSK bool) (*device.KeyMaterial, error) {
	kp, err := tunnel.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	privEnc, err := s.Ring.Encrypt([]byte(kp.Private))
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}
	keys := &device.KeyMaterial{PublicKey: kp.Public, PrivateKeyEnc: privEnc}
	if withPSK {
		psk, err := tunnel.GeneratePresharedKey()
		if err != nil {
			return nil, err
		}
		pskEnc, err := s.Ring.Encrypt([]byte(psk))
		if err != nil {
			return nil, fmt.Errorf("encrypt preshared key: %w", err)
		}
		keys.PresharedEnc = pskEnc
	}
	return keys, nil
}

func (s *Server) handleDeviceGet(w http.ResponseWriter, r *http.Request) {
	d, err := s.Devices.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDeviceDTO(d))
}

func (s *Server) handleDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := s.Devices.Rename(r.Context(), id, req.Name)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "device.updated", id, map[string]any{"name": updated.Name})
	writeJSON(w, http.StatusOK, toDeviceDTO(updated))
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if err := s.Devices.Delete(r.Context(), id); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "device.deleted", id, nil)
	s.reconcile(r)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleDeviceEnable(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if err := s.Devices.SetEnabled(r.Context(), id, true); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "device.enabled", id, nil)
	s.reconcile(r)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "id": id})
}

func (s *Server) handleDeviceDisable(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if err := s.Devices.SetEnabled(r.Context(), id, false); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "device.disabled", id, nil)
	s.reconcile(r)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "id": id})
}

func (s *Server) handleDeviceRegenerate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var req struct {
		PresharedKey bool `json:"preshared_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	keys, err := s.generateKeys(r, req.PresharedKey)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	if err := s.Devices.Regenerate(r.Context(), id, *keys); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "device.regenerated", id, nil)
	s.reconcile(r) // stale key removal + new peer add (reconcile matches by device IP)
	d, err := s.Devices.Get(r.Context(), id)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDeviceDTO(d))
}

// handleDeviceConfig renders the client configuration (text/plain,
// no-store — private key material).
func (s *Server) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	text, err := s.renderClientConfig(r, pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="wg-guard.conf"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(text))
}

// handleDeviceQR renders the client configuration as a PNG QR code
// (no-store).
func (s *Server) handleDeviceQR(w http.ResponseWriter, r *http.Request) {
	text, err := s.renderClientConfig(r, pathID(r, "id"))
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	png, err := qrPNG(text)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// renderClientConfig builds the AWG client configuration for one device —
// a pure function of current settings and interface state, so endpoint/DNS/
// MTU changes propagate to every new download immediately (api.md). The
// private key is decrypted for the response only and never logged.
//
// Obfuscation parameters follow the pinned client↔server parity rule
// (docs/integrations/amneziawg.md): S1/S2/H1–H4 must match the server;
// Jc/Jmin/Jmax and I1–I5 are client-side (the server's values are offered as
// defaults; I1–I5 only when configured — iOS rejects I1–I5 configs, #115).
func (s *Server) renderClientConfig(r *http.Request, deviceID string) (string, error) {
	ctx := r.Context()
	d, err := s.Devices.Get(ctx, deviceID)
	if err != nil {
		return "", err
	}
	ifc, err := s.Ifaces.Get(ctx, d.InterfaceID)
	if err != nil {
		return "", err
	}
	priv, err := s.Devices.PrivateKey(r.Context(), d)
	if err != nil {
		return "", err
	}
	psk, err := s.Devices.PresharedKey(r.Context(), d)
	if err != nil {
		return "", err
	}

	endpoint := ifc.EndpointOverride
	if endpoint == "" {
		endpoint, _ = s.Settings.GetString(ctx, "node.endpoint")
	}
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("[Interface]\nPrivateKey = %s\n", priv)
	w("Address = %s\n", d.IPv4)
	if dns, err := s.Settings.GetStringList(ctx, "network.dns_servers"); err == nil && len(dns) > 0 {
		w("DNS = %s\n", strings.Join(dns, ", "))
	}
	if mtu, err := s.Settings.GetInt(ctx, "network.mtu"); err == nil && mtu > 0 {
		w("MTU = %d\n", mtu)
	}
	b.WriteString("\n[Peer]\n")
	w("PublicKey = %s\n", ifc.PublicKey)
	if psk != "" {
		w("PresharedKey = %s\n", psk)
	}
	if allowed, err := s.Settings.GetString(ctx, "network.client_allowed_ips"); err == nil && allowed != "" {
		w("AllowedIPs = %s\n", strings.ReplaceAll(allowed, ",", ", "))
	}
	if endpoint != "" {
		w("Endpoint = %s\n", withPort(endpoint, ifc.ListenPort))
	}
	if ka, err := s.Settings.GetInt(ctx, "network.client_keepalive_seconds"); err == nil && ka > 0 {
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

// withPort appends the interface listen port unless the endpoint already
// carries one. Bare IPv6 literals are bracketed (they would otherwise read
// as host:port garbage).
func withPort(endpoint string, port int) string {
	if strings.Contains(endpoint, "]") { // [v6] or [v6]:port
		if strings.HasSuffix(endpoint, "]") {
			return fmt.Sprintf("%s:%d", endpoint, port)
		}
		return endpoint
	}
	if ip := net.ParseIP(endpoint); ip != nil && ip.To4() == nil {
		return fmt.Sprintf("[%s]:%d", endpoint, port)
	}
	if _, _, err := net.SplitHostPort(endpoint); err == nil {
		return endpoint // already host:port
	}
	return fmt.Sprintf("%s:%d", endpoint, port)
}
