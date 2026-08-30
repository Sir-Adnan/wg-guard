package api

import (
	"fmt"
	"net/http"

	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
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
	img, err := clientconf.QR(text)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(img)
}

// renderClientConfig delegates to the shared renderer (internal/clientconf)
// — the admin panel renders the exact same configs through the same code.
func (s *Server) renderClientConfig(r *http.Request, deviceID string) (string, error) {
	return s.ClientConf.Render(r.Context(), deviceID)
}
