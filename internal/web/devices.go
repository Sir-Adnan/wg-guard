package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

// deviceRow is one row of the user-detail device table.
type deviceRow struct {
	D      *device.Device
	Online bool
	LastHS *time.Time
}

// userDetailData feeds the user detail page.
type userDetailData struct {
	U            *userDetailUser
	Devices      []deviceRow
	PlanName     string
	IfaceName    string
	OnlineWindow int64
}

// userDetailUser wraps user.User with display helpers that templates cannot
// compute (pointer-free copies of the accumulated counters).
type userDetailUser struct {
	*user.User
	Used     int64
	HasLimit bool
}

// handleUserDetail renders the account page: overview, devices, actions.
func (s *Server) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	data := userDetailData{OnlineWindow: 180}
	if v, err := s.Settings.GetInt(ctx, "accounting.online_window_seconds"); err == nil && v > 0 {
		data.OnlineWindow = int64(v)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(data.OnlineWindow) * time.Second)

	if p, err := s.Plans.Get(ctx, deref(u.PlanID)); err == nil && u.PlanID != nil {
		data.PlanName = p.Name
	}
	if f, err := s.Ifaces.Get(ctx, deref(u.InterfaceID)); err == nil && u.InterfaceID != nil {
		data.IfaceName = f.Name
	}
	if devs, err := s.Devices.ListForUser(ctx, u.ID); err == nil {
		data.Devices = make([]deviceRow, 0, len(devs))
		for _, d := range devs {
			row := deviceRow{D: d}
			if d.LastHandshake != nil {
				hs := *d.LastHandshake
				row.LastHS = &hs
				row.Online = hs.After(cutoff)
			}
			data.Devices = append(data.Devices, row)
		}
	} else {
		s.logError(r, "device list", err)
	}
	data.U = &userDetailUser{User: u, Used: u.TrafficUsedRX + u.TrafficUsedTX, HasLimit: u.TrafficLimitBytes != nil}
	_ = s.render(w, r, "user_detail", "app", data)
}

// handleDeviceCreate provisions one device with fresh keys.
func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	name := r.PostFormValue("name")
	keys, err := s.generateKeys(r, false)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	if _, err := s.Devices.Create(r.Context(), u.ID, name, *keys, ""); err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest || domain.CodeOf(err) == domain.CodeDeviceLimitReached || domain.CodeOf(err) == domain.CodeDeviceKeyExists {
			s.redirectToast(w, r, "/users/"+u.ID, "common.error_validation")
			return
		}
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "device.created", u.ID, map[string]any{"name": name})
	s.runReconcile(r)
	s.redirectToast(w, r, "/users/"+u.ID, "devices.toast.created")
}

func (s *Server) handleDeviceEnable(w http.ResponseWriter, r *http.Request) {
	s.deviceToggle(w, r, true)
}

func (s *Server) handleDeviceDisable(w http.ResponseWriter, r *http.Request) {
	s.deviceToggle(w, r, false)
}

func (s *Server) deviceToggle(w http.ResponseWriter, r *http.Request, enable bool) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if err := s.Devices.SetEnabled(r.Context(), d.ID, enable); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, map[bool]string{true: "device.enabled", false: "device.disabled"}[enable], d.ID, nil)
	s.runReconcile(r)
	if enable {
		s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.enabled")
	} else {
		s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.disabled")
	}
}

func (s *Server) handleDeviceRegenerate(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	keys, err := s.generateKeys(r, false)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	if err := s.Devices.Regenerate(r.Context(), d.ID, *keys); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "device.regenerated", d.ID, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.regenerated")
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if err := s.Devices.Delete(r.Context(), d.ID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "device.deleted", d.ID, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.deleted")
}

// handleDeviceConfig streams the client .conf (no-store — key material).
func (s *Server) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	text, err := s.ClientConf.Render(r.Context(), r.PathValue("id"))
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="wg-guard.conf"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(text))
}

// handleDeviceQR streams the client config as a PNG (no-store).
func (s *Server) handleDeviceQR(w http.ResponseWriter, r *http.Request) {
	text, err := s.ClientConf.Render(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	png, err := clientconf.QR(text)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// loadDevice fetches the path device or writes the error response.
func (s *Server) loadDevice(w http.ResponseWriter, r *http.Request) (*device.Device, bool) {
	d, err := s.Devices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if domain.CodeOf(err) == domain.CodeDeviceNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return nil, false
		}
		s.logError(r, "device load", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	return d, true
}

// generateKeys mints a fresh X25519 keypair with the private half sealed by
// the master key ring (same path as the API; plaintext keys never persist).
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
